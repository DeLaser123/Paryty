"""
Paryty Intelligence Service — gRPC Server Entry Point.

Runs the ForecastingService and AnomalyDetectionService as async
gRPC services using compiled protobuf stubs.  Handles graceful shutdown
on SIGINT/SIGTERM.

Usage::

    # Direct
    python -m intelligence.server.main

    # Via entry point
    paryty-intel

Environment Variables:
    INTELLIGENCE_PORT: gRPC port (default: 50051)
    INTELLIGENCE_HOST: Bind host (default: [::])
    LOG_LEVEL: Logging level (default: INFO)
    INTELLIGENCE_TLS_CERT: Path to the server certificate (PEM)
    INTELLIGENCE_TLS_KEY: Path to the server private key (PEM)
    INTELLIGENCE_TLS_CLIENT_CA: Path to the CA bundle for client cert
        verification — presence enables mutual TLS (cluster↔intelligence
        communication is mTLS per the architecture)
    PARYTY_DEV_MODE: "true" permits plaintext gRPC for local development.
        Without it, missing TLS material is a fatal misconfiguration.
"""

from __future__ import annotations

import asyncio
import os
import signal
import sys
import time
from concurrent import futures
from typing import Any

import grpc
import grpc.aio
import structlog

# Generated protobuf stubs (compiled from proto/paryty/v1/*.proto)
from ..proto.generated.paryty.v1 import anomaly_pb2
from ..proto.generated.paryty.v1 import anomaly_pb2_grpc
from ..proto.generated.paryty.v1 import forecasting_pb2
from ..proto.generated.paryty.v1 import forecasting_pb2_grpc

# Internal servicer implementations
from .anomaly_service import AnomalyServicer
from .forecasting_service import ForecastingServicer
from ..data.streaming import MetricConsumer, IntelligenceProducer
from ..data.dragonfly_client import DragonflyClient

# Pipeline (lazy import — may not be installed in all environments)
pipeline_available = False
try:
    from ..pipeline.config import PipelineConfig
    from ..pipeline.orchestrator import IntelligencePipeline
    pipeline_available = True
except ImportError:
    PipelineConfig = None  # type: ignore[assignment,misc]
    IntelligencePipeline = None  # type: ignore[assignment,misc]

# Dataclass models for internal conversion
from ..proto.models import (
    ForecastMetricRequest as DCForecastMetricRequest,
    ForecastMetricResponse as DCForecastMetricResponse,
    ForecastPoint as DCForecastPoint,
    ModelInfo as DCModelInfo,
    ForecastBatchRequest as DCForecastBatchRequest,
    ForecastBatchResponse as DCForecastBatchResponse,
    GetModelAccuracyRequest as DCGetModelAccuracyRequest,
    GetModelAccuracyResponse as DCGetModelAccuracyResponse,
    RetrainModelsRequest as DCRetrainModelsRequest,
    RetrainModelsResponse as DCRetrainModelsResponse,
    DetectAnomaliesRequest as DCDetectAnomaliesRequest,
    DetectAnomaliesResponse as DCDetectAnomaliesResponse,
    DetectCrossMetricRequest as DCDetectCrossMetricRequest,
    DetectCrossMetricResponse as DCDetectCrossMetricResponse,
    FloatArray as DCFloatArray,
    ExplainAnomalyRequest as DCExplainAnomalyRequest,
    ExplainAnomalyResponse as DCExplainAnomalyResponse,
    AnomalyProto as DCAnomalyProto,
    CorrelationAnomaly as DCCorrelationAnomaly,
    AnomalyTypeProto as DCAnomalyTypeProto,
    SeverityProto as DCSeverityProto,
    GetDetectionStatusRequest as DCGetDetectionStatusRequest,
    GetDetectionStatusResponse as DCGetDetectionStatusResponse,
    ModelStatus as DCModelStatus,
)

logger = structlog.get_logger(__name__)


# ---------------------------------------------------------------------------
# Proto ↔ Dataclass conversion helpers
# ---------------------------------------------------------------------------

