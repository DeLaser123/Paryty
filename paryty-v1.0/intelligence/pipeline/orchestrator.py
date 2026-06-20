"""
Intelligence Pipeline Orchestrator.

The central coordinator that bridges the Redpanda consumer callback to
all intelligence subsystems:
- Stores incoming metrics in Dragonfly hot store
- Triggers anomaly detection on count-gated windows
- Generates forecasts on time-gated intervals
- Runs concept drift detection on every value
- Publishes results via IntelligenceProducer
- Records Prometheus metrics at each stage
- Manages periodic model retraining

All heavy ML work is dispatched to a shared ThreadPoolExecutor.
The consumer callback itself only performs fast operations (Redis write,
counter increment, O(1) drift update).
"""

from __future__ import annotations

import re
import time
from concurrent.futures import Future, ThreadPoolExecutor
from typing import Any

import structlog

from ..anomaly.concept_drift import DriftDetectionManager, DriftResult
from ..data.dragonfly_client import DragonflyClient
from ..data.streaming import IntelligenceProducer
from ..forecasting.retrain_scheduler import RetrainScheduler
from ..proto.models import (
    DetectAnomaliesRequest,
    DetectAnomaliesResponse,
    ForecastMetricResponse,
)
from ..server.metrics import (
    record_drift_detection,
    record_metric_consumed,
    record_pipeline_anomaly,
    record_pipeline_error,
    record_pipeline_forecast,
    record_forecast_generated,
    update_pipeline_active_metrics,
)
from .buffer import MetricBuffer
from .config import PipelineConfig

logger = structlog.get_logger(__name__)

# Regex to extract tenant_id from topic name: paryty.<tenant>.metrics.raw
_TENANT_TOPIC_RE = re.compile(r"^paryty\.(.+?)\.metrics\.raw$")


