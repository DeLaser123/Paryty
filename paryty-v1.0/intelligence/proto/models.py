"""
Plain Python dataclass models mirroring the protobuf definitions.

These dataclasses allow the intelligence service to work without
requiring proto compilation.  The gRPC servicers convert to/from
proto messages at the boundary.

Mirrors: proto/paryty/v1/forecasting.proto
Mirrors: proto/paryty/v1/anomaly.proto
Mirrors: proto/paryty/v1/common.proto
"""

from __future__ import annotations

import enum
from dataclasses import dataclass, field


# ---------------------------------------------------------------------------
# Common enums
# ---------------------------------------------------------------------------


class AnomalyTypeProto(str, enum.Enum):
    """Anomaly type enum matching proto AnomalyType."""

    UNSPECIFIED = "unspecified"
    POINT = "point"
    CONTEXTUAL = "contextual"
    COLLECTIVE = "collective"
    TREND = "trend"


class SeverityProto(str, enum.Enum):
    """Severity enum matching proto Severity."""

    UNSPECIFIED = "unspecified"
    CRITICAL = "critical"
    HIGH = "high"
    MEDIUM = "medium"
    LOW = "low"
    INFO = "info"


# ---------------------------------------------------------------------------
# Forecasting messages
# ---------------------------------------------------------------------------


@dataclass
class ForecastPoint:
    """Single forecast data point."""

    timestamp: int = 0
    value: float = 0.0
    lower_bound: float = 0.0
    upper_bound: float = 0.0


@dataclass
class ModelInfo:
    """Metadata about the forecasting model."""

    best_model: str = "ensemble"
    weights: dict[str, float] = field(default_factory=dict)
    accuracy: dict[str, float] = field(default_factory=dict)
    last_trained: int = 0
    training_samples: int = 0


@dataclass
class ForecastMetricRequest:
    """Request to forecast a single metric."""

    agent_id: str = ""
    tenant_id: str = ""
    metric_name: str = ""
    horizon_seconds: int = 604800
    confidence_level: int = 95
    step_seconds: int = 300


@dataclass
class ForecastMetricResponse:
    """Response for a single metric forecast."""

    agent_id: str = ""
    tenant_id: str = ""
    metric_name: str = ""
    forecast: list[ForecastPoint] = field(default_factory=list)
    model_info: ModelInfo = field(default_factory=ModelInfo)
    overall_confidence: float = 0.0


@dataclass
class ForecastBatchRequest:
    """Request to forecast multiple metrics."""

    requests: list[ForecastMetricRequest] = field(default_factory=list)


@dataclass
class ForecastBatchResponse:
    """Response for batch forecast."""

    forecasts: list[ForecastMetricResponse] = field(default_factory=list)


@dataclass
class GetModelAccuracyRequest:
    """Request model accuracy info."""

    metric_name: str = ""
    tenant_id: str = ""


@dataclass
class GetModelAccuracyResponse:
    """Response with model accuracy data."""

    models: dict[str, ModelInfo] = field(default_factory=dict)


@dataclass
class RetrainModelsRequest:
    """Request model retraining."""

    metric_name: str = ""
    tenant_id: str = ""
    force: bool = False


@dataclass
class RetrainModelsResponse:
    """Response confirming retraining."""

    success: bool = False
    message: str = ""
    updated_models: dict[str, ModelInfo] = field(default_factory=dict)


# ---------------------------------------------------------------------------
# Anomaly detection messages
# ---------------------------------------------------------------------------


@dataclass
class AnomalyProto:
    """A detected anomaly (proto mirror)."""

    timestamp: int = 0
    value: float = 0.0
    score: float = 0.0
    type: AnomalyTypeProto = AnomalyTypeProto.UNSPECIFIED
    explanation: str = ""
    contributing_factors: list[str] = field(default_factory=list)
    detection_method: str = ""
    severity: SeverityProto = SeverityProto.UNSPECIFIED


@dataclass
class DetectAnomaliesRequest:
    """Request anomaly detection on a single metric."""

    agent_id: str = ""
    tenant_id: str = ""
    metric_name: str = ""
    values: list[float] = field(default_factory=list)
    timestamps: list[int] = field(default_factory=list)
    sensitivity: float = 0.5


@dataclass
class DetectAnomaliesResponse:
    """Response with detected anomalies."""

    agent_id: str = ""
    tenant_id: str = ""
    metric_name: str = ""
    anomalies: list[AnomalyProto] = field(default_factory=list)
    overall_score: float = 0.0


@dataclass
class FloatArray:
    """Wrapper for repeated float (used in maps)."""

    values: list[float] = field(default_factory=list)


@dataclass
class DetectCrossMetricRequest:
    """Request cross-metric anomaly detection."""

    agent_id: str = ""
    tenant_id: str = ""
    metrics: dict[str, FloatArray] = field(default_factory=dict)
    timestamps: list[int] = field(default_factory=list)


@dataclass
class CorrelationAnomaly:
    """Anomaly in metric correlations."""

    metric_a: str = ""
    metric_b: str = ""
    description: str = ""
    severity: float = 0.0


@dataclass
class DetectCrossMetricResponse:
    """Response for cross-metric anomaly detection."""

    anomalies: list[AnomalyProto] = field(default_factory=list)
    correlation_anomalies: list[CorrelationAnomaly] = field(default_factory=list)


@dataclass
class ExplainAnomalyRequest:
    """Request anomaly explanation."""

    agent_id: str = ""
    tenant_id: str = ""
    metric_name: str = ""
    timestamp: int = 0


@dataclass
class ExplainAnomalyResponse:
    """Response with anomaly explanation."""

    anomaly: AnomalyProto = field(default_factory=AnomalyProto)
    similar_incidents: list[str] = field(default_factory=list)
    recommendations: list[str] = field(default_factory=list)


@dataclass
class GetDetectionStatusRequest:
    """Request detection model status."""

    tenant_id: str = ""


@dataclass
class ModelStatus:
    """Status of a single detection model."""

    name: str = ""
    trained: bool = False
    accuracy: float = 0.0
    last_updated: int = 0


@dataclass
class GetDetectionStatusResponse:
    """Response with detection model status."""

    models: dict[str, ModelStatus] = field(default_factory=dict)
    last_training: int = 0
    anomalies_detected_24h: int = 0
    false_positive_rate: float = 0.0
