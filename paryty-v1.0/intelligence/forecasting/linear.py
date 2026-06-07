"""
Linear Regression forecasting layer.

Provides fast (<1ms) baseline forecasts using rolling window linear regression.
Best for: short-term trends (1-60 minutes), simple monotonic patterns.
NOT good for: seasonal patterns, complex non-linear behavior.

ALGORITHM:
    1. Take last N data points (window_size, default 60)
    2. Fit linear regression: value = a * time + b
    3. Extrapolate to horizon
    4. Compute prediction intervals using residual standard error
    5. Confidence intervals widen linearly with distance from training data

PERFORMANCE: <1ms per forecast (single metric)
MIN DATA: 3 points
"""

from __future__ import annotations

import time

import numpy as np
import structlog
from sklearn.linear_model import LinearRegression

logger = structlog.get_logger(__name__)

# z-scores for common confidence levels
_Z_SCORES: dict[float, float] = {
    0.80: 1.282,
    0.85: 1.440,
    0.90: 1.645,
    0.95: 1.960,
    0.99: 2.576,
}


class ForecastResult:
    """Single forecast output.

    Attributes:
        timestamps: Unix timestamps (seconds) for each forecast point.
        values: Forecasted metric values.
        lower_bound: Lower confidence interval values.
        upper_bound: Upper confidence interval values.
        confidence: Overall confidence score 0.0 – 1.0.
        model_name: Name of the model that produced this result.
    """

    __slots__ = (
        "timestamps",
        "values",
        "lower_bound",
        "upper_bound",
        "confidence",
        "model_name",
    )

    def __init__(
        self,
        timestamps: list[int],
        values: list[float],
        lower_bound: list[float],
        upper_bound: list[float],
        confidence: float,
        model_name: str = "linear",
    ) -> None:
        self.timestamps = timestamps
        self.values = values
        self.lower_bound = lower_bound
        self.upper_bound = upper_bound
        self.confidence = confidence
        self.model_name = model_name

    def __repr__(self) -> str:
        return (
            f"ForecastResult(model={self.model_name!r}, "
            f"points={len(self.timestamps)}, "
            f"confidence={self.confidence:.3f})"
        )