def _proto_to_dc_anomaly_type(proto_val: int) -> DCAnomalyTypeProto:
    _map = {
        0: DCAnomalyTypeProto.UNSPECIFIED,
        1: DCAnomalyTypeProto.POINT,
        2: DCAnomalyTypeProto.CONTEXTUAL,
        3: DCAnomalyTypeProto.COLLECTIVE,
        4: DCAnomalyTypeProto.TREND,
    }
    return _map.get(proto_val, DCAnomalyTypeProto.UNSPECIFIED)


def _dc_to_proto_anomaly_type(dc_val: DCAnomalyTypeProto) -> int:
    _map = {
        DCAnomalyTypeProto.UNSPECIFIED: 0,
        DCAnomalyTypeProto.POINT: 1,
        DCAnomalyTypeProto.CONTEXTUAL: 2,
        DCAnomalyTypeProto.COLLECTIVE: 3,
        DCAnomalyTypeProto.TREND: 4,
    }
    return _map.get(dc_val, 0)


def _proto_to_dc_severity(proto_val: int) -> DCSeverityProto:
    _map = {
        0: DCSeverityProto.UNSPECIFIED,
        1: DCSeverityProto.CRITICAL,
        2: DCSeverityProto.HIGH,
        3: DCSeverityProto.MEDIUM,
        4: DCSeverityProto.LOW,
        5: DCSeverityProto.INFO,
    }
    return _map.get(proto_val, DCSeverityProto.UNSPECIFIED)


def _dc_to_proto_severity(dc_val: DCSeverityProto) -> int:
    _map = {
        DCSeverityProto.UNSPECIFIED: 0,
        DCSeverityProto.CRITICAL: 1,
        DCSeverityProto.HIGH: 2,
        DCSeverityProto.MEDIUM: 3,
        DCSeverityProto.LOW: 4,
        DCSeverityProto.INFO: 5,
    }
    return _map.get(dc_val, 0)


def _dc_model_info_to_proto(dc: DCModelInfo) -> forecasting_pb2.ModelInfo:
    return forecasting_pb2.ModelInfo(
        best_model=dc.best_model,
        weights=dc.weights,
        accuracy=dc.accuracy,
        last_trained=dc.last_trained,
        training_samples=dc.training_samples,
    )


def _dc_forecast_point_to_proto(dc: DCForecastPoint) -> forecasting_pb2.ForecastPoint:
    return forecasting_pb2.ForecastPoint(
        timestamp=dc.timestamp,
        value=dc.value,
        lower_bound=dc.lower_bound,
        upper_bound=dc.upper_bound,
    )


def _dc_anomaly_to_proto(dc: DCAnomalyProto) -> anomaly_pb2.Anomaly:
    return anomaly_pb2.Anomaly(
        timestamp=dc.timestamp,
        value=dc.value,
        score=dc.score,
        type=_dc_to_proto_anomaly_type(dc.type),
        explanation=dc.explanation,
        contributing_factors=dc.contributing_factors,
        detection_method=dc.detection_method,
        severity=_dc_to_proto_severity(dc.severity),
    )


def _proto_anomaly_to_dc(proto: anomaly_pb2.Anomaly) -> DCAnomalyProto:
    return DCAnomalyProto(
        timestamp=proto.timestamp,
        value=proto.value,
        score=proto.score,
        type=_proto_to_dc_anomaly_type(proto.type),
        explanation=proto.explanation,
        contributing_factors=list(proto.contributing_factors),
        detection_method=proto.detection_method,
        severity=_proto_to_dc_severity(proto.severity),
    )


# ---------------------------------------------------------------------------
# gRPC Servicer wrappers — extend generated base, delegate to impl
# ---------------------------------------------------------------------------


