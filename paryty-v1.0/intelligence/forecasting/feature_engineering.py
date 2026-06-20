"""
Shared feature extraction utilities for time-series forecasting.

Provides reusable feature builders used by XGBoost and potentially
other models.  All functions operate on numpy arrays for speed.
"""

from __future__ import annotations

from typing import Any

import numpy as np
import pandas as pd


# ---------------------------------------------------------------------------
# Lag features
# ---------------------------------------------------------------------------


def build_lag_features(
    values: np.ndarray,
    lags: list[int],
) -> tuple[np.ndarray, list[str]]:
    """Build lag-1…lag-N features.

    For each lag *k*, the feature at index *i* is ``values[i - k]``
    (filled with ``values[0]`` for the first *k* rows).

    Returns (X, names) where X has shape (n, len(lags)).
    """
    n = len(values)
    features: list[np.ndarray] = []
    names: list[str] = []
    for lag in lags:
        arr = np.roll(values, lag)
        arr[:lag] = values[0]
        features.append(arr)
        names.append(f"lag_{lag}")
    return np.column_stack(features), names


# ---------------------------------------------------------------------------
# Rolling-window statistics
# ---------------------------------------------------------------------------


def _rolling_agg(
    values: np.ndarray,
    window: int,
    func: Any,
) -> np.ndarray:
    """Compute a rolling aggregation over a fixed window."""
    n = len(values)
    result = np.empty(n, dtype=np.float64)
    for i in range(n):
        start = max(0, i - window + 1)
        result[i] = func(values[start : i + 1])
    return result


def build_rolling_features(
    values: np.ndarray,
    windows: list[int],
) -> tuple[np.ndarray, list[str]]:
    """Build rolling mean/std/min/max features for each window.

    Returns (X, names) with shape (n, 4 * len(windows)).
    """
    features: list[np.ndarray] = []
    names: list[str] = []
    for w in windows:
        for agg_fn, suffix in [
            (np.mean, "mean"),
            (np.std, "std"),
            (np.min, "min"),
            (np.max, "max"),
        ]:
            features.append(_rolling_agg(values, w, agg_fn))
            names.append(f"rolling_{w}_{suffix}")
    return np.column_stack(features), names


# ---------------------------------------------------------------------------
# Time features
# ---------------------------------------------------------------------------


def build_time_features(timestamps: np.ndarray) -> tuple[np.ndarray, list[str]]:
    """Build normalised time-of-day / day-of-week / minute-of-hour features.

    All outputs are scaled to [0, 1].

    Returns (X, names) with shape (n, 3).
    """
    dt = pd.to_datetime(timestamps, unit="s")
    features = [
        dt.hour.values / 23.0,
        dt.dayofweek.values / 6.0,
        dt.minute.values / 59.0,
    ]
    names = ["hour_of_day", "day_of_week", "minute_of_hour"]
    return np.column_stack(features), names


# ---------------------------------------------------------------------------
# Trend feature
# ---------------------------------------------------------------------------


def build_trend_feature(
    values: np.ndarray,
    window: int = 10,
) -> np.ndarray:
    """Compute a rolling linear trend (slope) feature.

    For each index *i*, the slope of a least-squares fit over the
    preceding *window* observations is returned.  The first *window*
    entries are zero.
    """
    n = len(values)
    trend = np.zeros(n, dtype=np.float64)
    x = np.arange(window, dtype=np.float64)
    for i in range(window, n):
        y = values[i - window : i]
        trend[i] = float(np.polyfit(x, y, 1)[0])
    return trend


# ---------------------------------------------------------------------------
# Composite builder
# ---------------------------------------------------------------------------


def build_all_features(
    values: np.ndarray,
    timestamps: np.ndarray,
    lags: list[int] | None = None,
    windows: list[int] | None = None,
    trend_window: int = 10,
) -> tuple[np.ndarray, list[str]]:
    """Build the full feature set used by the XGBoost forecaster.

    Combines lag, rolling, time, and trend features into a single matrix.

    Args:
        values: Metric values (n,).
        timestamps: Unix timestamps in seconds (n,).
        lags: Lag offsets (default [1, 5, 15, 60]).
        windows: Rolling windows (default [5, 15, 60]).
        trend_window: Window for slope calculation (default 10).

    Returns:
        (X, feature_names) with X shape (n, n_features).
    """
    if lags is None:
        lags = [1, 5, 15, 60]
    if windows is None:
        windows = [5, 15, 60]

    parts: list[np.ndarray] = []
    all_names: list[str] = []

    lag_X, lag_names = build_lag_features(values, lags)
    parts.append(lag_X)
    all_names.extend(lag_names)

    roll_X, roll_names = build_rolling_features(values, windows)
    parts.append(roll_X)
    all_names.extend(roll_names)

    time_X, time_names = build_time_features(timestamps)
    parts.append(time_X)
    all_names.extend(time_names)

    trend = build_trend_feature(values, window=trend_window)
    parts.append(trend.reshape(-1, 1))
    all_names.append("trend")

    return np.column_stack(parts), all_names


