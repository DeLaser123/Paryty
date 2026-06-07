"""
gRPC ForecastingService implementation.

Implements the ForecastingService defined in proto/paryty/v1/forecasting.proto.
Provides metric forecasting via the weighted ensemble of Linear Regression,
Prophet, and XGBoost models.

All operations are tenant-scoped.  Models are cached in-memory after
first training and persisted to disk via joblib.
"""

from __future__ import annotations

import asyncio
import time
from concurrent.futures import ThreadPoolExecutor
from typing import Any

import grpc
import structlog

from ..data.questdb_client import QuestDBClient
from ..forecasting.config import ForecastingConfig
from ..forecasting.ensemble import ForecastEnsemble
from ..forecasting.model_store import ForecastModelStore
from ..proto.models import (
    ForecastBatchRequest,
    ForecastBatchResponse,
    ForecastMetricRequest,
    ForecastMetricResponse,
    ForecastPoint,
    GetModelAccuracyRequest,
    GetModelAccuracyResponse,
    ModelInfo,
    RetrainModelsRequest,
    RetrainModelsResponse,
)

logger = structlog.get_logger(__name__)

# Thread pool for CPU-bound ML work
_ML_EXECUTOR = ThreadPoolExecutor(max_workers=4, thread_name_prefix="ml-forecast")


