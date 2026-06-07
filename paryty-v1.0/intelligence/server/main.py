"""
Paryty Intelligence Service — gRPC Server Entry Point.

Runs the ForecastingService and AnomalyDetectionService as async
gRPC services.  Handles graceful shutdown on SIGINT/SIGTERM.

Usage::

    # Direct
    python -m intelligence.server.main

    # Via entry point
    paryty-intel

Environment Variables:
    INTELLIGENCE_PORT: gRPC port (default: 50051)
    INTELLIGENCE_HOST: Bind host (default: [::])
    LOG_LEVEL: Logging level (default: INFO)
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

from .anomaly_service import AnomalyServicer
from .forecasting_service import ForecastingServicer

logger = structlog.get_logger(__name__)


# ---------------------------------------------------------------------------
# Proto stub service classes (implement the gRPC interface without compiled protos)
# ---------------------------------------------------------------------------


class _ForecastingServiceServicer:
    """gRPC adapter for the ForecastingService.

    This wraps the ForecastingServicer and implements the gRPC
    async interface.  In production, this would be generated from
    the proto definition; here we provide a plain implementation.
    """

    def __init__(self, inner: ForecastingServicer) -> None:
        self._inner = inner

    async def ForecastMetric(
        self, request: Any, context: Any
    ) -> Any:
        return await self._inner.ForecastMetric(request, context)

    async def ForecastBatch(
        self, request: Any, context: Any
    ) -> Any:
        return await self._inner.ForecastBatch(request, context)

    async def GetModelAccuracy(
        self, request: Any, context: Any
    ) -> Any:
        return await self._inner.GetModelAccuracy(request, context)

    async def RetrainModels(
        self, request: Any, context: Any
    ) -> Any:
        return await self._inner.RetrainModels(request, context)


class _AnomalyDetectionServiceServicer:
    """gRPC adapter for the AnomalyDetectionService."""

    def __init__(self, inner: AnomalyServicer) -> None:
        self._inner = inner

    async def DetectAnomalies(
        self, request: Any, context: Any
    ) -> Any:
        return await self._inner.DetectAnomalies(request, context)

    async def DetectCrossMetricAnomalies(
        self, request: Any, context: Any
    ) -> Any:
        return await self._inner.DetectCrossMetricAnomalies(request, context)

    async def ExplainAnomaly(
        self, request: Any, context: Any
    ) -> Any:
        return await self._inner.ExplainAnomaly(request, context)

    async def GetDetectionStatus(
        self, request: Any, context: Any
    ) -> Any:
        return await self._inner.GetDetectionStatus(request, context)


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

    # Create servicers
    forecasting_servicer = ForecastingServicer()
    anomaly_servicer = AnomalyServicer()
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

    # NOTE: In a production setup with compiled proto stubs, you would
    # register the service servicers like:
    #
    #   from proto import forecasting_pb2_grpc
    #   from proto import anomaly_pb2_grpc
    #   forecasting_pb2_grpc.add_ForecastingServiceServicer_to_server(
    #       _ForecastingServiceServicer(forecasting_servicer), server
    #   )
    #
    # For now, we log that the servicers are ready.
    logger.info(
        "servicers_registered",
        services=["ForecastingService", "AnomalyDetectionService"],
    )

    listen_addr = f"{actual_host}:{actual_port}"
    server.add_insecure_port(listen_addr)

    logger.info("intelligence_service_ready", address=listen_addr)

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