# ---------------------------------------------------------------------------
# Prediction-time feature extraction
# ---------------------------------------------------------------------------


def extract_predict_features(
    values: np.ndarray,
    timestamp: int,
    lags: list[int],
    windows: list[int],
    trend_window: int = 10,
) -> np.ndarray:
    """Extract the feature vector for a single prediction step.

    Uses the tail of *values* to compute lag, rolling, and trend features
    and the given *timestamp* for time features.

    Returns a 1-D feature array.
    """
    feat: list[float] = []

    # Lag features
    for lag in lags:
        idx = max(len(values) - lag - 1, 0)
        feat.append(float(values[idx]))

    # Rolling stats
    for w in windows:
        tail = values[max(0, len(values) - w) :]
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
    if len(values) >= trend_window:
        slope = float(np.polyfit(np.arange(trend_window), values[-trend_window:], 1)[0])
    else:
        slope = 0.0
    feat.append(slope)

    return np.array(feat, dtype=np.float64)


# ---------------------------------------------------------------------------
# Fourier features for seasonality
# ---------------------------------------------------------------------------


def build_fourier_features(
    timestamps: np.ndarray,
    periods: list[int] | None = None,
    n_harmonics: int = 3,
) -> tuple[np.ndarray, list[str]]:
    """Build Fourier features for capturing seasonality.

    Args:
        timestamps: Unix timestamps in seconds.
        periods: List of seasonal periods in seconds (default: hourly, daily, weekly).
        n_harmonics: Number of harmonics per period.

    Returns:
        (X, feature_names) with shape (n, 2 * n_harmonics * len(periods)).
    """
    if periods is None:
        periods = [3600, 86400, 604800]  # hourly, daily, weekly

    features: list[np.ndarray] = []
    names: list[str] = []
    t = timestamps.astype(np.float64)

    for period in periods:
        for k in range(1, n_harmonics + 1):
            angle = 2 * np.pi * k * t / period
            features.append(np.sin(angle))
            names.append(f"fourier_sin_{period}_{k}")
            features.append(np.cos(angle))
            names.append(f"fourier_cos_{period}_{k}")

    return np.column_stack(features) if features else np.empty((len(timestamps), 0)), names


# ---------------------------------------------------------------------------
# Volatility features
# ---------------------------------------------------------------------------


def build_volatility_features(
    values: np.ndarray,
    windows: list[int] | None = None,
) -> tuple[np.ndarray, list[str]]:
    """Build volatility features: range, ATR-like, and coefficient of variation.

    Args:
        values: Metric values.
        windows: Rolling windows (default: [5, 15, 60]).

    Returns:
        (X, feature_names) with shape (n, 3 * len(windows)).
    """
    if windows is None:
        windows = [5, 15, 60]

    features: list[np.ndarray] = []
    names: list[str] = []
    n = len(values)

    for w in windows:
        # Rolling range (max - min)
        range_feat = np.empty(n, dtype=np.float64)
        for i in range(n):
            start = max(0, i - w + 1)
            window_vals = values[start:i + 1]
            range_feat[i] = float(np.max(window_vals) - np.min(window_vals)) if len(window_vals) > 0 else 0.0
        features.append(range_feat)
        names.append(f"volatility_range_{w}")

        # Rolling coefficient of variation (std / mean)
        cv_feat = np.empty(n, dtype=np.float64)
        for i in range(n):
            start = max(0, i - w + 1)
            window_vals = values[start:i + 1]
            if len(window_vals) > 1:
                mean_val = np.mean(window_vals)
                cv_feat[i] = float(np.std(window_vals) / abs(mean_val)) if abs(mean_val) > 1e-9 else 0.0
            else:
                cv_feat[i] = 0.0
        features.append(cv_feat)
        names.append(f"volatility_cv_{w}")

        # Mean absolute change
        mac_feat = np.empty(n, dtype=np.float64)
        for i in range(n):
            start = max(0, i - w + 1)
            window_vals = values[start:i + 1]
            if len(window_vals) > 1:
                mac_feat[i] = float(np.mean(np.abs(np.diff(window_vals))))
            else:
                mac_feat[i] = 0.0
        features.append(mac_feat)
        names.append(f"volatility_mac_{w}")

    return np.column_stack(features) if features else np.empty((n, 0)), names