class _ForecastingGrpcServicer(forecasting_pb2_grpc.ForecastingServiceServicer):
    """Async gRPC servicer that wraps ForecastingServicer.

    Converts between compiled protobuf messages (wire format) and the
    internal dataclass models used by ForecastingServicer.
    """

    def __init__(self, inner: ForecastingServicer) -> None:
        self._inner = inner

    async def ForecastMetric(self, request: forecasting_pb2.ForecastMetricRequest, context: Any) -> forecasting_pb2.ForecastMetricResponse:
        dc_req = DCForecastMetricRequest(
            agent_id=request.agent_id,
            tenant_id=request.tenant_id,
            metric_name=request.metric_name,
            horizon_seconds=request.horizon_seconds,
            confidence_level=request.confidence_level,
            step_seconds=request.step_seconds,
        )
        dc_resp = await self._inner.ForecastMetric(dc_req, context)
        return forecasting_pb2.ForecastMetricResponse(
            agent_id=dc_resp.agent_id,
            tenant_id=dc_resp.tenant_id,
            metric_name=dc_resp.metric_name,
            forecast=[_dc_forecast_point_to_proto(fp) for fp in dc_resp.forecast],
            model_info=_dc_model_info_to_proto(dc_resp.model_info),
            overall_confidence=dc_resp.overall_confidence,
        )

    async def ForecastBatch(self, request: forecasting_pb2.ForecastBatchRequest, context: Any) -> forecasting_pb2.ForecastBatchResponse:
        dc_reqs = [
            DCForecastMetricRequest(
                agent_id=r.agent_id,
                tenant_id=r.tenant_id,
                metric_name=r.metric_name,
                horizon_seconds=r.horizon_seconds,
                confidence_level=r.confidence_level,
                step_seconds=r.step_seconds,
            )
            for r in request.requests
        ]
        dc_req = DCForecastBatchRequest(requests=dc_reqs)
        dc_resp = await self._inner.ForecastBatch(dc_req, context)
        return forecasting_pb2.ForecastBatchResponse(
            forecasts=[
                forecasting_pb2.ForecastMetricResponse(
                    agent_id=fr.agent_id,
                    tenant_id=fr.tenant_id,
                    metric_name=fr.metric_name,
                    forecast=[_dc_forecast_point_to_proto(fp) for fp in fr.forecast],
                    model_info=_dc_model_info_to_proto(fr.model_info),
                    overall_confidence=fr.overall_confidence,
                )
                for fr in dc_resp.forecasts
            ]
        )

    async def GetModelAccuracy(self, request: forecasting_pb2.GetModelAccuracyRequest, context: Any) -> forecasting_pb2.GetModelAccuracyResponse:
        dc_req = DCGetModelAccuracyRequest(
            metric_name=request.metric_name,
            tenant_id=request.tenant_id,
        )
        dc_resp = await self._inner.GetModelAccuracy(dc_req, context)
        return forecasting_pb2.GetModelAccuracyResponse(
            models={
                k: _dc_model_info_to_proto(v)
                for k, v in dc_resp.models.items()
            }
        )

    async def RetrainModels(self, request: forecasting_pb2.RetrainModelsRequest, context: Any) -> forecasting_pb2.RetrainModelsResponse:
        dc_req = DCRetrainModelsRequest(
            metric_name=request.metric_name,
            tenant_id=request.tenant_id,
            force=request.force,
        )
        dc_resp = await self._inner.RetrainModels(dc_req, context)
        return forecasting_pb2.RetrainModelsResponse(
            success=dc_resp.success,
            message=dc_resp.message,
            updated_models={
                k: _dc_model_info_to_proto(v)
                for k, v in dc_resp.updated_models.items()
            }
        )


