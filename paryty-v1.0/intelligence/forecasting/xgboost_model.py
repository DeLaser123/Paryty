"""
XGBoost forecasting layer.

Provides multi-variable pattern recognition for complex metric interactions.
Best for: complex non-linear patterns, multi-metric forecasting.
NOT good for: simple trends (overkill), very small datasets (<1000 points).

ALGORITHM:
    1. Engineer features (lag values, rolling stats, time features)
    2. Train XGBoost with time-series cross-validation
    3. Predict future values using recursive forecasting
    4. Estimate uncertainty via prediction interval widening

PERFORMANCE: 5-10 seconds per fit, <10ms per predict
MIN DATA: 1000 points
"""

from __future__ import annotations

import time
from typing import Any

import numpy as np
import pandas as pd
import structlog
import xgboost as xgb
from sklearn.metrics import mean_absolute_percentage_error

from .linear import ForecastResult

logger = structlog.get_logger(__name__)

# z-score for 95% CI
_Z_95 = 1.96


class XGBoostForecaster:
    """XGBoost-based forecaster with rich feature engineering.

    Features:
        * **Lag values**: value at t-1, t-5, t-15, t-60
        * **Rolling statistics**: mean, std, min, max over windows [5, 15, 60]
        * **Time features**: hour-of-day, day-of-week, minute-of-hour (normalised)
        * **Trend**: slope of last 10 observations

    Multi-step forecasting uses a *recursive* strategy: predict one step
    ahead, append the prediction, and recompute features for the next step.
    """

    def __init__(
        self,
        n_estimators: int = 100,
        max_depth: int = 6,
        learning_rate: float = 0.1,
        lag_features: list[int] | None = None,
        rolling_windows: list[int] | None = None,
    ) -> None:
        self.n_estimators = n_estimators
        self.max_depth = max_depth
        self.learning_rate = learning_rate
        self.lag_features: list[int] = lag_features or [1, 5, 15, 60]
        self.rolling_windows: list[int] = rolling_windows or [5, 15, 60]

        self._model: xgb.XGBRegressor | None = None
        self._feature_names: list[str] = []
        self._last_values: list[float] = []
        self._last_timestamps: list[int] = []
        self._val_mape: float = 100.0
        self._t_start: float = 0.0

    # ------------------------------------------------------------------
    # Feature engineering
    # ------------------------------------------------------------------

    def _engineer_features(
        self,
        values: np.ndarray,
        timestamps: np.ndarray,
    ) -> tuple[np.ndarray, list[str]]:
        """Build feature matrix from raw time-series.

        Returns (X, feature_names).
        """
        n = len(values)
        features: list[np.ndarray] = []
        names: list[str] = []

        # Lag features
        for lag in self.lag_features:
            lag_arr = np.roll(values, lag)
            lag_arr[:lag] = values[0]
            features.append(lag_arr)
            names.append(f"lag_{lag}")

        # Rolling statistics
        for window in self.rolling_windows:
            for agg_fn, suffix in [
                (np.mean, "mean"),
                (np.std, "std"),
                (np.min, "min"),
                (np.max, "max"),
            ]:
                rolling = np.array(
                    [agg_fn(values[max(0, i - window) : i + 1]) for i in range(n)],
                    dtype=np.float64,
                )
                features.append(rolling)
                names.append(f"rolling_{window}_{suffix}")

        # Time features (normalised 0-1)
        dt = pd.to_datetime(timestamps, unit="s")
        features.append(dt.hour.values / 23.0)
        names.append("hour_of_day")
        features.append(dt.dayofweek.values / 6.0)
        names.append("day_of_week")
        features.append(dt.minute.values / 59.0)
        names.append("minute_of_hour")

        # Trend – slope of last 10 values
        trend = np.zeros(n, dtype=np.float64)
        for i in range(10, n):
            slope = float(np.polyfit(np.arange(10), values[i - 10 : i], 1)[0])
            trend[i] = slope
        features.append(trend)
        names.append("trend")

        X = np.column_stack(features)
        return X, names

    def _extract_predict_features(
        self,
        values: np.ndarray,
        timestamp: int,
    ) -> np.ndarray:
        """Extract features for a single prediction step.

        Uses the most recent values to compute lag, rolling, and time features.
        """
        feat: list[float] = []

        # Lag features
        for lag in self.lag_features:
            idx = max(len(values) - lag - 1, 0)
            feat.append(float(values[idx]))

        # Rolling statistics
        for window in self.rolling_windows:
            tail = values[max(0, len(values) - window) :]
            feat.append(float(np.mean(tail)))
            feat.append(float(np.std(tail)) if len(tail) > 1 else 0.0)
            feat.append(float(np.min(tail)))
            feat.append(float(np.max(tail)))

        # Time features
        dt = pd.Timestamp(timestamp, unit="s")
        feat.append(dt.hour / 23.0)
        feat.append(dt.dayofweek / 6.0)
        feat.append(dt.minute / 59.0)

        # Trend
        if len(values) >= 10:
            slope = float(
                np.polyfit(np.arange(10), values[-10:], 1)[0]
            )
        else:
            slope = 0.0
        feat.append(slope)

        return np.array(feat, dtype=np.float64)

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def fit(self, timestamps: list[int], values: list[float]) -> None:
        """Train XGBoost model with time-series split.

        Args:
            timestamps: Unix timestamps (seconds).
            values: Metric values.

        Raises:
            ValueError: Fewer than 1000 data points.
        """
        n = len(values)
        if n < 1000:
            raise ValueError(
                f"XGBoost requires >=1000 data points, got {n}"
            )

        values_arr = np.asarray(values, dtype=np.float64)
        timestamps_arr = np.asarray(timestamps, dtype=np.int64)

        # Feature engineering
        X, names = self._engineer_features(values_arr, timestamps_arr)
        self._feature_names = names

        # Time-series train/val split (last 10% as validation)
        split_idx = int(len(X) * 0.9)
        X_train, X_val = X[:split_idx], X[split_idx:]
        y_train, y_val = values_arr[:split_idx], values_arr[split_idx:]

        # Train
        self._model = xgb.XGBRegressor(
            n_estimators=self.n_estimators,
            max_depth=self.max_depth,
            learning_rate=self.learning_rate,
            objective="reg:squarederror",
            n_jobs=-1,
            verbosity=0,
            random_state=42,
        )
        self._model.fit(
            X_train,
            y_train,
            eval_set=[(X_val, y_val)],
            verbose=False,
        )

        # Validation MAPE
        val_pred = self._model.predict(X_val)
        self._val_mape = float(mean_absolute_percentage_error(y_val, val_pred) * 100)

        # Store tail for recursive forecasting
        max_lag = max(self.lag_features)
        self._last_values = list(values[-max(max_lag, max(self.rolling_windows)) :])
        self._last_timestamps = list(timestamps[-max(max_lag, max(self.rolling_windows)) :])
        self._t_start = float(timestamps_arr[0])

        logger.info(
            "xgboost_forecaster_fitted",
            data_points=n,
            features=len(names),
            val_mape=f"{self._val_mape:.2f}%",
        )

    def predict(
        self,
        horizon_seconds: int,
        step_seconds: int = 60,
    ) -> ForecastResult:
        """Generate forecast using recursive prediction.

        Args:
            horizon_seconds: Forecast horizon in seconds.
            step_seconds: Interval between points.

        Returns:
            :class:`ForecastResult`.

        Raises:
            RuntimeError: Model not fitted.
        """
        if self._model is None:
            raise RuntimeError("Model not fitted – call fit() first.")

        now = int(time.time())
        n_steps = max(1, horizon_seconds // step_seconds)

        # Recursive forecasting
        predicted_values: list[float] = []
        current_values = list(self._last_values)
        current_timestamps = list(self._last_timestamps)

        for i in range(n_steps):
            ts = now + i * step_seconds
            feat = self._extract_predict_features(
                np.array(current_values, dtype=np.float64), ts
            )
            pred = float(self._model.predict(feat.reshape(1, -1))[0])
            predicted_values.append(pred)
            current_values.append(pred)
            current_timestamps.append(ts)

        # Confidence intervals (widening with distance)
        preds = np.array(predicted_values, dtype=np.float64)
        residuals_std = self._val_mape / 100.0 * np.abs(preds)
        distances = np.arange(1, n_steps + 1, dtype=np.float64)
        widening = 1.0 + distances / max(n_steps, 1)
        margin = _Z_95 * residuals_std * widening

        lower = preds - margin
        upper = preds + margin

        # Confidence decays with distance
        mean_abs = float(np.mean(np.abs(preds))) + 1e-9
        base_conf = max(0.3, 1.0 - self._val_mape / 100.0)
        step_conf = base_conf * (1.0 - distances / (n_steps * 2))
        overall_confidence = float(np.clip(np.mean(step_conf), 0.1, 0.95))

        future_timestamps = [now + i * step_seconds for i in range(n_steps)]

        return ForecastResult(
            timestamps=future_timestamps,
            values=preds.tolist(),
            lower_bound=lower.tolist(),
            upper_bound=upper.tolist(),
            confidence=overall_confidence,
            model_name="xgboost",
        )

    # ------------------------------------------------------------------
    # Introspection
    # ------------------------------------------------------------------

    @property
    def is_fitted(self) -> bool:
        """True if the model has been fitted."""
        return self._model is not None

    @property
    def feature_importance(self) -> dict[str, float]:
        """Return feature importance as name→importance mapping."""
        if self._model is None:
            return {}
        importances = self._model.feature_importances_
        return dict(zip(self._feature_names, importances.tolist()))

    @property
    def validation_mape(self) -> float:
        """MAPE on the held-out validation set."""
        return self._val_mape

    def compute_mape(
        self,
        actual_timestamps: list[int],
        actual_values: list[float],
    ) -> float:
        """Compute MAPE against actuals (single-step per point)."""
        if self._model is None or len(actual_values) == 0:
            return 100.0

        values_arr = np.asarray(actual_values, dtype=np.float64)
        timestamps_arr = np.asarray(actual_timestamps, dtype=np.int64)

        # Build features using available history + actuals
        combined_vals = np.concatenate(
            [np.array(self._last_values, dtype=np.float64), values_arr]
        )
        combined_ts = np.concatenate(
            [np.array(self._last_timestamps, dtype=np.int64), timestamps_arr]
        )
        X, _ = self._engineer_features(combined_vals, combined_ts)
        X_eval = X[len(self._last_values) :]
        preds = self._model.predict(X_eval)

        denom = np.where(np.abs(values_arr) < 1e-9, 1e-9, np.abs(values_arr))
        return float(np.mean(np.abs(values_arr - preds) / denom) * 100.0)