# ---------------------------------------------------------------------------
# Autocorrelation features
# ---------------------------------------------------------------------------


def build_autocorrelation_features(
    values: np.ndarray,
    lags: list[int] | None = None,
    window: int = 100,
) -> tuple[np.ndarray, list[str]]:
    """Build rolling autocorrelation features.

    Args:
        values: Metric values.
        lags: Autocorrelation lags (default: [1, 5, 10]).
        window: Rolling window size for ACF computation.

    Returns:
        (X, feature_names) with shape (n, len(lags)).
    """
    if lags is None:
        lags = [1, 5, 10]

    n = len(values)
    features: list[np.ndarray] = []
    names: list[str] = []

    for lag in lags:
        acf_feat = np.zeros(n, dtype=np.float64)
        for i in range(max(window, lag), n):
            start = max(0, i - window + 1)
            window_vals = values[start:i + 1]
            if len(window_vals) > lag:
                # Compute autocorrelation at given lag
                mean_val = np.mean(window_vals)
                var_val = np.var(window_vals)
                if var_val > 1e-9:
                    cov = np.mean((window_vals[lag:] - mean_val) * (window_vals[:-lag] - mean_val))
                    acf_feat[i] = float(cov / var_val)
        features.append(acf_feat)
        names.append(f"acf_{lag}_{window}")

    return np.column_stack(features), names


# ---------------------------------------------------------------------------
# Enhanced composite builder
# ---------------------------------------------------------------------------


def build_enhanced_features(
    values: np.ndarray,
    timestamps: np.ndarray,
    lags: list[int] | None = None,
    windows: list[int] | None = None,
    trend_window: int = 10,
    include_fourier: bool = True,
    include_volatility: bool = True,
    include_acf: bool = True,
) -> tuple[np.ndarray, list[str]]:
    """Build the enhanced feature set with all available features.

    Combines lag, rolling, time, trend, Fourier, volatility, and
    autocorrelation features into a single matrix.

    Args:
        values: Metric values (n,).
        timestamps: Unix timestamps in seconds (n,).
        lags: Lag offsets (default [1, 5, 15, 60]).
        windows: Rolling windows (default [5, 15, 60]).
        trend_window: Window for slope calculation (default 10).
        include_fourier: Whether to include Fourier features.
        include_volatility: Whether to include volatility features.
        include_acf: Whether to include autocorrelation features.

    Returns:
        (X, feature_names) with X shape (n, n_features).
    """
    if lags is None:
        lags = [1, 5, 15, 60]
    if windows is None:
        windows = [5, 15, 60]

    parts: list[np.ndarray] = []
    all_names: list[str] = []

    # Base features
    lag_X, lag_names = build_lag_features(values, lags)
    parts.append(lag_X)
    all_names.extend(lag_names)

    roll_X, roll_names = build_rolling_features(values, windows)
    parts.append(roll_X)
    all_names.extend(roll_names)

    time_X, time_names = build_time_features(timestamps)
    parts.append(time_X)
    all_names.extend(time_names)

    trend = build_trend_feature(values, window=trend_window)
    parts.append(trend.reshape(-1, 1))
    all_names.append("trend")

    # Enhanced features
    if include_fourier:
        fourier_X, fourier_names = build_fourier_features(timestamps)
        if fourier_X.shape[1] > 0:
            parts.append(fourier_X)
            all_names.extend(fourier_names)

    if include_volatility:
        vol_X, vol_names = build_volatility_features(values, windows)
        if vol_X.shape[1] > 0:
            parts.append(vol_X)
            all_names.extend(vol_names)

    if include_acf:
        acf_X, acf_names = build_autocorrelation_features(values)
        parts.append(acf_X)
        all_names.extend(acf_names)

    return np.column_stack(parts), all_names
