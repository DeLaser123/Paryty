"""
Statistical anomaly detectors — Z-Score, IQR, EWMA.

These lightweight detectors run in <0.1ms per point and are ideal for
real-time streaming anomaly detection.  Each detector returns an
AnomalyResult for every flagged point.

Algorithms:
    * Z-Score: flag when |z| > threshold within a rolling window.
    * IQR: flag when value < Q1 - k*IQR or value > Q3 + k*IQR.
    * EWMA: flag when deviation from exponentially weighted mean exceeds
      threshold × EWMA standard deviation.
"""

from __future__ import annotations

import enum
from dataclasses import dataclass, field
from typing import Sequence

import numpy as np
import structlog

from .config import AnomalyConfig

logger = structlog.get_logger(__name__)


# ---------------------------------------------------------------------------
# Shared data structures
# ---------------------------------------------------------------------------


class AnomalyType(str, enum.Enum):
    """Classification of anomaly kinds."""

    POINT = "point"
    CONTEXTUAL = "contextual"
    COLLECTIVE = "collective"
    TREND = "trend"


class Severity(str, enum.Enum):
    """Anomaly severity for UI stacking."""

    INFO = "info"
    WARN = "warn"
    CRITICAL = "critical"


@dataclass(frozen=True)
class AnomalyResult:
    """A single detected anomaly.

    Attributes:
        timestamp: Unix timestamp of the anomalous point.
        value: The anomalous metric value.
        score: Anomaly score 0.0 – 1.0 (higher = more anomalous).
        anomaly_type: Classification of the anomaly kind.
        explanation: Human-readable explanation.
        method: Detector that produced this result.
        severity: Severity level for UI rendering.
        contributing_factors: Features/values that contributed.
    """

    timestamp: int
    value: float
    score: float
    anomaly_type: AnomalyType = AnomalyType.POINT
    explanation: str = ""
    method: str = ""
    severity: Severity = Severity.WARN
    contributing_factors: list[str] = field(default_factory=list)


def _severity_from_score(score: float) -> Severity:
    """Map a 0-1 anomaly score to a severity level."""
    if score >= 0.8:
        return Severity.CRITICAL
    if score >= 0.5:
        return Severity.WARN
    return Severity.INFO


# ---------------------------------------------------------------------------
# Z-Score Detector
# ---------------------------------------------------------------------------


class ZScoreDetector:
    """Rolling-window Z-Score anomaly detector.

    Flags a point as anomalous when its Z-score (number of standard
    deviations from the rolling mean) exceeds the configured threshold.

    PERFORMANCE: <0.1ms per point (vectorised numpy).
    """

    def __init__(
        self,
        threshold: float = 3.0,
        window_size: int = 60,
    ) -> None:
        self.threshold = threshold
        self.window_size = window_size

    def detect(
        self,
        timestamps: Sequence[int],
        values: Sequence[float],
    ) -> list[AnomalyResult]:
        """Scan the series and return all anomalous points.

        Args:
            timestamps: Unix timestamps.
            values: Metric values (same length as *timestamps*).

        Returns:
            List of :class:`AnomalyResult` for flagged points.
        """
        if len(values) < 3:
            return []

        v = np.asarray(values, dtype=np.float64)
        t = np.asarray(timestamps, dtype=np.int64)
        n = len(v)
        anomalies: list[AnomalyResult] = []

        for i in range(self.window_size, n):
            window = v[max(0, i - self.window_size) : i]
            mu = float(np.mean(window))
            sigma = float(np.std(window))
            if sigma < 1e-9:
                continue
            z = abs(v[i] - mu) / sigma
            if z >= self.threshold:
                score = float(np.clip(z / (self.threshold * 2), 0.0, 1.0))
                anomalies.append(
                    AnomalyResult(
                        timestamp=int(t[i]),
                        value=float(v[i]),
                        score=score,
                        anomaly_type=AnomalyType.POINT,
                        explanation=(
                            f"Value {v[i]:.2f} is {z:.1f}σ from rolling "
                            f"mean {mu:.2f} (threshold {self.threshold}σ)"
                        ),
                        method="zscore",
                        severity=_severity_from_score(score),
                        contributing_factors=[
                            f"z_score={z:.2f}",
                            f"mean={mu:.2f}",
                            f"std={sigma:.2f}",
                        ],
                    )
                )

        return anomalies


# ---------------------------------------------------------------------------
# IQR Detector
# ---------------------------------------------------------------------------


