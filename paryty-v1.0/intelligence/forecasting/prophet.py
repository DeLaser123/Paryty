"""
Prophet forecasting layer.

Provides seasonal pattern detection and forecasting.
Best for: daily/weekly cycles, holiday effects, trend changes.
NOT good for: real-time (<100ms) predictions (takes 1-5 seconds to fit).

ALGORITHM:
    1. Convert metric data to Prophet DataFrame
    2. Fit Prophet model (detects daily/weekly seasonality automatically)
    3. Predict future values with uncertainty intervals
    4. Detect changepoints (sudden trend changes)

PERFORMANCE: 1-5 seconds per fit, <100ms per predict
MIN DATA: 1440 points (1 day at 1-min intervals)
"""

from __future__ import annotations

import time
import warnings
from typing import Any

import numpy as np
import pandas as pd
import structlog

from .linear import ForecastResult

logger = structlog.get_logger(__name__)


class ProphetForecaster:
    """Prophet-based forecaster for seasonal patterns.

    Wraps Facebook Prophet to detect and exploit daily/weekly seasonality
    in infrastructure metrics.  Changepoint detection highlights sudden
    trend shifts (e.g., after deployments or config changes).

    The model requires **at least 1440** data points (one day at one-minute
    resolution) and works best with a full 7-day window.
    """

    def __init__(
        self,
        daily_seasonality: bool = True,
        weekly_seasonality: bool = True,
        yearly_seasonality: bool = False,
        changepoint_prior_scale: float = 0.05,
        confidence_level: float = 0.95,
    ) -> None:
        self.daily_seasonality = daily_seasonality
        self.weekly_seasonality = weekly_seasonality
        self.yearly_seasonality = yearly_seasonality
        self.changepoint_prior_scale = changepoint_prior_scale
        self.confidence_level = confidence_level

        self._model: Any | None = None  # Prophet instance or None
        self._last_fit_size: int = 0
        self._last_fit_ts: int = 0
        self._changepoint_count: int = 0

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def fit(self, timestamps: list[int], values: list[float]) -> None:
        """Fit Prophet on historical data.

        Args:
            timestamps: Unix timestamps (seconds), ascending order.
            values: Metric values corresponding to *timestamps*.

        Raises:
            ValueError: Fewer than 1440 data points.
        """
        n = len(values)
        if n < 1440:
            raise ValueError(
                f"Prophet requires >=1440 data points (1 day @ 1 min), got {n}"
            )

        # Lazy import to avoid module-level overhead when Prophet unused
        from prophet import Prophet  # noqa: F811

        # Build DataFrame
        df = pd.DataFrame(
            {
                "ds": pd.to_datetime(timestamps, unit="s"),
                "y": list(values),
            }
        )
        df = df.drop_duplicates(subset="ds").sort_values("ds").reset_index(
            drop=True
        )

        self._model = Prophet(
            daily_seasonality=self.daily_seasonality,
            weekly_seasonality=self.weekly_seasonality,
            yearly_seasonality=self.yearly_seasonality,
            changepoint_prior_scale=self.changepoint_prior_scale,
            interval_width=self.confidence_level,
        )

        # Silence Prophet's verbose INFO logging
        prophet_logger = __import__("logging").getLogger("prophet")
        prev_level = prophet_logger.level
        prophet_logger.setLevel(__import__("logging").WARNING)
        with warnings.catch_warnings():
            warnings.simplefilter("ignore", FutureWarning)
            self._model.fit(df)
        prophet_logger.setLevel(prev_level)

        self._last_fit_size = len(df)
        self._last_fit_ts = int(timestamps[-1])
        self._changepoint_count = (
            len(self._model.changepoints)
            if self._model.changepoints is not None
            else 0
        )

        logger.info(
            "prophet_forecaster_fitted",
            data_points=len(df),
            changepoints=self._changepoint_count,
            daily_seasonality=self.daily_seasonality,
            weekly_seasonality=self.weekly_seasonality,
        )

    def predict(
        self,
        horizon_seconds: int,
        step_seconds: int = 60,
    ) -> ForecastResult:
        """Generate forecast for the given horizon.

        Args:
            horizon_seconds: How far ahead to forecast (seconds).
            step_seconds: Interval between forecast points.

        Returns:
            :class:`ForecastResult` with timestamps, values, and CIs.

        Raises:
            RuntimeError: Model not fitted.
        """
        if self._model is None:
            raise RuntimeError("Model not fitted – call fit() first.")

        now = int(time.time())
        n_steps = max(1, horizon_seconds // step_seconds)

        # Prophet expects a `ds` column – future dates from the last
        # training timestamp to avoid extrapolation into the past.
        last_ds = self._model.history["ds"].iloc[-1]
        future_dates = pd.date_range(
            start=last_ds,
            periods=n_steps + 1,
            freq=pd.Timedelta(seconds=step_seconds),
        )[1:]  # drop the first (last known) point

        future_df = pd.DataFrame({"ds": future_dates})

        forecast = self._model.predict(future_df)

        # Extract columns
        pred_values = forecast["yhat"].values
        lower = forecast["yhat_lower"].values
        upper = forecast["yhat_upper"].values

        # Convert predicted timestamps to Unix seconds
        pred_timestamps = [now + i * step_seconds for i in range(n_steps)]

        # Confidence based on relative uncertainty width
        denom = np.abs(pred_values) + 1e-9
        avg_uncertainty = float(
            np.mean((upper - lower) / denom)
        )
        confidence = float(np.clip(1.0 - avg_uncertainty, 0.3, 0.95))

        return ForecastResult(
            timestamps=pred_timestamps,
            values=pred_values.tolist(),
            lower_bound=lower.tolist(),
            upper_bound=upper.tolist(),
            confidence=confidence,
            model_name="prophet",
        )

    # ------------------------------------------------------------------
    # Introspection
    # ------------------------------------------------------------------

    @property
    def is_fitted(self) -> bool:
        """True if a model has been fitted."""
        return self._model is not None

    def get_seasonality_components(self) -> dict[str, Any]:
        """Return detected seasonality metadata."""
        if self._model is None:
            return {}
        return {
            "daily": self.daily_seasonality,
            "weekly": self.weekly_seasonality,
            "yearly": self.yearly_seasonality,
            "changepoints": self._changepoint_count,
            "training_points": self._last_fit_size,
        }

    def get_changepoints(self) -> list[int]:
        """Return detected changepoint timestamps (Unix seconds)."""
        if self._model is None or self._model.changepoints is None:
            return []
        return [int(cp.timestamp()) for cp in self._model.changepoints]

    def compute_mape(
        self,
        actual_timestamps: list[int],
        actual_values: list[float],
    ) -> float:
        """Compute MAPE against actuals using Prophet's in-sample fit.

        Args:
            actual_timestamps: Observed timestamps.
            actual_values: Observed values.

        Returns:
            MAPE as a percentage (0-100+).
        """
        if self._model is None or len(actual_values) == 0:
            return 100.0

        df = pd.DataFrame(
            {
                "ds": pd.to_datetime(actual_timestamps, unit="s"),
                "y": list(actual_values),
            }
        )
        df = df.drop_duplicates(subset="ds").sort_values("ds").reset_index(
            drop=True
        )

        forecast = self._model.predict(df[["ds"]])
        y_true = df["y"].values
        y_pred = forecast["yhat"].values
        denom = np.where(np.abs(y_true) < 1e-9, 1e-9, np.abs(y_true))
        return float(np.mean(np.abs(y_true - y_pred) / denom) * 100.0)