class _AnomalyGrpcServicer(anomaly_pb2_grpc.AnomalyDetectionServiceServicer):
    """Async gRPC servicer that wraps AnomalyServicer.

    Converts between compiled protobuf messages (wire format) and the
    internal dataclass models used by AnomalyServicer.
    """

    def __init__(self, inner: AnomalyServicer) -> None:
        self._inner = inner

    async def DetectAnomalies(self, request: anomaly_pb2.DetectAnomaliesRequest, context: Any) -> anomaly_pb2.DetectAnomaliesResponse:
        dc_req = DCDetectAnomaliesRequest(
            agent_id=request.agent_id,
            tenant_id=request.tenant_id,
            metric_name=request.metric_name,
            values=list(request.values),
            timestamps=list(request.timestamps),
            sensitivity=request.sensitivity,
        )
        dc_resp = await self._inner.DetectAnomalies(dc_req, context)
        return anomaly_pb2.DetectAnomaliesResponse(
            agent_id=dc_resp.agent_id,
            tenant_id=dc_resp.tenant_id,
            metric_name=dc_resp.metric_name,
            anomalies=[_dc_anomaly_to_proto(a) for a in dc_resp.anomalies],
            overall_score=dc_resp.overall_score,
        )

    async def DetectCrossMetricAnomalies(self, request: anomaly_pb2.DetectCrossMetricRequest, context: Any) -> anomaly_pb2.DetectCrossMetricResponse:
        dc_metrics = {
            k: DCFloatArray(values=list(v.values))
            for k, v in request.metrics.items()
        }
        dc_req = DCDetectCrossMetricRequest(
            agent_id=request.agent_id,
            tenant_id=request.tenant_id,
            metrics=dc_metrics,
            timestamps=list(request.timestamps),
        )
        dc_resp = await self._inner.DetectCrossMetricAnomalies(dc_req, context)
        return anomaly_pb2.DetectCrossMetricResponse(
            anomalies=[_dc_anomaly_to_proto(a) for a in dc_resp.anomalies],
            correlation_anomalies=[
                anomaly_pb2.CorrelationAnomaly(
                    metric_a=c.metric_a,
                    metric_b=c.metric_b,
                    description=c.description,
                    severity=c.severity,
                )
                for c in dc_resp.correlation_anomalies
            ],
        )

    async def ExplainAnomaly(self, request: anomaly_pb2.ExplainAnomalyRequest, context: Any) -> anomaly_pb2.ExplainAnomalyResponse:
        dc_req = DCExplainAnomalyRequest(
            agent_id=request.agent_id,
            tenant_id=request.tenant_id,
            metric_name=request.metric_name,
            timestamp=request.timestamp,
        )
        dc_resp = await self._inner.ExplainAnomaly(dc_req, context)
        return anomaly_pb2.ExplainAnomalyResponse(
            anomaly=_dc_anomaly_to_proto(dc_resp.anomaly),
            similar_incidents=dc_resp.similar_incidents,
            recommendations=dc_resp.recommendations,
        )

    async def GetDetectionStatus(self, request: anomaly_pb2.GetDetectionStatusRequest, context: Any) -> anomaly_pb2.GetDetectionStatusResponse:
        dc_req = DCGetDetectionStatusRequest(tenant_id=request.tenant_id)
        dc_resp = await self._inner.GetDetectionStatus(dc_req, context)
        return anomaly_pb2.GetDetectionStatusResponse(
            models={
                k: anomaly_pb2.ModelStatus(
                    name=v.name,
                    trained=v.trained,
                    accuracy=v.accuracy,
                    last_updated=v.last_updated,
                )
                for k, v in dc_resp.models.items()
            },
            last_training=dc_resp.last_training,
            anomalies_detected_24h=dc_resp.anomalies_detected_24h,
            false_positive_rate=dc_resp.false_positive_rate,
        )


# ---------------------------------------------------------------------------
# Health service
# ---------------------------------------------------------------------------


class _HealthServicer:
    """Simple health check endpoint."""

    def __init__(
        self,
        forecasting: ForecastingServicer,
        anomaly: AnomalyServicer,
    ) -> None:
        self._forecasting = forecasting
        self._anomaly = anomaly
        self._start_time = time.time()

    def check(self) -> dict[str, Any]:
        """Return health status."""
        return {
            "status": "healthy",
            "uptime_seconds": int(time.time() - self._start_time),
            "forecasting": self._forecasting.get_health(),
            "anomaly": self._anomaly.get_health(),
        }


# ---------------------------------------------------------------------------
# Server
# ---------------------------------------------------------------------------


def _configure_logging(log_level: str = "INFO") -> None:
    """Configure structlog with JSON output."""
    structlog.configure(
        processors=[
            structlog.contextvars.merge_contextvars,
            structlog.processors.add_log_level,
            structlog.processors.StackInfoRenderer(),
            structlog.dev.set_exc_info,
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.JSONRenderer(),
        ],
        wrapper_class=structlog.make_filtering_bound_logger(
            getattr(__import__("logging"), log_level.upper(), __import__("logging").INFO)
        ),
        context_class=dict,
        logger_factory=structlog.PrintLoggerFactory(),
        cache_logger_on_first_use=True,
    )