class IQRDetector:
    """Interquartile Range anomaly detector.

    Flags points outside [Q1 - k*IQR, Q3 + k*IQR] computed over a
    rolling window.  The standard multiplier is 1.5 (mild outliers);
    3.0 flags only extreme outliers.

    PERFORMANCE: <0.1ms per point.
    """

    def __init__(
        self,
        multiplier: float = 1.5,
        window_size: int = 100,
    ) -> None:
        self.multiplier = multiplier
        self.window_size = window_size

    def detect(
        self,
        timestamps: Sequence[int],
        values: Sequence[float],
    ) -> list[AnomalyResult]:
        """Detect IQR-based anomalies."""
        if len(values) < 5:
            return []

        v = np.asarray(values, dtype=np.float64)
        t = np.asarray(timestamps, dtype=np.int64)
        n = len(v)
        anomalies: list[AnomalyResult] = []

        for i in range(self.window_size, n):
            window = v[max(0, i - self.window_size) : i]
            q1 = float(np.percentile(window, 25))
            q3 = float(np.percentile(window, 75))
            iqr = q3 - q1
            if iqr < 1e-9:
                continue

            lower = q1 - self.multiplier * iqr
            upper = q3 + self.multiplier * iqr

            if v[i] < lower or v[i] > upper:
                dist = max(lower - v[i], v[i] - upper, 0)
                score = float(np.clip(dist / (iqr * self.multiplier), 0.0, 1.0))
                direction = "below" if v[i] < lower else "above"
                anomalies.append(
                    AnomalyResult(
                        timestamp=int(t[i]),
                        value=float(v[i]),
                        score=score,
                        anomaly_type=AnomalyType.POINT,
                        explanation=(
                            f"Value {v[i]:.2f} is {direction} IQR bounds "
                            f"[{lower:.2f}, {upper:.2f}] (k={self.multiplier})"
                        ),
                        method="iqr",
                        severity=_severity_from_score(score),
                        contributing_factors=[
                            f"q1={q1:.2f}",
                            f"q3={q3:.2f}",
                            f"iqr={iqr:.2f}",
                        ],
                    )
                )

        return anomalies


# ---------------------------------------------------------------------------
# EWMA Detector
# ---------------------------------------------------------------------------


class EWMADetector:
    """Exponentially Weighted Moving Average anomaly detector.

    Maintains an EWMA and EWMA standard deviation.  A point is flagged
    when its deviation from the EWMA exceeds ``threshold × ewma_std``.

    This detector is more responsive to recent changes than a simple
    rolling Z-Score and is well-suited for detecting gradual drift.

    PERFORMANCE: <0.1ms per point.
    """

    def __init__(
        self,
        alpha: float = 0.3,
        threshold: float = 3.0,
    ) -> None:
        self.alpha = alpha
        self.threshold = threshold

    def detect(
        self,
        timestamps: Sequence[int],
        values: Sequence[float],
    ) -> list[AnomalyResult]:
        """Detect EWMA-based anomalies."""
        if len(values) < 3:
            return []

        v = np.asarray(values, dtype=np.float64)
        t = np.asarray(timestamps, dtype=np.int64)
        n = len(v)
        anomalies: list[AnomalyResult] = []

        # Initialise EWMA
        ewma = v[0]
        ewma_var = 0.0

        for i in range(1, n):
            diff = v[i] - ewma
            ewma = self.alpha * v[i] + (1 - self.alpha) * ewma
            ewma_var = self.alpha * diff**2 + (1 - self.alpha) * ewma_var
            ewma_std = max(ewma_var**0.5, 1e-9)

            deviation = abs(diff) / ewma_std
            if deviation >= self.threshold:
                score = float(
                    np.clip(deviation / (self.threshold * 2), 0.0, 1.0)
                )
                anomalies.append(
                    AnomalyResult(
                        timestamp=int(t[i]),
                        value=float(v[i]),
                        score=score,
                        anomaly_type=AnomalyType.TREND,
                        explanation=(
                            f"Value {v[i]:.2f} deviates {deviation:.1f}σ "
                            f"from EWMA {ewma:.2f} (threshold {self.threshold}σ)"
                        ),
                        method="ewma",
                        severity=_severity_from_score(score),
                        contributing_factors=[
                            f"ewma={ewma:.2f}",
                            f"ewma_std={ewma_std:.2f}",
                            f"deviation={deviation:.2f}",
                        ],
                    )
                )

        return anomalies


# ---------------------------------------------------------------------------
# Composite statistical detector
# ---------------------------------------------------------------------------


class StatisticalDetector:
    """Run all three statistical detectors and merge results.

    A point is flagged if **any** sub-detector flags it.  The score
    is the maximum across detectors.
    """

    def __init__(self, config: AnomalyConfig | None = None) -> None:
        cfg = config or AnomalyConfig()
        self._zscore = ZScoreDetector(
            threshold=cfg.zscore.threshold,
            window_size=cfg.zscore.window_size,
        )
        self._iqr = IQRDetector(
            multiplier=cfg.iqr.multiplier,
            window_size=cfg.iqr.window_size,
        )
        self._ewma = EWMADetector(
            alpha=cfg.ewma.alpha,
            threshold=cfg.ewma.threshold,
        )

    def detect(
        self,
        timestamps: Sequence[int],
        values: Sequence[float],
    ) -> list[AnomalyResult]:
        """Run Z-Score, IQR, and EWMA detectors.

        Returns a merged list of anomalies, deduplicated by timestamp
        with the maximum score kept.
        """
        all_results: list[AnomalyResult] = []
        all_results.extend(self._zscore.detect(timestamps, values))
        all_results.extend(self._iqr.detect(timestamps, values))
        all_results.extend(self._ewma.detect(timestamps, values))

        # Deduplicate by timestamp – keep highest score
        by_ts: dict[int, AnomalyResult] = {}
        for r in all_results:
            existing = by_ts.get(r.timestamp)
            if existing is None or r.score > existing.score:
                by_ts[r.timestamp] = r

        return sorted(by_ts.values(), key=lambda x: x.timestamp)
