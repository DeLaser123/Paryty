"""
Anomaly explanation generator.

Produces human-readable explanations for detected anomalies, including:
    * Root cause hints based on anomaly characteristics.
    * Similar incident lookup (pattern matching against past anomalies).
    * Actionable recommendations for investigation and remediation.
"""

from __future__ import annotations

import time
from collections import deque
from dataclasses import dataclass, field

import structlog

from .statistical import AnomalyResult, AnomalyType, Severity

logger = structlog.get_logger(__name__)


@dataclass
class Explanation:
    """Full explanation for an anomaly.

    Attributes:
        summary: One-line summary.
        details: Multi-line detailed explanation.
        root_cause_hints: Possible root causes.
        similar_incidents: Past anomalies with similar characteristics.
        recommendations: Actionable next steps.
        blast_radius: Estimated impact scope.
    """

    summary: str = ""
    details: str = ""
    root_cause_hints: list[str] = field(default_factory=list)
    similar_incidents: list[str] = field(default_factory=list)
    recommendations: list[str] = field(default_factory=list)
    blast_radius: str = ""


# ---------------------------------------------------------------------------
# Root cause templates
# ---------------------------------------------------------------------------

_METRIC_ROOT_CAUSES: dict[str, list[str]] = {
    "cpu_usage": [
        "Runaway process or infinite loop",
        "Spike in request traffic (check ingress metrics)",
        "Background job or cron task consuming CPU",
        "Container CPU throttling causing queue buildup",
        "Cryptominer or unauthorised workload",
    ],
    "memory_usage": [
        "Memory leak in application code",
        "Excessive caching without eviction policy",
        "Large batch job loading data into memory",
        "Connection pool exhaustion",
        "JVM heap not tuned (if Java workload)",
    ],
    "disk_usage": [
        "Log files growing unbounded",
        "Docker images / build artifacts accumulating",
        "Database WAL or temp files growing",
        "Backup files not being rotated",
        "Orphaned volumes or snapshots",
    ],
    "network_io": [
        "DDoS or traffic spike",
        "Large file transfer or backup",
        "Misconfigured retry loop causing traffic amplification",
        "Service mesh misconfiguration",
        "DNS resolution loop",
    ],
    "disk_io": [
        "Database full table scan",
        "Excessive logging to disk",
        "Swap thrashing (memory pressure)",
        "RAID rebuild in progress",
        "Anti-virus or security scan",
    ],
}

_DEFAULT_ROOT_CAUSES: list[str] = [
    "Unexpected change in workload pattern",
    "Resource contention with co-located services",
    "External dependency degradation",
    "Configuration change or deployment",
    "Infrastructure issue (host, network, storage)",
]

# ---------------------------------------------------------------------------
# Recommendation templates
# ---------------------------------------------------------------------------

_SEVERITY_RECOMMENDATIONS: dict[Severity, list[str]] = {
    Severity.CRITICAL: [
        "IMMEDIATE: Page on-call engineer for investigation",
        "Check service health dashboards for cascading failures",
        "Review recent deployments and configuration changes",
        "Consider scaling up or enabling auto-scaling if available",
        "Prepare rollback plan if issue is deployment-related",
    ],
    Severity.WARN: [
        "Investigate within the next 30 minutes",
        "Check application logs for error patterns",
        "Review resource usage trends over the past hour",
        "Verify dependent services are healthy",
    ],
    Severity.INFO: [
        "Monitor — no immediate action required",
        "Add to next standup for discussion",
        "Consider adding alerting threshold if recurring",
    ],
}

# ---------------------------------------------------------------------------
# Similar incident patterns
# ---------------------------------------------------------------------------


@dataclass
class _IncidentRecord:
    """Stored incident for similarity matching."""

    timestamp: int
    metric: str
    anomaly_type: AnomalyType
    severity: Severity
    value_range: tuple[float, float]
    description: str


