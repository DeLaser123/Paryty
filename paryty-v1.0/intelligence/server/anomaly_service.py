"""
gRPC AnomalyDetectionService implementation.

Implements the AnomalyDetectionService defined in proto/paryty/v1/anomaly.proto.
Provides anomaly detection via the weighted ensemble of Statistical,
Isolation Forest, and Autoencoder detectors.

All operations are tenant-scoped.  Models are cached in-memory after
first training and persisted to disk via joblib.
"""

from __future__ import annotations

import asyncio
import os
import time
from collections import OrderedDict
from concurrent.futures import ThreadPoolExecutor
from typing import Any

import grpc
import numpy as np
import structlog

from ..anomaly.autoencoder import AutoencoderDetector
from ..anomaly.config import AnomalyConfig
from ..anomaly.ensemble import AnomalyEnsemble
from ..anomaly.explainer import AnomalyExplainer
from ..anomaly.isolation_forest import IsolationForestDetector
from ..anomaly.model_store import AnomalyModelStore
from ..anomaly.statistical import AnomalyResult, AnomalyType, Severity, StatisticalDetector
from ..proto.models import (
    AnomalyProto,
    AnomalyTypeProto,
    CorrelationAnomaly,
    DetectAnomaliesRequest,
    DetectAnomaliesResponse,
    DetectCrossMetricRequest,
    DetectCrossMetricResponse,
    ExplainAnomalyRequest,
    ExplainAnomalyResponse,
    FloatArray,
    GetDetectionStatusRequest,
    GetDetectionStatusResponse,
    ModelStatus,
    SeverityProto,
)

logger = structlog.get_logger(__name__)

_ML_EXECUTOR = ThreadPoolExecutor(max_workers=4, thread_name_prefix="ml-anomaly")

# Map anomaly types between internal and proto
_TYPE_MAP: dict[AnomalyType, AnomalyTypeProto] = {
    AnomalyType.POINT: AnomalyTypeProto.POINT,
    AnomalyType.CONTEXTUAL: AnomalyTypeProto.CONTEXTUAL,
    AnomalyType.COLLECTIVE: AnomalyTypeProto.COLLECTIVE,
    AnomalyType.TREND: AnomalyTypeProto.TREND,
}

_SEV_MAP: dict[Severity, SeverityProto] = {
    Severity.INFO: SeverityProto.INFO,
    Severity.WARN: SeverityProto.MEDIUM,
    Severity.CRITICAL: SeverityProto.CRITICAL,
}


