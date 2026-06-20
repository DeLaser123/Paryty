"""
Thread-safe metric buffer with count-gating and forecast time-gating.

The buffer tracks incoming data points per (tenant, agent, metric) triple.
When the count reaches a configurable threshold it signals the pipeline to
run anomaly detection.  Separately, it tracks the last forecast timestamp
per (tenant, metric) pair so the pipeline can time-gate forecast generation.
"""

from __future__ import annotations

import threading
import time
from collections import defaultdict

import structlog

logger = structlog.get_logger(__name__)


class MetricBuffer:
    """Per-metric data point counter and forecast scheduler.

    All public methods are thread-safe.
    """

    def __init__(self, min_points: int = 30) -> None:
        self._min_points = min_points
        # count per (tenant, agent, metric) triple
        self._counts: dict[tuple[str, str, str], int] = defaultdict(int)
        # last forecast epoch per (tenant, metric) pair
        self._last_forecast: dict[tuple[str, str], float] = {}
        # registered (tenant, metric) pairs for retrain iteration
        self._known_metrics: set[tuple[str, str]] = set()
        self._lock = threading.Lock()

    # ------------------------------------------------------------------
    # Metric counting
    # ------------------------------------------------------------------

    def add(self, tenant_id: str, agent_id: str, metric_name: str) -> bool:
        """Record an incoming data point.

        Returns ``True`` when the buffer has accumulated at least
        ``min_points`` for this triple — signalling the caller to
        trigger anomaly detection.  The counter is then reset so the
        next detection fires after another ``min_points`` arrivals.
        """
        key = (tenant_id, agent_id, metric_name)
        with self._lock:
            self._counts[key] += 1
            # Register for known-metrics iteration
            self._known_metrics.add((tenant_id, metric_name))
            if self._counts[key] >= self._min_points:
                self._counts[key] = 0
                return True
        return False

    # ------------------------------------------------------------------
    # Forecast time-gating
    # ------------------------------------------------------------------

    def should_forecast(
        self,
        tenant_id: str,
        metric_name: str,
        interval_seconds: float,
    ) -> bool:
        """Return ``True`` if enough time has elapsed since the last forecast."""
        key = (tenant_id, metric_name)
        with self._lock:
            last = self._last_forecast.get(key, 0.0)
            return (time.time() - last) >= interval_seconds

    def mark_forecast(self, tenant_id: str, metric_name: str) -> None:
        """Record that a forecast was just generated for this metric."""
        key = (tenant_id, metric_name)
        with self._lock:
            self._last_forecast[key] = time.time()

    # ------------------------------------------------------------------
    # Introspection
    # ------------------------------------------------------------------

    def get_known_metrics(self) -> list[tuple[str, str]]:
        """Return all registered (tenant_id, metric_name) pairs."""
        with self._lock:
            return list(self._known_metrics)

    @property
    def active_metrics(self) -> int:
        """Number of unique (tenant, metric) pairs seen."""
        with self._lock:
            return len(self._known_metrics)

    def reset(self) -> None:
        """Clear all state (for testing)."""
        with self._lock:
            self._counts.clear()
            self._last_forecast.clear()
            self._known_metrics.clear()