class LinearForecaster:
    """Rolling-window linear regression forecaster.

    Fits a simple OLS on the last ``window_size`` observations and
    extrapolates forward.  Prediction intervals are derived from the
    residual standard error and widen linearly with distance from the
    training window.

    The model handles as few as **3** data points by reducing the
    effective window.
    """

    def __init__(
        self,
        window_size: int = 60,
        confidence_level: float = 0.95,
    ) -> None:
        self.window_size = window_size
        self.confidence_level = confidence_level
        self._model: LinearRegression | None = None
        self._residual_std: float = 0.0
        self._last_train_ts: int = 0
        self._n_training_points: int = 0
        self._t_start: float = 0.0  # reference epoch for normalization

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def fit(self, timestamps: list[int], values: list[float]) -> None:
        """Fit the model on recent data.

        Args:
            timestamps: Unix timestamps in **seconds** (ascending).
            values: Corresponding metric values.

        Raises:
            ValueError: If fewer than 3 data points are provided.
        """
        n = len(values)
        if n < 3:
            raise ValueError(
                f"Linear regression requires >=3 data points, got {n}"
            )

        # Select the rolling window
        effective_window = min(n, self.window_size)
        t = np.asarray(timestamps[-effective_window:], dtype=np.float64)
        y = np.asarray(values[-effective_window:], dtype=np.float64)

        # Normalise time axis to seconds-since-window-start
        self._t_start = float(t[0])
        t_norm = (t - self._t_start).reshape(-1, 1)

        # Fit OLS
        self._model = LinearRegression()
        self._model.fit(t_norm, y)

        # Residual standard error
        predictions = self._model.predict(t_norm)
        residuals = y - predictions
        self._residual_std = float(np.std(residuals, ddof=2)) if n > 2 else 0.0

        self._last_train_ts = int(t[-1])
        self._n_training_points = effective_window

        logger.debug(
            "linear_forecaster_fitted",
            window=effective_window,
            residual_std=self._residual_std,
            slope=float(self._model.coef_[0]),
            intercept=float(self._model.intercept_),
        )

    def predict(
        self,
        horizon_seconds: int,
        step_seconds: int = 60,
    ) -> ForecastResult:
        """Generate a forecast for the given horizon.

        Args:
            horizon_seconds: How far ahead to forecast.
            step_seconds: Time step between forecast points.

        Returns:
            :class:`ForecastResult` with timestamps, values, and CIs.

        Raises:
            RuntimeError: If the model has not been fitted.
        """
        if self._model is None:
            raise RuntimeError("Model not fitted – call fit() first.")

        now = int(time.time())
        n_steps = max(1, horizon_seconds // step_seconds)

        # Future time offsets relative to training start
        future_t = np.array(
            [now + i * step_seconds - self._t_start for i in range(n_steps)],
            dtype=np.float64,
        ).reshape(-1, 1)

        # Point predictions
        preds = self._model.predict(future_t)

        # Confidence intervals – widen with distance
        z = _Z_SCORES.get(
            round(self.confidence_level, 2),
            1.96,
        )
        distances = np.arange(1, n_steps + 1, dtype=np.float64)
        widening = 1.0 + distances / max(n_steps, 1)
        margin = z * self._residual_std * widening

        lower = preds - margin
        upper = preds + margin

        # Per-step confidence decays with distance
        mean_abs = float(np.mean(np.abs(preds))) + 1e-9
        base_conf = max(0.3, 1.0 - self._residual_std / mean_abs)
        step_conf = base_conf * (1.0 - distances / (n_steps * 2))
        overall_confidence = float(np.clip(np.mean(step_conf), 0.1, 0.95))

        future_timestamps = [now + i * step_seconds for i in range(n_steps)]

        return ForecastResult(
            timestamps=future_timestamps,
            values=preds.tolist(),
            lower_bound=lower.tolist(),
            upper_bound=upper.tolist(),
            confidence=overall_confidence,
            model_name="linear",
        )

    # ------------------------------------------------------------------
    # Introspection
    # ------------------------------------------------------------------

    @property
    def is_fitted(self) -> bool:
        """Return True if the model has been fitted."""
        return self._model is not None

    @property
    def slope(self) -> float:
        """Return the fitted slope (units/second)."""
        if self._model is None:
            return 0.0
        return float(self._model.coef_[0])

    @property
    def intercept(self) -> float:
        """Return the fitted intercept."""
        if self._model is None:
            return 0.0
        return float(self._model.intercept_)

    def compute_mape(
        self,
        actual_timestamps: list[int],
        actual_values: list[float],
    ) -> float:
        """Compute MAPE against actual observations.

        Args:
            actual_timestamps: Observed timestamps.
            actual_values: Observed values.

        Returns:
            MAPE as a percentage (0–100+).
        """
        if self._model is None or len(actual_values) == 0:
            return 100.0

        t = np.asarray(actual_timestamps, dtype=np.float64)
        y = np.asarray(actual_values, dtype=np.float64)
        t_norm = (t - self._t_start).reshape(-1, 1)
        preds = self._model.predict(t_norm)

        # Avoid division by zero
        denom = np.where(np.abs(y) < 1e-9, 1e-9, np.abs(y))
        mape = float(np.mean(np.abs(y - preds) / denom) * 100.0)
        return mape

    def get_state(self) -> dict[str, float]:
        """Return model state for serialisation."""
        return {
            "slope": self.slope,
            "intercept": self.intercept,
            "residual_std": self._residual_std,
            "window_size": float(self.window_size),
            "confidence_level": self.confidence_level,
            "t_start": self._t_start,
            "n_training_points": float(self._n_training_points),
        }

    def set_state(self, state: dict[str, float]) -> None:
        """Restore model from saved state (re-fit from parameters)."""
        self._residual_std = state["residual_std"]
        self._t_start = state["t_start"]
        self._n_training_points = int(state["n_training_points"])
        self.window_size = int(state["window_size"])
        self.confidence_level = state["confidence_level"]

        # Reconstruct a minimal LinearRegression from stored params
        self._model = LinearRegression()
        self._model.coef_ = np.array([state["slope"]])
        self._model.intercept_ = state["intercept"]
        self._last_train_ts = 0  # unknown without full data