class IntelligencePipeline:
    """End-to-end metric processing pipeline.

    Receives metrics from the Redpanda consumer, buffers them,
    runs anomaly detection, concept drift detection, and
    periodic forecasting — publishing results back to Redpanda.
    """

    def __init__(
        self,
        anomaly_servicer: Any,  # AnomalyServicer
        forecasting_servicer: Any,  # ForecastingServicer
        dragonfly: DragonflyClient,
        producer: IntelligenceProducer,
        config: PipelineConfig,
    ) -> None:
        self._anomaly = anomaly_servicer
        self._forecast = forecasting_servicer
        self._dragonfly = dragonfly
        self._producer = producer
        self._config = config

        # ── Sub-systems ────────────────────────────────────────────
        self._buffer = MetricBuffer(min_points=config.anomaly_min_points)

        self._drift: DriftDetectionManager | None = None
        if config.drift_enabled:
            self._drift = DriftDetectionManager()
            # LRU eviction: DriftDetectionManager has no built-in cap,
            # so we enforce it manually in on_metric().

        self._retrain: RetrainScheduler | None = None
        if config.retrain_enabled:
            self._retrain = RetrainScheduler(
                retrain_fn=self._do_retrain,
                check_interval_seconds=3600,
            )

        # ── Thread pool for ML work ────────────────────────────────
        self._executor = ThreadPoolExecutor(
            max_workers=config.pipeline_workers,
            thread_name_prefix="intel-pipeline",
        )

        # ── Drain thread ───────────────────────────────────────────
        self._drain_thread: Any = None
        self._running = False

        # ── Retrain cooldown tracking ──────────────────────────────
        self._last_retrain: dict[tuple[str, str], float] = {}

        # ── In-memory fallback buffer when Dragonfly is down ───────
        self._fallback_buffer: dict[tuple[str, str, str], list[tuple[int, float]]] = {}
        self._max_fallback_points = 1000

    # ==================================================================
    # Lifecycle
    # ==================================================================

    def start(self) -> None:
        """Start the pipeline: connect Dragonfly, start retrain scheduler,
        start drain thread."""
        if self._running:
            return
        self._running = True

        # Connect Dragonfly (lazy, may already be connected)
        try:
            self._dragonfly.connect()
            logger.info("pipeline_dragonfly_connected")
        except Exception as exc:
            logger.warning("pipeline_dragonfly_connect_failed", error=str(exc))

        # Start retrain scheduler
        if self._retrain is not None:
            self._retrain.start()

        # Start drain thread
        import threading

        self._drain_thread = threading.Thread(
            target=self._drain_loop,
            name="intel-pipeline-drain",
            daemon=True,
        )
        self._drain_thread.start()

        logger.info(
            "intelligence_pipeline_started",
            anomaly_enabled=self._config.anomaly_enabled,
            forecast_enabled=self._config.forecast_enabled,
            drift_enabled=self._config.drift_enabled,
            retrain_enabled=self._config.retrain_enabled,
        )

    def stop(self) -> None:
        """Stop the pipeline: stop retrain scheduler, drain thread, flush producer."""
        if not self._running:
            return
        self._running = False

        # Stop retrain scheduler
        if self._retrain is not None:
            self._retrain.stop()

        # Join drain thread
        if self._drain_thread is not None:
            self._drain_thread.join(timeout=10)
            self._drain_thread = None

        # Shutdown executor (cancel pending futures)
        self._executor.shutdown(wait=False, cancel_futures=True)

        # Flush producer
        try:
            self._producer.flush(timeout=10)
        except Exception as exc:
            logger.warning("pipeline_producer_flush_failed", error=str(exc))

        logger.info(
            "intelligence_pipeline_stopped",
            active_metrics=self._buffer.active_metrics,
        )

    @property
    def is_running(self) -> bool:
        return self._running

    # ==================================================================
    # Consumer callback (called on Kafka consumer thread — must be fast)
    # ==================================================================

    def on_metric(self, msg: dict[str, Any]) -> None:
        """Process a single consumed metric message.

        This method runs on the Kafka consumer thread.  It MUST be fast:
        only O(1) operations and fire-and-forget submissions to the
        thread pool.  Heavy ML work is dispatched asynchronously.
        """
        try:
            # ── Parse message ──────────────────────────────────────
            tenant_id = msg.get("tenant_id", "")
            agent_id = msg.get("agent_id", "")
            metric_name = msg.get("metric_name", "")
            value = float(msg.get("value", 0.0))
            timestamp = int(msg.get("timestamp", time.time()))

            # Extract tenant from topic if not in message body
            if not tenant_id:
                tenant_id = self._extract_tenant_from_topic(msg)
            if not tenant_id:
                tenant_id = "default"

            # ── 1. Store in Dragonfly (fast Redis ZADD) ────────────
            self._store_metric(metric_name, agent_id, tenant_id, timestamp, value)

            # ── 2. Concept drift detection (O(1) per value) ────────
            if self._drift is not None:
                self._check_drift(tenant_id, agent_id, metric_name, value, timestamp)

            # ── 3. Count-gated anomaly detection ───────────────────
            if self._config.anomaly_enabled:
                window_ready = self._buffer.add(tenant_id, agent_id, metric_name)
                if window_ready:
                    self._executor.submit(
                        self._run_anomaly_detection,
                        tenant_id,
                        agent_id,
                        metric_name,
                    )

            # ── 4. Register metric for retrain scheduler ───────────
            if self._retrain is not None:
                self._retrain.add_metric(tenant_id, metric_name)

            # ── 5. Prometheus ──────────────────────────────────────
            record_metric_consumed(tenant_id, metric_name)
            update_pipeline_active_metrics(self._buffer.active_metrics)

        except Exception as exc:
            logger.warning("pipeline_on_metric_error", error=str(exc))
            record_pipeline_error("on_metric")

    # ==================================================================
    # Anomaly detection (runs in thread pool)
    # ==================================================================

    def _run_anomaly_detection(
        self, tenant_id: str, agent_id: str, metric_name: str
    ) -> None:
        """Run anomaly detection on a recent window of data."""
        try:
            # Read recent window from Dragonfly
            timestamps, values = self._dragonfly.get_metric_window(
                metric_name=metric_name,
                agent_id=agent_id,
                tenant_id=tenant_id,
                window_minutes=self._config.anomaly_window_minutes,
            )

            # Fall back to in-memory buffer if Dragonfly has no data
            if len(values) < 3:
                fallback_key = (tenant_id, agent_id, metric_name)
                fallback = self._fallback_buffer.get(fallback_key, [])
                if fallback:
                    timestamps = [ts for ts, _ in fallback]
                    values = [val for _, val in fallback]

            if len(values) < 3:
                return  # Not enough data for detection

            # Build request dataclass
            request = DetectAnomaliesRequest(
                agent_id=agent_id,
                tenant_id=tenant_id,
                metric_name=metric_name,
                values=values,
                timestamps=timestamps,
            )

            # Run detection (uses existing AnomalyServicer internals)
            response: DetectAnomaliesResponse = self._anomaly._detect_sync(request)

            # Publish results if anomalies found
            if response.anomalies:
                anomaly_dicts = [
                    {
                        "timestamp": a.timestamp,
                        "value": a.value,
                        "score": a.score,
                        "type": a.type.value if hasattr(a.type, "value") else str(a.type),
                        "severity": a.severity.value if hasattr(a.severity, "value") else str(a.severity),
                        "explanation": a.explanation,
                        "detection_method": a.detection_method,
                    }
                    for a in response.anomalies
                ]
                try:
                    self._producer.publish_anomaly_tenant(
                        tenant_id=tenant_id,
                        agent_id=agent_id,
                        metric_name=metric_name,
                        anomalies=anomaly_dicts,
                    )
                except Exception as exc:
                    logger.warning("pipeline_anomaly_publish_failed", error=str(exc))

                logger.info(
                    "pipeline_anomaly_detected",
                    tenant=tenant_id,
                    agent=agent_id,
                    metric=metric_name,
                    count=len(response.anomalies),
                    score=response.overall_score,
                )

            # Prometheus
            record_pipeline_anomaly(tenant_id)

        except Exception as exc:
            logger.error(
                "pipeline_anomaly_detection_failed",
                tenant=tenant_id,
                metric=metric_name,
                error=str(exc),
            )
            record_pipeline_error("anomaly_detection")

    # ==================================================================
    # Forecasting (runs in thread pool, called from drain thread)
    # ==================================================================

    def _run_forecast(
        self, tenant_id: str, agent_id: str, metric_name: str
    ) -> None:
        """Generate a forecast for a single metric."""
        try:
            response: ForecastMetricResponse = self._forecast._forecast_sync(
                agent_id=agent_id,
                tenant_id=tenant_id,
                metric_name=metric_name,
                horizon_seconds=self._config.forecast_horizon_seconds,
                step_seconds=self._config.forecast_step_seconds,
            )

            # Convert response to serializable dict
            forecast_dict = {
                "overall_confidence": response.overall_confidence,
                "model_info": {
                    "best_model": response.model_info.best_model,
                    "weights": dict(response.model_info.weights),
                    "accuracy": dict(response.model_info.accuracy),
                    "last_trained": response.model_info.last_trained,
                    "training_samples": response.model_info.training_samples,
                },
                "forecast_points": [
                    {
                        "timestamp": p.timestamp,
                        "value": p.value,
                        "lower_bound": p.lower_bound,
                        "upper_bound": p.upper_bound,
                    }
                    for p in response.forecast
                ],
            }

            # Publish
            try:
                self._producer.publish_forecast_tenant(
                    tenant_id=tenant_id,
                    agent_id=agent_id,
                    metric_name=metric_name,
                    forecast=forecast_dict,
                )
            except Exception as exc:
                logger.warning("pipeline_forecast_publish_failed", error=str(exc))

            # Update buffer and Prometheus
            self._buffer.mark_forecast(tenant_id, metric_name)
            record_pipeline_forecast(tenant_id)
            record_forecast_generated(tenant_id, metric_name, response.model_info.best_model)

            logger.info(
                "pipeline_forecast_generated",
                tenant=tenant_id,
                metric=metric_name,
                confidence=response.overall_confidence,
                points=len(response.forecast),
            )

        except Exception as exc:
            logger.error(
                "pipeline_forecast_failed",
                tenant=tenant_id,
                metric=metric_name,
                error=str(exc),
            )
            record_pipeline_error("forecast")

    # ==================================================================
    # Concept drift detection (runs on consumer thread — O(1))
    # ==================================================================

    def _check_drift(
        self,
        tenant_id: str,
        agent_id: str,
        metric_name: str,
        value: float,
        timestamp: int,
    ) -> None:
        """Check for concept drift and act on detection."""
        assert self._drift is not None

        # LRU eviction: cap number of detectors
        if len(self._drift._detectors) >= self._config.max_drift_detectors:
            # Remove the oldest detector (first key)
            try:
                oldest = next(iter(self._drift._detectors))
                self._drift.remove_detector(oldest)
            except StopIteration:
                pass

        result: DriftResult = self._drift.update_metric(metric_name, value, timestamp)

        if not result.drift_detected:
            return

        # ── Drift detected — act ───────────────────────────────────
        logger.info(
            "pipeline_drift_detected",
            metric=metric_name,
            drift_type=result.drift_type,
            confidence=result.confidence,
        )

        record_drift_detection(metric_name, result.drift_type)

        # Publish drift event
        try:
            self._producer.publish_drift(
                tenant_id=tenant_id,
                agent_id=agent_id,
                metric_name=metric_name,
                drift_type=result.drift_type,
                confidence=result.confidence,
                timestamp=result.timestamp,
            )
        except Exception as exc:
            logger.warning("pipeline_drift_publish_failed", error=str(exc))

        # Trigger retrain if confidence is high enough and cooldown elapsed
        if self._retrain is not None and result.confidence >= 0.67:
            retrain_key = (tenant_id, metric_name)
            last = self._last_retrain.get(retrain_key, 0.0)
            if (time.time() - last) >= self._config.retrain_cooldown_seconds:
                self._last_retrain[retrain_key] = time.time()
                self._executor.submit(self._do_retrain, tenant_id, metric_name)
                logger.info(
                    "pipeline_drift_retrain_triggered",
                    tenant=tenant_id,
                    metric=metric_name,
                )

    # ==================================================================
    # Retrain function (called by scheduler and drift-triggered retrains)
    # ==================================================================

    def _do_retrain(self, tenant_id: str, metric_name: str) -> bool:
        """Retrain the forecasting model for a tenant+metric."""
        try:
            response = self._forecast._forecast_sync(
                agent_id="",  # retrain across all agents
                tenant_id=tenant_id,
                metric_name=metric_name,
                horizon_seconds=self._config.forecast_horizon_seconds,
                step_seconds=self._config.forecast_step_seconds,
            )
            logger.info(
                "pipeline_retrain_completed",
                tenant=tenant_id,
                metric=metric_name,
                confidence=response.overall_confidence,
            )
            return True
        except Exception as exc:
            logger.error(
                "pipeline_retrain_failed",
                tenant=tenant_id,
                metric=metric_name,
                error=str(exc),
            )
            record_pipeline_error("retrain")
            return False

    # ==================================================================
    # Drain thread (periodic forecast sweep)
    # ==================================================================

    def _drain_loop(self) -> None:
        """Background thread that periodically checks for forecast eligibility."""
        while self._running:
            try:
                time.sleep(self._config.drain_interval_seconds)
                if not self._running:
                    break

                if not self._config.forecast_enabled:
                    continue

                # Iterate known metrics and check if forecast is due
                known = self._buffer.get_known_metrics()
                for tenant_id, metric_name in known:
                    if not self._running:
                        break
                    if self._buffer.should_forecast(
                        tenant_id,
                        metric_name,
                        self._config.forecast_interval_seconds,
                    ):
                        self._executor.submit(
                            self._run_forecast,
                            tenant_id,
                            "",  # agent_id not needed for forecast
                            metric_name,
                        )

            except Exception as exc:
                logger.error("pipeline_drain_loop_error", error=str(exc))
                record_pipeline_error("drain_loop")

    # ==================================================================
    # Helpers
    # ==================================================================

    def _store_metric(
        self,
        metric_name: str,
        agent_id: str,
        tenant_id: str,
        timestamp: int,
        value: float,
    ) -> None:
        """Store metric in Dragonfly hot store, with in-memory fallback."""
        try:
            self._dragonfly.store_metric(
                metric_name=metric_name,
                agent_id=agent_id,
                tenant_id=tenant_id,
                timestamp=timestamp,
                value=value,
            )
        except Exception as exc:
            logger.warning("pipeline_dragonfly_store_failed", error=str(exc))
            # Fallback: buffer in memory
            key = (tenant_id, agent_id, metric_name)
            buf = self._fallback_buffer.setdefault(key, [])
            if len(buf) < self._max_fallback_points:
                buf.append((timestamp, value))
            else:
                # Ring buffer: drop oldest
                buf.pop(0)
                buf.append((timestamp, value))

    @staticmethod
    def _extract_tenant_from_topic(msg: dict[str, Any]) -> str:
        """Try to extract tenant_id from the Redpanda topic name.

        The consumer message may include a ``_topic`` metadata field
        injected by the consumer wrapper.  Falls back to empty string.
        """
        topic = msg.get("_topic", "")
        if topic:
            m = _TENANT_TOPIC_RE.match(topic)
            if m:
                return m.group(1)
        return ""