def _build_server_credentials() -> grpc.ServerCredentials | None:
    """Build TLS credentials from environment configuration.

    Returns:
        ServerCredentials when INTELLIGENCE_TLS_CERT/KEY are set — with
        client certificate verification (mTLS) when
        INTELLIGENCE_TLS_CLIENT_CA is also set. Returns None when no TLS
        material is configured.

    Raises:
        RuntimeError: when TLS is not configured and PARYTY_DEV_MODE is not
        "true". The architecture mandates mTLS for cluster↔intelligence
        traffic; silently serving plaintext in production would expose
        tenant metric data on the network.
    """
    cert_path = os.getenv("INTELLIGENCE_TLS_CERT", "")
    key_path = os.getenv("INTELLIGENCE_TLS_KEY", "")
    client_ca_path = os.getenv("INTELLIGENCE_TLS_CLIENT_CA", "")

    if cert_path and key_path:
        with open(cert_path, "rb") as f:
            cert_chain = f.read()
        with open(key_path, "rb") as f:
            private_key = f.read()

        if client_ca_path:
            with open(client_ca_path, "rb") as f:
                client_ca = f.read()
            logger.info("tls_configured", mode="mTLS (client certs required)")
            return grpc.ssl_server_credentials(
                [(private_key, cert_chain)],
                root_certificates=client_ca,
                require_client_auth=True,
            )

        logger.info("tls_configured", mode="server-side TLS")
        return grpc.ssl_server_credentials([(private_key, cert_chain)])

    if os.getenv("PARYTY_DEV_MODE", "").lower() == "true":
        logger.warning(
            "tls_disabled",
            reason="PARYTY_DEV_MODE=true — plaintext gRPC; never run this in production",
        )
        return None

    raise RuntimeError(
        "TLS is required: set INTELLIGENCE_TLS_CERT/INTELLIGENCE_TLS_KEY "
        "(and INTELLIGENCE_TLS_CLIENT_CA for mTLS), or PARYTY_DEV_MODE=true "
        "for local development"
    )


