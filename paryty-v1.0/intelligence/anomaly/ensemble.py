"""
Anomaly detection ensemble with weighted voting.

Combines results from Statistical, Isolation Forest, and Autoencoder
detectors using a weighted vote scheme.

DEFAULT WEIGHTS:
    * Statistical:     0.30
    * Isolation Forest: 0.35
    * Autoencoder:      0.35

A point is flagged as anomalous when the weighted vote fraction exceeds
the configured threshold (default 0.5).  Confidence is computed as the
number of detectors that flagged the point divided by the total.
"""

from __future__ import annotations

from typing import Sequence

import numpy as np
import structlog

from .config import AnomalyConfig
from .statistical import AnomalyResult, Severity, _severity_from_score

logger = structlog.get_logger(__name__)

_DEFAULT_WEIGHTS: dict[str, float] = {
    "statistical": 0.30,
    "isolation_forest": 0.35,
    "autoencoder": 0.35,
}


class AnomalyEnsemble:
    """Weighted voting ensemble for anomaly detection.

    Each detector produces a list of :class:`AnomalyResult` keyed by
    timestamp.  The ensemble merges them, computes a weighted vote
    per timestamp, and flags points where the vote exceeds the threshold.
    """

    def __init__(self, config: AnomalyConfig | None = None) -> None:
        cfg = config or AnomalyConfig()
        self._weights: dict[str, float] = dict(cfg.ensemble.weights)
        self._vote_threshold: float = cfg.ensemble.vote_threshold

    def combine(
        self,
        statistical_results: Sequence[AnomalyResult],
        isolation_results: Sequence[AnomalyResult],
        autoencoder_results: Sequence[AnomalyResult],
        n_samples: int,
    ) -> list[AnomalyResult]:
        """Combine detector outputs via weighted voting.

        Args:
            statistical_results: Anomalies from the statistical detector.
            isolation_results: Anomalies from the Isolation Forest.
            autoencoder_results: Anomalies from the Autoencoder.
            n_samples: Total number of data points (for context).

        Returns:
            Merged list of :class:`AnomalyResult` where the weighted vote
            exceeds ``vote_threshold``.
        """
        # Build per-detector timestamp→score maps
        stat_by_ts: dict[int, float] = {
            r.timestamp: r.score for r in statistical_results
        }
        if_by_ts: dict[int, float] = {
            r.timestamp: r.score for r in isolation_results
        }
        ae_by_ts: dict[int, float] = {
            r.timestamp: r.score for r in autoencoder_results
        }

        # All flagged timestamps
        all_timestamps = set(stat_by_ts) | set(if_by_ts) | set(ae_by_ts)

        # Build lookup for explanations and values
        detail_by_ts: dict[int, AnomalyResult] = {}
        for r in list(statistical_results) + list(isolation_results) + list(autoencoder_results):
            existing = detail_by_ts.get(r.timestamp)
            if existing is None or r.score > existing.score:
                detail_by_ts[r.timestamp] = r

        results: list[AnomalyResult] = []

        for ts in sorted(all_timestamps):
            # Weighted vote
            vote = 0.0
            detectors_flagged: list[str] = []

            stat_score = stat_by_ts.get(ts, 0.0)
            if stat_score > 0:
                vote += self._weights.get("statistical", 0.30)
                detectors_flagged.append("statistical")

            if_score = if_by_ts.get(ts, 0.0)
            if if_score > 0:
                vote += self._weights.get("isolation_forest", 0.35)
                detectors_flagged.append("isolation_forest")

            ae_score = ae_by_ts.get(ts, 0.0)
            if ae_score > 0:
                vote += self._weights.get("autoencoder", 0.35)
                detectors_flagged.append("autoencoder")

            if vote < self._vote_threshold:
                continue

            # Confidence = fraction of detectors that flagged
            confidence = len(detectors_flagged) / 3.0

            # Combined score: weighted average of individual scores
            scores = [
                s
                for s in [stat_score, if_score, ae_score]
                if s > 0
            ]
            combined_score = float(np.mean(scores)) if scores else 0.0

            detail = detail_by_ts.get(ts)
            if detail is None:
                continue

            # Merge contributing factors
            all_factors: list[str] = []
            for r in [stat_by_ts, if_by_ts, ae_by_ts]:
                pass  # gather from detail_by_ts
            for r in list(statistical_results) + list(isolation_results) + list(autoencoder_results):
                if r.timestamp == ts:
                    all_factors.extend(
                        [f"[{r.method}] {f}" for f in r.contributing_factors[:2]]
                    )

            explanation = (
                f"Ensemble anomaly (vote={vote:.2f}, "
                f"confidence={confidence:.2f}): detected by "
                f"{', '.join(detectors_flagged)}. "
                f"{detail.explanation}"
            )

            results.append(
                AnomalyResult(
                    timestamp=ts,
                    value=detail.value,
                    score=combined_score,
                    anomaly_type=detail.anomaly_type,
                    explanation=explanation,
                    method="ensemble",
                    severity=_severity_from_score(combined_score),
                    contributing_factors=all_factors[:6],
                )
            )

        logger.debug(
            "anomaly_ensemble_combined",
            total_timestamps=len(all_timestamps),
            flagged=len(results),
            n_samples=n_samples,
        )

        return results

    @property
    def weights(self) -> dict[str, float]:
        """Current ensemble weights."""
        return dict(self._weights)

    def update_weights(self, new_weights: dict[str, float]) -> None:
        """Update ensemble weights.

        Weights are normalised to sum to 1.0.
        """
        total = sum(new_weights.values())
        if total > 0:
            self._weights = {k: v / total for k, v in new_weights.items()}
        else:
            self._weights = dict(_DEFAULT_WEIGHTS)
