"""
Concept drift detection for time-series data.

Monitors when the underlying data distribution changes significantly,
triggering model retraining when drift is detected.

Algorithms:
- Page-Hinkley Test: Sequential detection of mean changes
- Kolmogorov-Smirnov Test: Distribution change detection
- Adaptive Windowing (ADWIN): Sliding window with automatic size adjustment
"""

from __future__ import annotations

import time
from collections import deque
from dataclasses import dataclass, field
from typing import Any

import numpy as np
import structlog

logger = structlog.get_logger(__name__)


@dataclass
class DriftResult:
    """Result of drift detection analysis."""

    drift_detected: bool
    drift_type: str  # 'none', 'gradual', 'sudden', 'recurring'
    confidence: float  # 0.0 - 1.0
    metric_name: str
    timestamp: int
    details: dict[str, Any] = field(default_factory=dict)


class PageHinkleyDetector:
    """Page-Hinkley test for sequential drift detection.

    Detects changes in the mean of a data stream. Lower thresholds
    make the detector more sensitive but may produce false positives.
    """

    def __init__(
        self,
        delta: float = 0.005,
        threshold: float = 50.0,
        min_instances: int = 30,
    ):
        self.delta = delta
        self.threshold = threshold
        self.min_instances = min_instances
        self._n: int = 0
        self._sum: float = 0.0
        self._x_mean: float = 0.0
        self._m_n: float = 0.0
        self._m_min: float = float("inf")

    def update(self, value: float) -> bool:
        """Update detector with new value and check for drift.

        Returns:
            True if drift detected.
        """
        self._n += 1
        self._sum += value
        self._x_mean = self._sum / self._n

        self._m_n += value - self._x_mean - self.delta
        self._m_min = min(self._m_min, self._m_n)

        if self._n < self.min_instances:
            return False

        return (self._m_n - self._m_min) > self.threshold

    def reset(self) -> None:
        """Reset detector state."""
        self._n = 0
        self._sum = 0.0
        self._x_mean = 0.0
        self._m_n = 0.0
        self._m_min = float("inf")


class KSDetector:
    """Kolmogorov-Smirnov test for distribution change detection.

    Compares two windows of data to detect if they come from
    different distributions.
    """

    def __init__(
        self,
        window_size: int = 100,
        p_value_threshold: float = 0.01,
    ):
        self.window_size = window_size
        self.p_value_threshold = p_value_threshold
        self._reference_window: deque[float] = deque(maxlen=window_size)
        self._test_window: deque[float] = deque(maxlen=window_size)
        self._n_updates: int = 0

    def update(self, value: float) -> bool:
        """Update detector and check for distribution drift.

        Returns:
            True if drift detected.
        """
        self._n_updates += 1

        # Fill reference window first
        if len(self._reference_window) < self.window_size:
            self._reference_window.append(value)
            return False

        self._test_window.append(value)

        if len(self._test_window) < self.window_size:
            return False

        # Perform KS test
        ref = np.array(self._reference_window)
        test = np.array(self._test_window)

        # Two-sample KS statistic
        all_values = np.sort(np.concatenate([ref, test]))
        cdf_ref = np.searchsorted(np.sort(ref), all_values, side='right') / len(ref)
        cdf_test = np.searchsorted(np.sort(test), all_values, side='right') / len(test)
        ks_stat = np.max(np.abs(cdf_ref - cdf_test))

        # Approximate p-value using Kolmogorov distribution
        n = len(ref)
        m = len(test)
        en = np.sqrt(n * m / (n + m))
        p_value = self._kolmogorov_cdf(ks_stat, en)

        if p_value < self.p_value_threshold:
            # Drift detected: slide reference window forward
            self._reference_window = deque(self._test_window, maxlen=self.window_size)
            self._test_window.clear()
            return True

        return False

    def _kolmogorov_cdf(self, x: float, n: float) -> float:
        """Approximate the CDF of the Kolmogorov distribution."""
        # Uses the limiting form for large n
        if x <= 0:
            return 0.0
        if x >= 1:
            return 1.0

        # Sum approximation
        s = 0.0
        for k in range(1, 100):
            term = (-1) ** (k - 1) * np.exp(-2 * k * k * x * x * n)
            s += term
            if abs(term) < 1e-10:
                break

        return max(0.0, min(1.0, 2.0 * s))

    def reset(self) -> None:
        """Reset detector state."""
        self._reference_window.clear()
        self._test_window.clear()
        self._n_updates = 0