class AnomalyExplainer:
    """Generate human-readable explanations for anomalies.

    Maintains a rolling history of recent anomalies (last 1000) to
    enable similar-incident lookup.  Explanations are generated from
    templates that are customised with anomaly-specific details.
    """

    def __init__(self, history_size: int = 1000) -> None:
        self._history: deque[_IncidentRecord] = deque(maxlen=history_size)

    def explain(
        self,
        anomaly: AnomalyResult,
        metric_name: str = "",
        metric_context: dict[str, float] | None = None,
    ) -> Explanation:
        """Generate a full explanation for the given anomaly.

        Args:
            anomaly: The detected anomaly.
            metric_name: Name of the metric (e.g. "cpu_usage").
            metric_context: Additional context (mean, std, min, max, etc.).

        Returns:
            :class:`Explanation` with summary, details, hints, incidents,
            and recommendations.
        """
        ctx = metric_context or {}

        # Summary
        summary = self._build_summary(anomaly, metric_name)

        # Details
        details = self._build_details(anomaly, metric_name, ctx)

        # Root cause hints
        hints = self._get_root_cause_hints(anomaly, metric_name)

        # Similar incidents
        similar = self._find_similar_incidents(anomaly, metric_name)

        # Recommendations
        recommendations = self._get_recommendations(anomaly)

        # Blast radius
        blast_radius = self._estimate_blast_radius(anomaly, metric_name)

        # Store for future similarity matching
        value_range = (anomaly.value * 0.8, anomaly.value * 1.2)
        self._history.append(
            _IncidentRecord(
                timestamp=anomaly.timestamp,
                metric=metric_name,
                anomaly_type=anomaly.anomaly_type,
                severity=anomaly.severity,
                value_range=value_range,
                description=anomaly.explanation,
            )
        )

        return Explanation(
            summary=summary,
            details=details,
            root_cause_hints=hints,
            similar_incidents=similar,
            recommendations=recommendations,
            blast_radius=blast_radius,
        )

    # ------------------------------------------------------------------
    # Internal builders
    # ------------------------------------------------------------------

    def _build_summary(
        self,
        anomaly: AnomalyResult,
        metric_name: str,
    ) -> str:
        """Build a one-line summary."""
        metric_label = metric_name or "metric"
        type_label = anomaly.anomaly_type.value
        sev_label = anomaly.severity.value.upper()
        return (
            f"[{sev_label}] {type_label} anomaly on {metric_label}: "
            f"value={anomaly.value:.2f}, score={anomaly.score:.2f}"
        )

    def _build_details(
        self,
        anomaly: AnomalyResult,
        metric_name: str,
        context: dict[str, float],
    ) -> str:
        """Build multi-line detailed explanation."""
        lines = [
            f"Anomaly Type: {anomaly.anomaly_type.value}",
            f"Detection Method: {anomaly.method}",
            f"Anomaly Score: {anomaly.score:.3f}",
            f"Value: {anomaly.value:.2f}",
            f"Timestamp: {anomaly.timestamp}",
        ]

        if context:
            mean = context.get("mean", 0)
            std = context.get("std", 0)
            lines.append(f"Context: mean={mean:.2f}, std={std:.2f}")
            if std > 0:
                z = abs(anomaly.value - mean) / std
                lines.append(f"Z-score: {z:.1f}σ")

        if anomaly.contributing_factors:
            lines.append("Contributing factors:")
            for f in anomaly.contributing_factors:
                lines.append(f"  - {f}")

        if anomaly.explanation:
            lines.append(f"Explanation: {anomaly.explanation}")

        return "\n".join(lines)

    def _get_root_cause_hints(
        self,
        anomaly: AnomalyResult,
        metric_name: str,
    ) -> list[str]:
        """Select relevant root cause hints."""
        hints = list(_METRIC_ROOT_CAUSES.get(metric_name, _DEFAULT_ROOT_CAUSES))

        # Filter based on anomaly type
        if anomaly.anomaly_type == AnomalyType.TREND:
            hints.insert(0, "Gradual resource exhaustion or slow leak")
            hints.insert(1, "Traffic pattern shift over time")
        elif anomaly.anomaly_type == AnomalyType.COLLECTIVE:
            hints.insert(0, "Cascading failure across dependent services")
            hints.insert(1, "Sustained overload condition")
        elif anomaly.anomaly_type == AnomalyType.CONTEXTUAL:
            hints.insert(0, "Value is normal in isolation but abnormal in context")
            hints.insert(1, "Correlation with other metric changes")

        return hints[:5]

    def _find_similar_incidents(
        self,
        anomaly: AnomalyResult,
        metric_name: str,
    ) -> list[str]:
        """Look for similar past anomalies."""
        similar: list[str] = []

        for record in self._history:
            if record.metric != metric_name:
                continue
            if record.anomaly_type != anomaly.anomaly_type:
                continue

            # Check value range similarity
            if (
                record.value_range[0] <= anomaly.value <= record.value_range[1]
                and record.severity == anomaly.severity
            ):
                ts_str = time.strftime(
                    "%Y-%m-%d %H:%M", time.localtime(record.timestamp)
                )
                similar.append(
                    f"{ts_str}: {record.description} "
                    f"(severity={record.severity.value})"
                )

        return similar[:5]

    def _get_recommendations(self, anomaly: AnomalyResult) -> list[str]:
        """Select recommendations based on severity."""
        recs = list(_SEVERITY_RECOMMENDATIONS.get(anomaly.severity, []))

        # Add method-specific recs
        if anomaly.method == "autoencoder" and anomaly.anomaly_type == AnomalyType.COLLECTIVE:
            recs.insert(0, "Pattern anomaly detected — check for cascading failures")
        elif anomaly.method == "ewma" and anomaly.anomaly_type == AnomalyType.TREND:
            recs.insert(0, "Trend change detected — investigate gradual drift")

        return recs[:5]

    def _estimate_blast_radius(
        self,
        anomaly: AnomalyResult,
        metric_name: str,
    ) -> str:
        """Estimate the blast radius of the anomaly."""
        if anomaly.severity == Severity.CRITICAL:
            if metric_name in ("cpu_usage", "memory_usage"):
                return "HIGH — may affect all services on this host"
            if metric_name == "network_io":
                return "HIGH — network issue may cascade to dependent services"
            return "MEDIUM — single metric critical, monitor related metrics"
        if anomaly.severity == Severity.WARN:
            return "LOW-MEDIUM — isolated to this metric, monitor for escalation"
        return "LOW — informational, no immediate impact expected"
