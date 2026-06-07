"""
Time-series data utilities.

Provides validation, gap-filling, downsampling, and statistics
computation for metric time-series data.
"""

from __future__ import annotations

from typing import Any

import numpy as np


def validate_series(
    timestamps: list[int] | np.ndarray,
    values: list[float] | np.ndarray,
) -> bool:
    """Validate a time-series for common issues.

    Checks:
        * Equal length.
        * Timestamps are monotonically non-decreasing.
        * No NaN/Inf values.
        * At least 1 data point.

    Returns:
        True if valid, False otherwise.
    """
    t = np.asarray(timestamps, dtype=np.int64)
    v = np.asarray(values, dtype=np.float64)

    if len(t) != len(v):
        return False
    if len(t) == 0:
        return False
    if np.any(np.diff(t) < 0):
        return False
    if np.any(np.isnan(v)) or np.any(np.isinf(v)):
        return False
    return True


def fill_gaps(
    timestamps: list[int] | np.ndarray,
    values: list[float] | np.ndarray,
    expected_interval: int = 60,
) -> tuple[list[int], list[float]]:
    """Fill gaps in a time-series by linear interpolation.

    If consecutive timestamps differ by more than ``expected_interval``
    seconds, intermediate points are inserted with linearly interpolated
    values.

    Args:
        timestamps: Unix timestamps (ascending).
        values: Metric values.
        expected_interval: Expected seconds between observations.

    Returns:
        Tuple of (filled_timestamps, filled_values).
    """
    t = np.asarray(timestamps, dtype=np.int64)
    v = np.asarray(values, dtype=np.float64)

    if len(t) < 2:
        return list(t.tolist()), list(v.tolist())

    filled_ts: list[int] = []
    filled_vals: list[float] = []

    for i in range(len(t)):
        filled_ts.append(int(t[i]))
        filled_vals.append(float(v[i]))

        # Check for gap after this point
        if i < len(t) - 1:
            gap = t[i + 1] - t[i]
            if gap > expected_interval * 1.5:
                # Number of missing points
                n_fill = int(gap / expected_interval) - 1
                n_fill = min(n_fill, 10000)  # safety cap

                for j in range(1, n_fill + 1):
                    frac = j / (n_fill + 1)
                    interp_ts = int(t[i] + frac * gap)
                    interp_val = float(v[i] + frac * (v[i + 1] - v[i]))
                    filled_ts.append(interp_ts)
                    filled_vals.append(interp_val)

    return filled_ts, filled_vals


def downsample(
    timestamps: list[int] | np.ndarray,
    values: list[float] | np.ndarray,
    target_points: int,
) -> tuple[list[int], list[float]]:
    """Downsample a time-series to approximately ``target_points``.

    Uses averaging over evenly-spaced bins.

    Args:
        timestamps: Unix timestamps.
        values: Metric values.
        target_points: Desired number of output points.

    Returns:
        Tuple of (downsampled_timestamps, downsampled_values).
    """
    t = np.asarray(timestamps, dtype=np.int64)
    v = np.asarray(values, dtype=np.float64)

    n = len(t)
    if n <= target_points or n == 0:
        return list(t.tolist()), list(v.tolist())

    bin_size = max(1, n // target_points)
    out_ts: list[int] = []
    out_vals: list[float] = []

    for start in range(0, n, bin_size):
        end = min(start + bin_size, n)
        bin_ts = t[start:end]
        bin_vals = v[start:end]

        out_ts.append(int(np.mean(bin_ts)))
        out_vals.append(float(np.mean(bin_vals)))

    return out_ts, out_vals


def compute_statistics(values: list[float] | np.ndarray) -> dict[str, float]:
    """Compute descriptive statistics for a value series.

    Returns:
        Dict with keys: min, max, avg, p50, p90, p99, std, count.
    """
    v = np.asarray(values, dtype=np.float64)
    if len(v) == 0:
        return {
            "min": 0.0,
            "max": 0.0,
            "avg": 0.0,
            "p50": 0.0,
            "p90": 0.0,
            "p99": 0.0,
            "std": 0.0,
            "count": 0.0,
        }

    return {
        "min": float(np.min(v)),
        "max": float(np.max(v)),
        "avg": float(np.mean(v)),
        "p50": float(np.percentile(v, 50)),
        "p90": float(np.percentile(v, 90)),
        "p99": float(np.percentile(v, 99)),
        "std": float(np.std(v)),
        "count": float(len(v)),
    }


def detect_interval(timestamps: list[int] | np.ndarray) -> int:
    """Auto-detect the expected interval between observations.

    Uses the median of consecutive differences.

    Returns:
        Interval in seconds (defaults to 60 if too few points).
    """
    t = np.asarray(timestamps, dtype=np.int64)
    if len(t) < 2:
        return 60
    diffs = np.diff(t)
    positive = diffs[diffs > 0]
    if len(positive) == 0:
        return 60
    return int(np.median(positive))


def slice_time_range(
    timestamps: list[int] | np.ndarray,
    values: list[float] | np.ndarray,
    start_ts: int,
    end_ts: int,
) -> tuple[list[int], list[float]]:
    """Slice a time-series to a specific time range [start, end].

    Returns:
        Tuple of (timestamps, values) within the range.
    """
    t = np.asarray(timestamps, dtype=np.int64)
    v = np.asarray(values, dtype=np.float64)

    mask = (t >= start_ts) & (t <= end_ts)
    return list(t[mask].tolist()), list(v[mask].tolist())
