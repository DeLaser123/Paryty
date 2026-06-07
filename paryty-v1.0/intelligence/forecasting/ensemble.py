"""
Weighted ensemble combining Linear, Prophet, and XGBoost forecasters.

Uses inverse-MAPE weighting: models with lower error get higher weight.
Weights are clamped to [min_weight, max_weight] and normalised to sum
to 1.0.  When a model fails, its weight is redistributed proportionally.

Maintains a rolling accuracy history (168 hourly entries = 7 days).
"""

from __future__ import annotations

import time
from collections import deque
from typing import Any

import numpy as np
import structlog

from .config import ForecastingConfig
from .linear import ForecastResult, LinearForecaster
from .prophet import ProphetForecaster
from .xgboost_model import XGBoostForecaster

logger = structlog.get_logger(__name__)

_MODEL_NAMES = ("linear", "prophet", "xgboost")


class ForecastEnsemble:
    """Weighted ensemble of Linear + Prophet + XGBoost forecasters.

    **Weighting strategy:**
        1. Compute per-model MAPE over recent predictions.
        2. Inverse-MAPE → raw weight (∝ 1 / MAPE).
        3. Clamp each weight to [min_weight, max_weight].
        4. Normalise so weights sum to 1.0.

    When a model raises during fit/predict its weight is set to 0 and
    the remaining weights are renormalised.
    """

    def __init__(self, config: ForecastingConfig | None = None) -> None:
        self._config = config or ForecastingConfig()
        self._linear = LinearForecaster(
            window_size=self._config.linear.window_size,
            confidence_level=self._config.linear.confidence_level,
        )
        self._prophet = ProphetForecaster(
            changepoint_prior_scale=self._config.prophet.changepoint_prior_scale,
            confidence_level=self._config.prophet.confidence_level,
        )
        self._xgboost = XGBoostForecaster(
            n_estimators=self._config.xgboost.n_estimators,
            max_depth=self._config.xgboost.max_depth,
            learning_rate=self._config.xgboost.learning_rate,
            lag_features=self._config.xgboost.lag_features,
            rolling_windows=self._config.xgboost.rolling_windows,
        )

        ec = self._config.ensemble
        self._weights: dict[str, float] = dict(ec.default_weights)
        self._accuracy_history: dict[str, deque[float]] = {
            name: deque(maxlen=ec.accuracy_history_size) for name in _MODEL_NAMES
        }
        self._last_fit_ts: int = 0
        self._last_predict_ts: int = 0
        self._n_training_points: int = 0

    # ------------------------------------------------------------------
    # Fitting
    # ------------------------------------------------------------------

    def fit_all(
        self,
        timestamps: list[int],
        values: list[float],
    ) -> dict[str, bool]:
        """Fit all sub-models, tolerating individual failures.

        Args:
            timestamps: Unix timestamps.
            values: Metric values.

        Returns:
            Mapping of model_name → success (True/False).
        """
        results: dict[str, bool] = {}
        mapes: dict[str, float] = {}

        # --- Linear --------------------------------------------------
        try:
            self._linear.fit(timestamps, values)
            results["linear"] = True
        except Exception as exc:
            logger.warning("linear_fit_failed", error=str(exc))
            results["linear"] = False

        # --- Prophet ------------------------------------------------
        try:
            self._prophet.fit(timestamps, values)
            results["prophet"] = True
        except Exception as exc:
            logger.warning("prophet_fit_failed", error=str(exc))
            results["prophet"] = False

        # --- XGBoost ------------------------------------------------
        try:
            self._xgboost.fit(timestamps, values)
            results["xgboost"] = True
        except Exception as exc:
            logger.warning("xgboost_fit_failed", error=str(exc))
            results["xgboost"] = False

        # Compute per-model MAPE on a hold-out tail (last 10%)
        n = len(values)
        holdout_start = max(0, int(n * 0.9))
        if holdout_start < n:
            h_ts = timestamps[holdout_start:]
            h_vals = values[holdout_start:]
            for name, model in [
                ("linear", self._linear),
                ("prophet", self._prophet),
                ("xgboost", self._xgboost),
            ]:
                if results[name]:
                    try:
                        mape = model.compute_mape(h_ts, h_vals)
                    except Exception:
                        mape = 100.0
                    mapes[name] = mape
                    self._accuracy_history[name].append(mape)

        self.update_weights()
        self._last_fit_ts = int(time.time())
        self._n_training_points = len(values)

        logger.info(
            "ensemble_fitted",
            models=results,
            weights=self._weights,
            mapes=mapes,
            data_points=len(values),
        )
        return results

    # ------------------------------------------------------------------
    # Weight management
    # ------------------------------------------------------------------

    def update_weights(self) -> dict[str, float]:
        """Recalculate weights based on rolling MAPE history.

        Returns the updated weight dictionary.
        """
        ec = self._config.ensemble
        inverse_mapes: dict[str, float] = {}

        for name in _MODEL_NAMES:
            history = self._accuracy_history[name]
            if history:
                avg_mape = float(np.mean(history))
                avg_mape = max(avg_mape, 0.01)  # avoid division by zero
                inverse_mapes[name] = 1.0 / avg_mape
            else:
                inverse_mapes[name] = 1.0  # equal weight if no history

        total = sum(inverse_mapes.values())
        if total == 0:
            self._weights = dict(ec.default_weights)
            return self._weights

        raw_weights = {k: v / total for k, v in inverse_mapes.items()}

        # Clamp
        for name in _MODEL_NAMES:
            raw_weights[name] = max(
                ec.min_weight, min(ec.max_weight, raw_weights[name])
            )

        # Renormalise
        total = sum(raw_weights.values())
        if total > 0:
            self._weights = {k: v / total for k, v in raw_weights.items()}
        else:
            self._weights = dict(ec.default_weights)

        return self._weights

    # ------------------------------------------------------------------
    # Prediction
    # ------------------------------------------------------------------

    def predict(
        self,
        horizon_seconds: int,
        step_seconds: int = 60,
    ) -> ForecastResult:
        """Generate a weighted-ensemble forecast.

        Individual model predictions are weighted by their inverse-MAPE
        score.  If a model fails, its weight is redistributed to the
        survivors.

        Args:
            horizon_seconds: Forecast horizon.
            step_seconds: Step between forecast points.

        Returns:
            Combined :class:`ForecastResult`.
        """
        predictions: dict[str, ForecastResult] = {}
        active_weights: dict[str, float] = {}

        for name, model in [
            ("linear", self._linear),
            ("prophet", self._prophet),
            ("xgboost", self._xgboost),
        ]:
            if not model.is_fitted:
                continue
            try:
                result = model.predict(horizon_seconds, step_seconds)
                predictions[name] = result
                active_weights[name] = self._weights.get(name, 1 / 3)
            except Exception as exc:
                logger.warning("model_predict_failed", model=name, error=str(exc))

        if not predictions:
            raise RuntimeError("All sub-models failed – cannot produce forecast.")

        # Redistribute weights among active models
        w_sum = sum(active_weights.values())
        if w_sum > 0:
            active_weights = {k: v / w_sum for k, v in active_weights.items()}

        # Weighted combination
        first = next(iter(predictions.values()))
        n_steps = len(first.timestamps)
        combined_values = np.zeros(n_steps, dtype=np.float64)
        combined_lower = np.zeros(n_steps, dtype=np.float64)
        combined_upper = np.zeros(n_steps, dtype=np.float64)
        combined_confidence = 0.0

        for name, result in predictions.items():
            w = active_weights[name]
            combined_values += w * np.array(result.values, dtype=np.float64)
            combined_lower += w * np.array(result.lower_bound, dtype=np.float64)
            combined_upper += w * np.array(result.upper_bound, dtype=np.float64)
            combined_confidence += w * result.confidence

        self._last_predict_ts = int(time.time())

        return ForecastResult(
            timestamps=first.timestamps,
            values=combined_values.tolist(),
            lower_bound=combined_lower.tolist(),
            upper_bound=combined_upper.tolist(),
            confidence=float(np.clip(combined_confidence, 0.1, 0.95)),
            model_name="ensemble",
        )

    # ------------------------------------------------------------------
    # Accuracy tracking
    # ------------------------------------------------------------------

    def get_accuracy(self) -> dict[str, Any]:
        """Return current weights, per-model MAPE, and history depth."""
        avg_mapes: dict[str, float] = {}
        for name in _MODEL_NAMES:
            history = self._accuracy_history[name]
            avg_mapes[name] = float(np.mean(history)) if history else 100.0
        return {
            "weights": dict(self._weights),
            "mapes": avg_mapes,
            "history_depth": {
                name: len(self._accuracy_history[name]) for name in _MODEL_NAMES
            },
            "last_fit_ts": self._last_fit_ts,
            "last_predict_ts": self._last_predict_ts,
            "training_points": self._n_training_points,
        }

    # ------------------------------------------------------------------
    # Accessors
    # ------------------------------------------------------------------

    @property
    def weights(self) -> dict[str, float]:
        """Current ensemble weights."""
        return dict(self._weights)

    @property
    def is_fitted(self) -> bool:
        """True if at least one sub-model is fitted."""
        return any(
            m.is_fitted
            for m in (self._linear, self._prophet, self._xgboost)
        )

    @property
    def linear(self) -> LinearForecaster:
        """Access the linear forecaster."""
        return self._linear

    @property
    def prophet(self) -> ProphetForecaster:
        """Access the prophet forecaster."""
        return self._prophet

    @property
    def xgboost(self) -> XGBoostForecaster:
        """Access the xgboost forecaster."""
        return self._xgboost