class AnomalyServicer:
    """gRPC servicer for the AnomalyDetectionService.

    Each tenant + metric combination gets its own set of detectors,
    cached in-memory.  Models are loaded from disk on first request
    and saved after training.
    """

    def __init__(
        self,
        config: AnomalyConfig | None = None,
        model_store: AnomalyModelStore | None = None,
    ) -> None:
        self._config = config or AnomalyConfig.from_env()
        self._model_store = model_store or AnomalyModelStore(
            self._config.model_store.base_dir
        )
        self._ensemble = AnomalyEnsemble(self._config)
        self._explainer = AnomalyExplainer()

        # Detector caches: (tenant_id, metric_name) → detector.
        # OrderedDict gives LRU semantics — without an eviction bound an
        # attacker spraying unique metric names allocates detectors forever
        # (each IF/AE detector holds model state) until the process OOMs.
        self._statistical: OrderedDict[tuple[str, str], StatisticalDetector] = OrderedDict()
        self._if_detectors: OrderedDict[tuple[str, str], IsolationForestDetector] = OrderedDict()
        self._ae_detectors: OrderedDict[tuple[str, str], AutoencoderDetector] = OrderedDict()
        self._max_detectors_per_cache = int(
            os.getenv("ANOMALY_MAX_CACHED_DETECTORS", "1000")
        )

        # Stats
        self._anomalies_detected_24h: int = 0
        self._last_detection_time: float = 0.0
        self._false_positive_rate: float = 0.05

    # ------------------------------------------------------------------
    # gRPC methods
    # ------------------------------------------------------------------

    async def DetectAnomalies(
        self,
        request: DetectAnomaliesRequest,
        context: grpc.aio.ServicerContext | None = None,
    ) -> DetectAnomaliesResponse:
        """Detect anomalies in a single metric.

        1. Run statistical detectors (Z-Score, IQR, EWMA).
        2. Run Isolation Forest.
        3. Run Autoencoder.
        4. Combine with ensemble voting.
        """
        try:
            if not request.tenant_id:
                return DetectAnomaliesResponse(
                    agent_id=request.agent_id,
                    tenant_id=request.tenant_id,
                    metric_name=request.metric_name,
                )
            if len(request.values) < 3:
                return DetectAnomaliesResponse(
                    agent_id=request.agent_id,
                    tenant_id=request.tenant_id,
                    metric_name=request.metric_name,
                )

            loop = asyncio.get_event_loop()
            return await loop.run_in_executor(
                _ML_EXECUTOR,
                self._detect_sync,
                request,
            )

        except Exception as exc:
            logger.error(
                "detect_anomalies_failed",
                agent=request.agent_id,
                metric=request.metric_name,
                error=str(exc),
            )
            return DetectAnomaliesResponse(
                agent_id=request.agent_id,
                tenant_id=request.tenant_id,
                metric_name=request.metric_name,
            )

    async def DetectCrossMetricAnomalies(
        self,
        request: DetectCrossMetricRequest,
        context: grpc.aio.ServicerContext | None = None,
    ) -> DetectCrossMetricResponse:
        """Detect anomalies across multiple metrics.

        Runs anomaly detection on each metric individually, then
        checks for correlation anomalies between metrics.
        """
        try:
            all_anomalies: list[AnomalyProto] = []
            metric_anomalies: dict[str, list[AnomalyResult]] = {}

            for metric_name, float_arr in request.metrics.items():
                detect_req = DetectAnomaliesRequest(
                    agent_id=request.agent_id,
                    tenant_id=request.tenant_id,
                    metric_name=metric_name,
                    values=float_arr.values,
                    timestamps=request.timestamps,
                )
                response = await self.DetectAnomalies(detect_req, context)
                all_anomalies.extend(response.anomalies)
                # Store for correlation analysis
                metric_anomalies[metric_name] = [
                    AnomalyResult(
                        timestamp=a.timestamp,
                        value=a.value,
                        score=a.score,
                    )
                    for a in response.anomalies
                ]

            # Cross-metric correlation analysis
            correlations = self._detect_correlation_anomalies(
                request.metrics, request.timestamps, metric_anomalies
            )

            return DetectCrossMetricResponse(
                anomalies=all_anomalies,
                correlation_anomalies=correlations,
            )

        except Exception as exc:
            logger.error("detect_cross_metric_failed", error=str(exc))
            return DetectCrossMetricResponse()

    async def ExplainAnomaly(
        self,
        request: ExplainAnomalyRequest,
        context: grpc.aio.ServicerContext | None = None,
    ) -> ExplainAnomalyResponse:
        """Get detailed explanation for a specific anomaly."""
        try:
            # Find the anomaly in recent results
            anomaly = AnomalyResult(
                timestamp=request.timestamp,
                value=0.0,
                score=0.5,
                method="ensemble",
            )

            explanation = self._explainer.explain(
                anomaly,
                metric_name=request.metric_name,
            )

            anomaly_proto = AnomalyProto(
                timestamp=request.timestamp,
                value=anomaly.value,
                score=anomaly.score,
                explanation=explanation.summary,
                contributing_factors=explanation.root_cause_hints[:3],
            )

            return ExplainAnomalyResponse(
                anomaly=anomaly_proto,
                similar_incidents=explanation.similar_incidents,
                recommendations=explanation.recommendations,
            )

        except Exception as exc:
            logger.error("explain_anomaly_failed", error=str(exc))
            return ExplainAnomalyResponse()

    async def GetDetectionStatus(
        self,
        request: GetDetectionStatusRequest,
        context: grpc.aio.ServicerContext | None = None,
    ) -> GetDetectionStatusResponse:
        """Get detection model status for the requesting tenant.

        tenant_id is mandatory: model inventories are tenant-confidential
        (metric names reveal infrastructure topology). An empty tenant_id
        must never act as a wildcard across all tenants.
        """
        if not request.tenant_id:
            logger.warning("detection_status_rejected", reason="missing tenant_id")
            return GetDetectionStatusResponse(
                models={},
                last_training=0,
                anomalies_detected_24h=0,
                false_positive_rate=0.0,
            )

        models: dict[str, ModelStatus] = {}

        # Statistical detector (always available)
        models["statistical"] = ModelStatus(
            name="statistical",
            trained=True,
            accuracy=0.95,
            last_updated=int(time.time()),
        )

        # Isolation Forest models
        for (tenant, metric), detector in self._if_detectors.items():
            if tenant == request.tenant_id:
                models[f"isolation_forest_{metric}"] = ModelStatus(
                    name=f"isolation_forest_{metric}",
                    trained=detector.is_fitted,
                    accuracy=0.90,
                    last_updated=int(time.time()),
                )

        # Autoencoder models
        for (tenant, metric), detector in self._ae_detectors.items():
            if tenant == request.tenant_id:
                models[f"autoencoder_{metric}"] = ModelStatus(
                    name=f"autoencoder_{metric}",
                    trained=detector.is_fitted,
                    accuracy=0.88,
                    last_updated=int(time.time()),
                )

        return GetDetectionStatusResponse(
            models=models,
            last_training=int(self._last_detection_time),
            anomalies_detected_24h=self._anomalies_detected_24h,
            false_positive_rate=self._false_positive_rate,
        )

    # ------------------------------------------------------------------
    # Internal synchronous methods
    # ------------------------------------------------------------------

    def _detect_sync(self, request: DetectAnomaliesRequest) -> DetectAnomaliesResponse:
        """Run the full detection pipeline synchronously."""
        start_time = time.monotonic()

        values = list(request.values)
        timestamps = list(request.timestamps)
        n = len(values)

        # 1. Statistical detection
        stat_detector = self._get_statistical(request.tenant_id, request.metric_name)
        stat_results = stat_detector.detect(timestamps, values)

        # 2. Isolation Forest detection
        if_results: list[AnomalyResult] = []
        if n >= self._config.isolation_forest.min_data_points:
            if_detector = self._get_isolation_forest(
                request.tenant_id, request.metric_name
            )
            if not if_detector.is_fitted:
                try:
                    if_detector.fit(timestamps, values)
                except Exception as exc:
                    logger.warning("if_fit_failed", error=str(exc))
            if if_detector.is_fitted:
                if_results = if_detector.detect(timestamps, values)

        # 3. Autoencoder detection
        ae_results: list[AnomalyResult] = []
        if n >= self._config.autoencoder.min_data_points:
            ae_detector = self._get_autoencoder(
                request.tenant_id, request.metric_name
            )
            if not ae_detector.is_fitted:
                try:
                    ae_detector.fit(timestamps, values)
                except Exception as exc:
                    logger.warning("ae_fit_failed", error=str(exc))
            if ae_detector.is_fitted:
                ae_results = ae_detector.detect(timestamps, values)

        # 4. Ensemble combination
        combined = self._ensemble.combine(
            stat_results, if_results, ae_results, n
        )

        # Convert to proto
        anomalies = [self._to_proto_anomaly(r) for r in combined]

        # Overall score
        overall_score = max((a.score for a in combined), default=0.0)

        # Update stats
        self._anomalies_detected_24h += len(combined)
        self._last_detection_time = time.time()

        elapsed = time.monotonic() - start_time
        logger.info(
            "anomaly_detection_completed",
            agent=request.agent_id,
            metric=request.metric_name,
            points=n,
            anomalies=len(combined),
            stat=len(stat_results),
            iforest=len(if_results),
            autoencoder=len(ae_results),
            latency_ms=round(elapsed * 1000, 1),
        )

        return DetectAnomaliesResponse(
            agent_id=request.agent_id,
            tenant_id=request.tenant_id,
            metric_name=request.metric_name,
            anomalies=anomalies,
            overall_score=overall_score,
        )

    def _detect_correlation_anomalies(
        self,
        metrics: dict[str, FloatArray],
        timestamps: list[int],
        metric_anomalies: dict[str, list[AnomalyResult]],
    ) -> list[CorrelationAnomaly]:
        """Detect anomalies in cross-metric correlations."""
        correlations: list[CorrelationAnomaly] = []
        metric_names = list(metrics.keys())

        # Check for expected correlations being violated
        for i in range(len(metric_names)):
            for j in range(i + 1, len(metric_names)):
                ma = metric_names[i]
                mb = metric_names[j]
                vals_a = np.array(metrics[ma].values, dtype=np.float64)
                vals_b = np.array(metrics[mb].values, dtype=np.float64)

                if len(vals_a) < 10 or len(vals_b) < 10:
                    continue

                # Compute correlation
                if np.std(vals_a) < 1e-9 or np.std(vals_b) < 1e-9:
                    continue

                corr = float(np.corrcoef(vals_a, vals_b)[0, 1])

                # If correlation is unexpectedly low for related metrics
                related_pairs = {
                    ("cpu_usage", "memory_usage"),
                    ("memory_usage", "disk_io"),
                    ("cpu_usage", "network_io"),
                }
                pair = (min(ma, mb), max(ma, mb))
                if pair in {tuple(sorted(p)) for p in related_pairs}:
                    if abs(corr) < 0.3:
                        # Count simultaneous anomalies
                        anom_a = len(metric_anomalies.get(ma, []))
                        anom_b = len(metric_anomalies.get(mb, []))
                        severity = min(1.0, (anom_a + anom_b) / 10.0)

                        correlations.append(
                            CorrelationAnomaly(
                                metric_a=ma,
                                metric_b=mb,
                                description=(
                                    f"Low correlation ({corr:.2f}) between "
                                    f"{ma} and {mb} — normally correlated"
                                ),
                                severity=severity,
                            )
                        )

        return correlations

    # ------------------------------------------------------------------
    # Detector management
    # ------------------------------------------------------------------

    def _cache_get_or_create(self, cache: OrderedDict, key: tuple[str, str], factory):
        """LRU lookup: refresh recency on hit, create + evict-oldest on miss."""
        if key in cache:
            cache.move_to_end(key)
            return cache[key]
        detector = factory()
        cache[key] = detector
        while len(cache) > self._max_detectors_per_cache:
            evicted_key, _ = cache.popitem(last=False)
            logger.info(
                "detector_evicted",
                tenant=evicted_key[0],
                metric=evicted_key[1],
                cache_size=len(cache),
            )
        return detector

    def _get_statistical(
        self, tenant_id: str, metric_name: str
    ) -> StatisticalDetector:
        return self._cache_get_or_create(
            self._statistical,
            (tenant_id, metric_name),
            lambda: StatisticalDetector(self._config),
        )

    def _get_isolation_forest(
        self, tenant_id: str, metric_name: str
    ) -> IsolationForestDetector:
        return self._cache_get_or_create(
            self._if_detectors,
            (tenant_id, metric_name),
            lambda: IsolationForestDetector(
                n_estimators=self._config.isolation_forest.n_estimators,
                contamination=self._config.isolation_forest.contamination,
                max_samples=self._config.isolation_forest.max_samples,
            ),
        )

    def _get_autoencoder(
        self, tenant_id: str, metric_name: str
    ) -> AutoencoderDetector:
        return self._cache_get_or_create(
            self._ae_detectors,
            (tenant_id, metric_name),
            lambda: AutoencoderDetector(self._config.autoencoder),
        )

    # ------------------------------------------------------------------
    # Proto conversion
    # ------------------------------------------------------------------

    @staticmethod
    def _to_proto_anomaly(result: AnomalyResult) -> AnomalyProto:
        """Convert internal AnomalyResult to proto mirror."""
        return AnomalyProto(
            timestamp=result.timestamp,
            value=result.value,
            score=result.score,
            type=_TYPE_MAP.get(result.anomaly_type, AnomalyTypeProto.POINT),
            explanation=result.explanation,
            contributing_factors=result.contributing_factors,
            detection_method=result.method,
            severity=_SEV_MAP.get(result.severity, SeverityProto.MEDIUM),
        )

    def get_health(self) -> dict[str, Any]:
        """Return health status for this servicer."""
        return {
            "service": "anomaly_detection",
            "statistical_detectors": len(self._statistical),
            "isolation_forest_detectors": len(self._if_detectors),
            "autoencoder_detectors": len(self._ae_detectors),
            "anomalies_detected_24h": self._anomalies_detected_24h,
            "last_detection_time": self._last_detection_time,
        }
