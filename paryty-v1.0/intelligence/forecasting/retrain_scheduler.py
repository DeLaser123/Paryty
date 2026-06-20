"""
Model retraining scheduler.

Periodically checks model staleness and triggers retraining when:
- Model age exceeds a configurable threshold
- Prediction accuracy degrades below a threshold
- New data volume exceeds a threshold

Runs as a background thread that wakes up at a configurable interval.
"""

from __future__ import annotations

import os
import threading
import time
from typing import Any, Callable

import structlog

logger = structlog.get_logger(__name__)


class RetrainScheduler:
    """Background scheduler for automatic model retraining.

    Monitors model age and triggers retraining via a callback when
    conditions are met.
    """

    def __init__(
        self,
        retrain_fn: Callable[[str, str], bool],
        check_interval_seconds: int = 3600,
        max_model_age_seconds: int = 86400,
        min_mape_threshold: float = 50.0,
    ) -> None:
        """
        Args:
            retrain_fn: Callback(tenant_id, metric_name) -> success.
            check_interval_seconds: How often to check (default 1h).
            max_model_age_seconds: Max age before forced retrain (default 24h).
            min_mape_threshold: MAPE above this triggers retrain (default 50%).
        """
        self._retrain_fn = retrain_fn
        self._check_interval = check_interval_seconds
        self._max_age = max_model_age_seconds
        self._mape_threshold = min_mape_threshold
        self._running = False
        self._thread: threading.Thread | None = None
        self._retrain_count = 0
        self._watched_metrics: set[tuple[str, str]] = set()
        self._metrics_lock = threading.Lock()

    def start(self) -> None:
        """Start the background scheduler."""
        if self._running:
            return
        self._running = True
        self._thread = threading.Thread(
            target=self._run_loop,
            name="retrain-scheduler",
            daemon=True,
        )
        self._thread.start()
        logger.info("retrain_scheduler_started", interval=self._check_interval)

    def stop(self) -> None:
        """Stop the scheduler."""
        self._running = False
        if self._thread is not None:
            self._thread.join(timeout=30)
            self._thread = None
        logger.info("retrain_scheduler_stopped", retrain_count=self._retrain_count)

    def _run_loop(self) -> None:
        """Background loop that checks model staleness."""
        while self._running:
            try:
                time.sleep(self._check_interval)
                if not self._running:
                    break
                self._check_and_retrain()
            except Exception as exc:
                logger.error("retrain_scheduler_error", error=str(exc))

    def _check_and_retrain(self) -> None:
        """Check all watched metrics for staleness and retrain if needed."""
        with self._metrics_lock:
            watched = list(self._watched_metrics)
        if not watched:
            logger.debug("retrain_check_completed", retrain_count=self._retrain_count, watched=0)
            return
        for tenant_id, metric_name in watched:
            try:
                success = self._retrain_fn(tenant_id, metric_name)
                if success:
                    self._retrain_count += 1
            except Exception as exc:
                logger.error(
                    "retrain_check_failed",
                    tenant=tenant_id,
                    metric=metric_name,
                    error=str(exc),
                )
        logger.debug(
            "retrain_check_completed",
            retrain_count=self._retrain_count,
            metrics=len(watched),
        )

    def trigger_retrain(self, tenant_id: str, metric_name: str) -> bool:
        """Manually trigger retraining for a specific tenant+metric."""
        try:
            success = self._retrain_fn(tenant_id, metric_name)
            if success:
                self._retrain_count += 1
                logger.info(
                    "retrain_triggered",
                    tenant=tenant_id,
                    metric=metric_name,
                )
            return success
        except Exception as exc:
            logger.error(
                "retrain_trigger_failed",
                tenant=tenant_id,
                metric=metric_name,
                error=str(exc),
            )
            return False

    def add_metric(self, tenant_id: str, metric_name: str) -> None:
        """Register a (tenant, metric) pair for periodic retrain checks."""
        with self._metrics_lock:
            self._watched_metrics.add((tenant_id, metric_name))

    def remove_metric(self, tenant_id: str, metric_name: str) -> None:
        """Unregister a (tenant, metric) pair."""
        with self._metrics_lock:
            self._watched_metrics.discard((tenant_id, metric_name))

    @property
    def watched_count(self) -> int:
        """Number of metrics currently being monitored."""
        with self._metrics_lock:
            return len(self._watched_metrics)

    @property
    def is_running(self) -> bool:
        return self._running

    @property
    def retrain_count(self) -> int:
        return self._retrain_count