class ForecastingServicer:
    """gRPC servicer for the ForecastingService.

    Each tenant + metric combination gets its own :class:`ForecastEnsemble`
    instance, cached in-memory.  Models are loaded from disk on first
    request and saved after retraining.
    """

    def __init__(
        self,
        config: ForecastingConfig | None = None,
        questdb: QuestDBClient | None = None,
        model_store: ForecastModelStore | None = None,
    ) -> None:
        self._config = config or ForecastingConfig.from_env()
        self._questdb = questdb or QuestDBClient()
        self._model_store = model_store or ForecastModelStore(
            self._config.model_store.base_dir
        )
        # Cache: (tenant_id, metric_name) → ForecastEnsemble
        self._ensembles: dict[tuple[str, str], ForecastEnsemble] = {}
        self._last_predict_time: float = 0.0

    # ------------------------------------------------------------------
    # gRPC methods
    # ------------------------------------------------------------------

    async def ForecastMetric(
        self,
        request: ForecastMetricRequest,
        context: grpc.aio.ServicerContext | None = None,
    ) -> ForecastMetricResponse:
        """Forecast a single metric.

        1. Fetch historical data from QuestDB.
        2. Train ensemble (or use cached).
        3. Generate forecast.
        4. Return ForecastMetricResponse.
        """
        try:
            # Validate
            if not request.tenant_id:
                return self._error_response(
                    request, "tenant_id is required"
                )
            if not request.metric_name:
                return self._error_response(
                    request, "metric_name is required"
                )

            horizon = request.horizon_seconds or self._config.default_horizon_seconds
            step = request.step_seconds or self._config.default_step_seconds

            # Run in thread pool (CPU-bound)
            loop = asyncio.get_event_loop()
            return await loop.run_in_executor(
                _ML_EXECUTOR,
                self._forecast_sync,
                request.agent_id,
                request.tenant_id,
                request.metric_name,
                horizon,
                step,
            )

        except Exception as exc:
            logger.error(
                "forecast_metric_failed",
                agent=request.agent_id,
                metric=request.metric_name,
                error=str(exc),
            )
            return self._error_response(request, str(exc))

    async def ForecastBatch(
        self,
        request: ForecastBatchRequest,
        context: grpc.aio.ServicerContext | None = None,
    ) -> ForecastBatchResponse:
        """Forecast multiple metrics in parallel."""
        loop = asyncio.get_event_loop()
        tasks = [
            loop.run_in_executor(
                _ML_EXECUTOR,
                self._forecast_sync,
                req.agent_id,
                req.tenant_id,
                req.metric_name,
                req.horizon_seconds or self._config.default_horizon_seconds,
                req.step_seconds or self._config.default_step_seconds,
            )
            for req in request.requests
        ]
        results = await asyncio.gather(*tasks, return_exceptions=True)

        forecasts: list[ForecastMetricResponse] = []
        for i, result in enumerate(results):
            if isinstance(result, Exception):
                logger.warning(
                    "batch_forecast_item_failed",
                    index=i,
                    error=str(result),
                )
                forecasts.append(
                    self._error_response(request.requests[i], str(result))
                )
            else:
                forecasts.append(result)

        return ForecastBatchResponse(forecasts=forecasts)

    async def GetModelAccuracy(
        self,
        request: GetModelAccuracyRequest,
        context: grpc.aio.ServicerContext | None = None,
    ) -> GetModelAccuracyResponse:
        """Return model accuracy metrics."""
        models: dict[str, ModelInfo] = {}

        if request.metric_name:
            key = (request.tenant_id, request.metric_name)
            ensemble = self._ensembles.get(key)
            if ensemble is not None:
                acc = ensemble.get_accuracy()
                models[request.metric_name] = ModelInfo(
                    best_model="ensemble",
                    weights=acc.get("weights", {}),
                    accuracy=acc.get("mapes", {}),
                    last_trained=acc.get("last_fit_ts", 0),
                    training_samples=acc.get("training_points", 0),
                )
        else:
            for (tenant_id, metric_name), ensemble in self._ensembles.items():
                if tenant_id == request.tenant_id or not request.tenant_id:
                    acc = ensemble.get_accuracy()
                    models[metric_name] = ModelInfo(
                        best_model="ensemble",
                        weights=acc.get("weights", {}),
                        accuracy=acc.get("mapes", {}),
                        last_trained=acc.get("last_fit_ts", 0),
                        training_samples=acc.get("training_points", 0),
                    )

        return GetModelAccuracyResponse(models=models)

    async def RetrainModels(
        self,
        request: RetrainModelsRequest,
        context: grpc.aio.ServicerContext | None = None,
    ) -> RetrainModelsResponse:
        """Trigger model retraining."""
        try:
            loop = asyncio.get_event_loop()
            return await loop.run_in_executor(
                _ML_EXECUTOR,
                self._retrain_sync,
                request.tenant_id,
                request.metric_name,
                request.force,
            )
        except Exception as exc:
            logger.error("retrain_failed", error=str(exc))
            return RetrainModelsResponse(
                success=False,
                message=f"Retraining failed: {exc}",
            )

    # ------------------------------------------------------------------
    # Internal synchronous methods (run in thread pool)
    # ------------------------------------------------------------------

    def _forecast_sync(
        self,
        agent_id: str,
        tenant_id: str,
        metric_name: str,
        horizon_seconds: int,
        step_seconds: int,
    ) -> ForecastMetricResponse:
        """Synchronous forecast logic."""
        start_time = time.monotonic()

        # Get or create ensemble
        ensemble = self._get_ensemble(tenant_id, metric_name)

        # Fetch training data if not yet fitted
        if not ensemble.is_fitted:
            timestamps, values = self._questdb.query_metric_history(
                metric_name=metric_name,
                tenant_id=tenant_id,
                hours_back=168,  # 7 days
                agent_id=agent_id if agent_id else None,
            )
            if len(values) < 3:
                return ForecastMetricResponse(
                    agent_id=agent_id,
                    tenant_id=tenant_id,
                    metric_name=metric_name,
                    overall_confidence=0.0,
                )

            ensemble.fit_all(timestamps, values)

        # Predict
        result = ensemble.predict(horizon_seconds, step_seconds)

        # Build response
        forecast_points = [
            ForecastPoint(
                timestamp=ts,
                value=val,
                lower_bound=lb,
                upper_bound=ub,
            )
            for ts, val, lb, ub in zip(
                result.timestamps,
                result.values,
                result.lower_bound,
                result.upper_bound,
            )
        ]

        acc = ensemble.get_accuracy()
        model_info = ModelInfo(
            best_model=result.model_name,
            weights=acc.get("weights", {}),
            accuracy=acc.get("mapes", {}),
            last_trained=acc.get("last_fit_ts", 0),
            training_samples=acc.get("training_points", 0),
        )

        elapsed = time.monotonic() - start_time
        self._last_predict_time = time.time()

        logger.info(
            "forecast_completed",
            agent=agent_id,
            metric=metric_name,
            horizon=horizon_seconds,
            points=len(forecast_points),
            confidence=result.confidence,
            latency_ms=round(elapsed * 1000, 1),
        )

        return ForecastMetricResponse(
            agent_id=agent_id,
            tenant_id=tenant_id,
            metric_name=metric_name,
            forecast=forecast_points,
            model_info=model_info,
            overall_confidence=result.confidence,
        )

    def _retrain_sync(
        self,
        tenant_id: str,
        metric_name: str,
        force: bool,
    ) -> RetrainModelsResponse:
        """Synchronous retrain logic."""
        updated: dict[str, ModelInfo] = {}

        if metric_name:
            metrics = [metric_name]
        else:
            metrics = self._questdb.query_available_metrics(tenant_id)

        for m in metrics:
            try:
                key = (tenant_id, m)
                ensemble = self._get_ensemble(tenant_id, m)

                timestamps, values = self._questdb.query_metric_history(
                    metric_name=m,
                    tenant_id=tenant_id,
                    hours_back=168,
                )
                if len(values) < 3:
                    continue

                ensemble.fit_all(timestamps, values)

                acc = ensemble.get_accuracy()
                updated[m] = ModelInfo(
                    best_model="ensemble",
                    weights=acc.get("weights", {}),
                    accuracy=acc.get("mapes", {}),
                    last_trained=acc.get("last_fit_ts", 0),
                    training_samples=acc.get("training_points", 0),
                )

            except Exception as exc:
                logger.warning(
                    "retrain_metric_failed",
                    metric=m,
                    error=str(exc),
                )

        return RetrainModelsResponse(
            success=True,
            message=f"Retrained {len(updated)} models",
            updated_models=updated,
        )

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _get_ensemble(self, tenant_id: str, metric_name: str) -> ForecastEnsemble:
        """Get or create a cached ensemble for tenant+metric."""
        key = (tenant_id, metric_name)
        if key not in self._ensembles:
            self._ensembles[key] = ForecastEnsemble(self._config)
        return self._ensembles[key]

    @staticmethod
    def _error_response(
        request: ForecastMetricRequest,
        message: str,
    ) -> ForecastMetricResponse:
        """Build an empty error response."""
        logger.warning("forecast_error", metric=request.metric_name, error=message)
        return ForecastMetricResponse(
            agent_id=request.agent_id,
            tenant_id=request.tenant_id,
            metric_name=request.metric_name,
            overall_confidence=0.0,
        )

    def get_health(self) -> dict[str, Any]:
        """Return health status for this servicer."""
        return {
            "service": "forecasting",
            "ensembles_cached": len(self._ensembles),
            "last_predict_time": self._last_predict_time,
            "models_available": self._model_store.list_models(),
        }