class ADWINDetector:
    """Adaptive Windowing (ADWIN) detector.

    Maintains a variable-length window and detects drift when
    the distribution within sub-windows differs significantly.
    """

    def __init__(
        self,
        delta: float = 0.002,
        min_window_size: int = 10,
        max_window_size: int = 10000,
    ):
        self.delta = delta
        self.min_window_size = min_window_size
        self.max_window_size = max_window_size
        self._window: deque[float] = deque(maxlen=max_window_size)
        self._sum: float = 0.0

    def update(self, value: float) -> bool:
        """Update detector and check for drift.

        Returns:
            True if drift detected.
        """
        self._window.append(value)
        self._sum += value

        if len(self._window) < self.min_window_size * 2:
            return False

        # Check for drift by examining sub-windows
        return self._check_drift()

    def _check_drift(self) -> bool:
        """Check if drift exists between sub-windows."""
        n = len(self._window)
        arr = np.array(self._window)

        # Try different cut points
        for cut in range(self.min_window_size, n - self.min_window_size + 1, max(1, n // 20)):
            left = arr[:cut]
            right = arr[cut:]

            mean_left = np.mean(left)
            mean_right = np.mean(right)

            # Hoeffding bound
            n_left = len(left)
            n_right = len(right)
            m = 1.0 / (1.0 / n_left + 1.0 / n_right)

            epsilon = np.sqrt(1.0 / (2.0 * m) * np.log(4.0 / self.delta))

            if abs(mean_left - mean_right) >= epsilon:
                # Drift detected: remove left sub-window
                removed = list(self._window)[:cut]
                for _ in range(cut):
                    self._window.popleft()
                self._sum = float(np.sum(self._window))
                logger.info(
                    "adwin_drift_detected",
                    cut_point=cut,
                    mean_diff=abs(mean_left - mean_right),
                    epsilon=epsilon,
                    new_window_size=len(self._window),
                )
                return True

        return False

    @property
    def window_size(self) -> int:
        """Current window size."""
        return len(self._window)

    @property
    def mean(self) -> float:
        """Current window mean."""
        if not self._window:
            return 0.0
        return self._sum / len(self._window)

    def reset(self) -> None:
        """Reset detector state."""
        self._window.clear()
        self._sum = 0.0


class ConceptDriftDetector:
    """Multi-algorithm concept drift detector.

    Combines multiple drift detection algorithms to provide robust
    detection with confidence scoring.
    """

    def __init__(
        self,
        metric_name: str,
        ph_delta: float = 0.005,
        ph_threshold: float = 50.0,
        ks_window: int = 100,
        ks_p_value: float = 0.01,
        adwin_delta: float = 0.002,
    ):
        self.metric_name = metric_name
        self._ph = PageHinkleyDetector(delta=ph_delta, threshold=ph_threshold)
        self._ks = KSDetector(window_size=ks_window, p_value_threshold=ks_p_value)
        self._adwin = ADWINDetector(delta=adwin_delta)
        self._drift_history: list[DriftResult] = []
        self._last_check_ts: int = 0

    def update(self, value: float, timestamp: int | None = None) -> DriftResult:
        """Update all detectors with new value.

        Args:
            value: New data point.
            timestamp: Optional timestamp (uses current time if None).

        Returns:
            DriftResult indicating if drift was detected.
        """
        ts = timestamp or int(time.time())
        self._last_check_ts = ts

        ph_drift = self._ph.update(value)
        ks_drift = self._ks.update(value)
        adwin_drift = self._adwin.update(value)

        # Count detectors that found drift
        drift_count = sum([ph_drift, ks_drift, adwin_drift])
        confidence = drift_count / 3.0

        drift_detected = drift_count >= 2  # Majority voting
        drift_type = self._classify_drift(ph_drift, ks_drift, adwin_drift)

        result = DriftResult(
            drift_detected=drift_detected,
            drift_type=drift_type,
            confidence=confidence,
            metric_name=self.metric_name,
            timestamp=ts,
            details={
                "page_hinkley": ph_drift,
                "ks_test": ks_drift,
                "adwin": adwin_drift,
                "drift_count": drift_count,
                "adwin_window_size": self._adwin.window_size,
            },
        )

        if drift_detected:
            self._drift_history.append(result)
            logger.warning(
                "concept_drift_detected",
                metric=self.metric_name,
                drift_type=drift_type,
                confidence=confidence,
                details=result.details,
            )

        return result

    def _classify_drift(
        self, ph: bool, ks: bool, adwin: bool
    ) -> str:
        """Classify drift type based on detector agreement."""
        count = sum([ph, ks, adwin])
        if count == 0:
            return "none"
        if count == 3:
            return "sudden"
        # Partial agreement suggests gradual drift
        return "gradual"

    def get_drift_history(self, limit: int = 100) -> list[DriftResult]:
        """Get recent drift detection history."""
        return self._drift_history[-limit:]

    def get_status(self) -> dict[str, Any]:
        """Get current detector status."""
        return {
            "metric_name": self.metric_name,
            "last_check_ts": self._last_check_ts,
            "total_drifts": len(self._drift_history),
            "adwin_window_size": self._adwin.window_size,
            "adwin_mean": self._adwin.mean,
        }

    def reset(self) -> None:
        """Reset all detectors."""
        self._ph.reset()
        self._ks.reset()
        self._adwin.reset()
        self._drift_history.clear()


class DriftDetectionManager:
    """Manager for multiple drift detectors across metrics."""

    def __init__(self):
        self._detectors: dict[str, ConceptDriftDetector] = {}

    def get_or_create_detector(
        self, metric_name: str, **kwargs
    ) -> ConceptDriftDetector:
        """Get or create a detector for a metric."""
        if metric_name not in self._detectors:
            self._detectors[metric_name] = ConceptDriftDetector(
                metric_name=metric_name, **kwargs
            )
        return self._detectors[metric_name]

    def update_metric(
        self, metric_name: str, value: float, timestamp: int | None = None
    ) -> DriftResult:
        """Update a metric's detector with new value."""
        detector = self.get_or_create_detector(metric_name)
        return detector.update(value, timestamp)

    def get_all_status(self) -> dict[str, dict[str, Any]]:
        """Get status of all detectors."""
        return {
            name: detector.get_status()
            for name, detector in self._detectors.items()
        }

    def get_recent_drifts(
        self, metric_name: str | None = None, limit: int = 50
    ) -> list[DriftResult]:
        """Get recent drift detections, optionally filtered by metric."""
        if metric_name and metric_name in self._detectors:
            return self._detectors[metric_name].get_drift_history(limit)

        all_drifts: list[DriftResult] = []
        for detector in self._detectors.values():
            all_drifts.extend(detector.get_drift_history(limit))

        # Sort by timestamp descending
        all_drifts.sort(key=lambda x: x.timestamp, reverse=True)
        return all_drifts[:limit]

    def remove_detector(self, metric_name: str) -> None:
        """Remove a detector for a metric."""
        self._detectors.pop(metric_name, None)

    def reset_all(self) -> None:
        """Reset all detectors."""
        for detector in self._detectors.values():
            detector.reset()