async def serve(
    port: int | None = None,
    host: str | None = None,
) -> None:
    """Start the async gRPC server.

    Args:
        port: Port to listen on (default from INTELLIGENCE_PORT or 50051).
        host: Host to bind to (default from INTELLIGENCE_HOST or [::]).
    """
    actual_port = port or int(os.getenv("INTELLIGENCE_PORT", "50051"))
    actual_host = host or os.getenv("INTELLIGENCE_HOST", "[::]")
    log_level = os.getenv("LOG_LEVEL", "INFO")

    _configure_logging(log_level)

    logger.info(
        "intelligence_service_starting",
        host=actual_host,
        port=actual_port,
        log_level=log_level,
    )

    # Create internal servicer implementations
    forecasting_servicer = ForecastingServicer()
    anomaly_servicer = AnomalyServicer()

    # Intelligence Pipeline — always-on engine
    pipeline: Any = None
    dragonfly_client: DragonflyClient | None = None

    health_servicer = _HealthServicer(forecasting_servicer, anomaly_servicer)

    # Create async gRPC server
    server = grpc.aio.server(
        options=[
            ("grpc.max_receive_message_length", 50 * 1024 * 1024),  # 50MB
            ("grpc.max_send_message_length", 50 * 1024 * 1024),
            ("grpc.keepalive_time_ms", 30_000),
            ("grpc.keepalive_timeout_ms", 10_000),
        ],
    )

    # Register compiled gRPC service servicers
    forecasting_pb2_grpc.add_ForecastingServiceServicer_to_server(
        _ForecastingGrpcServicer(forecasting_servicer), server
    )
    anomaly_pb2_grpc.add_AnomalyDetectionServiceServicer_to_server(
        _AnomalyGrpcServicer(anomaly_servicer), server
    )

    logger.info(
        "servicers_registered",
        services=[
            "paryty.v1.ForecastingService",
            "paryty.v1.AnomalyDetectionService",
        ],
    )

    listen_addr = f"{actual_host}:{actual_port}"
    credentials = _build_server_credentials()
    if credentials is not None:
        server.add_secure_port(listen_addr, credentials)
    else:
        server.add_insecure_port(listen_addr)

    logger.info("intelligence_service_ready", address=listen_addr)

    # Start Redpanda streaming (non-fatal if unavailable)
    intel_producer = IntelligenceProducer()
    # Use tenant-scoped topic pattern when pipeline is enabled
    _consumer_topic_pattern = None
    if pipeline_available and PipelineConfig is not None:
        try:
            _pcfg = PipelineConfig.from_env()
            if _pcfg.enabled:
                _consumer_topic_pattern = _pcfg.input_topic_pattern
        except Exception:
            pass
    metric_consumer = MetricConsumer(topic_pattern=_consumer_topic_pattern)
    try:
        intel_producer.connect()
        metric_consumer.connect()

        # Initialize intelligence pipeline (always-on unless explicitly disabled)
        if pipeline_available and PipelineConfig is not None and IntelligencePipeline is not None:
            try:
                pipeline_cfg = PipelineConfig.from_env()
                if pipeline_cfg.enabled:  # killswitch — set PARYTY_INTEL_ENABLED=false to disable
                    dragonfly_client = DragonflyClient()
                    pipeline = IntelligencePipeline(
                        anomaly_servicer=anomaly_servicer,
                        forecasting_servicer=forecasting_servicer,
                        dragonfly=dragonfly_client,
                        producer=intel_producer,
                        config=pipeline_cfg,
                    )
                    pipeline.start()
                    logger.info("intelligence_pipeline_initialized")
            except Exception as exc:
                logger.warning("pipeline_init_failed", error=str(exc))

        def _on_metric(msg: dict) -> None:
            """Callback for consumed metrics — feed to anomaly detector."""
            try:
                tenant_id = msg.get("tenant_id", "")
                agent_id = msg.get("agent_id", "")
                metric_name = msg.get("metric_name", "")
                value = msg.get("value", 0.0)
                # Store in anomaly servicer for real-time detection
                logger.debug(
                    "metric_consumed",
                    tenant=tenant_id,
                    agent=agent_id,
                    metric=metric_name,
                )
            except Exception as exc:
                logger.warning("metric_consume_handler_error", error=str(exc))

        # Wire the consumer callback: pipeline if available, stub otherwise
        if pipeline is not None:
            metric_consumer.start(pipeline.on_metric)
        else:
            metric_consumer.start(_on_metric)
        logger.info("streaming_started", pipeline_active=pipeline is not None)
    except Exception as exc:
        logger.warning("streaming_start_failed", error=str(exc))

    # Graceful shutdown handler
    shutdown_event = asyncio.Event()

    def _signal_handler() -> None:
        logger.info("shutdown_signal_received")
        shutdown_event.set()

    loop = asyncio.get_running_loop()
    if sys.platform != "win32":
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(sig, _signal_handler)

    # Start server
    await server.start()

    # Wait for shutdown
    logger.info(
        "intelligence_service_running",
        port=actual_port,
        pid=os.getpid(),
        health=str(health_servicer.check()),
    )

    try:
        await shutdown_event.wait()
    except asyncio.CancelledError:
        pass
    finally:
        logger.info("intelligence_service_stopping")
        # Stop pipeline first (before consumer/producer)
        if pipeline is not None:
            try:
                pipeline.stop()
            except Exception as exc:
                logger.warning("pipeline_stop_failed", error=str(exc))
        # Stop streaming
        metric_consumer.stop()
        intel_producer.close()
        # Disconnect Dragonfly
        if dragonfly_client is not None:
            try:
                dragonfly_client.disconnect()
            except Exception:
                pass
        # Graceful shutdown with 10s drain
        await server.stop(grace=10)
        logger.info("intelligence_service_stopped")


def main() -> None:
    """Entry point for the intelligence service."""
    try:
        asyncio.run(serve())
    except KeyboardInterrupt:
        logger.info("keyboard_interrupt_received")
    except Exception as exc:
        logger.error("intelligence_service_fatal", error=str(exc))
        sys.exit(1)


if __name__ == "__main__":
    main()
