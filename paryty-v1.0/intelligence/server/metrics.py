"""
Prometheus metrics for the Intelligence Service.

Exposes counters, histograms, and gauges for:
- gRPC request counts and latency
- ML model fit/predict durations
- Anomaly detection counts
- Forecast generation counts
- Error counts
"""

from __future__ import annotations

try:
    from prometheus_client import Counter, Histogram, Gauge, Info
    _HAS_PROM = True
except ImportError:
    _HAS_PROM = False

# ---------------------------------------------------------------------------
# gRPC request metrics
# ---------------------------------------------------------------------------

GRPC_REQUESTS = Counter(
    "intelligence_grpc_requests_total",
    "Total gRPC requests",
    ["method", "status"],
) if _HAS_PROM else None

GRPC_REQUEST_LATENCY = Histogram(
    "intelligence_grpc_request_duration_seconds",
    "gRPC request latency in seconds",
    ["method"],
    buckets=[0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0],
) if _HAS_PROM else None

# ---------------------------------------------------------------------------
# ML model metrics
# ---------------------------------------------------------------------------

MODEL_FIT_DURATION = Histogram(
    "intelligence_model_fit_duration_seconds",
    "Model training duration in seconds",
    ["model_type", "metric_name"],
    buckets=[0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0],
) if _HAS_PROM else None

MODEL_PREDICT_DURATION = Histogram(
    "intelligence_model_predict_duration_seconds",
    "Model prediction duration in seconds",
    ["model_type"],
    buckets=[0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 5.0],
) if _HAS_PROM else None

# ---------------------------------------------------------------------------
# Anomaly detection metrics
# ---------------------------------------------------------------------------

ANOMALIES_DETECTED = Counter(
    "intelligence_anomalies_detected_total",
    "Total anomalies detected",
    ["tenant_id", "metric_name", "severity"],
) if _HAS_PROM else None

DETECTION_TIMEOUTS = Counter(
    "intelligence_detection_timeouts_total",
    "Total anomaly detection timeouts",
) if _HAS_PROM else None

# ---------------------------------------------------------------------------
# Forecasting metrics
# ---------------------------------------------------------------------------

FORECASTS_GENERATED = Counter(
    "intelligence_forecasts_generated_total",
    "Total forecasts generated",
    ["tenant_id", "metric_name", "model"],
) if _HAS_PROM else None

FORECAST_TIMEOUTS = Counter(
    "intelligence_forecast_timeouts_total",
    "Total forecast timeouts",
) if _HAS_PROM else None

# ---------------------------------------------------------------------------
# System metrics
# ---------------------------------------------------------------------------

ACTIVE_DETECTORS = Gauge(
    "intelligence_active_detectors",
    "Number of active anomaly detectors",
    ["detector_type"],
) if _HAS_PROM else None

ACTIVE_ENSEMBLES = Gauge(
    "intelligence_active_ensembles",
    "Number of active forecasting ensembles",
) if _HAS_PROM else None

CACHED_MODELS = Gauge(
    "intelligence_cached_models",
    "Number of cached ML models",
    ["model_type"],
) if _HAS_PROM else None

# ---------------------------------------------------------------------------
# Pipeline streaming metrics
# ---------------------------------------------------------------------------

PIPELINE_METRICS_CONSUMED = Counter(
    "intelligence_pipeline_metrics_consumed_total",
    "Total metrics consumed by the pipeline",
    ["tenant_id", "metric_name"],
) if _HAS_PROM else None

PIPELINE_ANOMALIES_TRIGGERED = Counter(
    "intelligence_pipeline_anomalies_triggered_total",
    "Pipeline-triggered anomaly detections",
    ["tenant_id"],
) if _HAS_PROM else None

PIPELINE_FORECASTS_TRIGGERED = Counter(
    "intelligence_pipeline_forecasts_triggered_total",
    "Pipeline-triggered forecast generations",
    ["tenant_id"],
) if _HAS_PROM else None

PIPELINE_DRIFT_DETECTIONS = Counter(
    "intelligence_drift_detections_total",
    "Concept drift events detected",
    ["metric_name", "drift_type"],
) if _HAS_PROM else None

PIPELINE_ERRORS = Counter(
    "intelligence_pipeline_errors_total",
    "Pipeline processing errors",
    ["stage"],
) if _HAS_PROM else None

PIPELINE_ACTIVE_METRICS = Gauge(
    "intelligence_pipeline_active_metrics",
    "Number of unique (tenant, metric) pairs tracked by pipeline",
) if _HAS_PROM else None

SERVICE_INFO = Info(
    "intelligence_service",
    "Intelligence service metadata",
) if _HAS_PROM else None


def record_grpc_request(method: str, status: str, duration: float) -> None:
    """Record a gRPC request."""
    if GRPC_REQUESTS is not None:
        GRPC_REQUESTS.labels(method=method, status=status).inc()
    if GRPC_REQUEST_LATENCY is not None:
        GRPC_REQUEST_LATENCY.labels(method=method).observe(duration)


def record_model_fit(model_type: str, metric_name: str, duration: float) -> None:
    """Record model training duration."""
    if MODEL_FIT_DURATION is not None:
        MODEL_FIT_DURATION.labels(model_type=model_type, metric_name=metric_name).observe(duration)


def record_model_predict(model_type: str, duration: float) -> None:
    """Record model prediction duration."""
    if MODEL_PREDICT_DURATION is not None:
        MODEL_PREDICT_DURATION.labels(model_type=model_type).observe(duration)


def record_anomaly_detected(tenant_id: str, metric_name: str, severity: str) -> None:
    """Record an anomaly detection event."""
    if ANOMALIES_DETECTED is not None:
        ANOMALIES_DETECTED.labels(tenant_id=tenant_id, metric_name=metric_name, severity=severity).inc()


def record_forecast_generated(tenant_id: str, metric_name: str, model: str) -> None:
    """Record a forecast generation event."""
    if FORECASTS_GENERATED is not None:
        FORECASTS_GENERATED.labels(tenant_id=tenant_id, metric_name=metric_name, model=model).inc()


def record_detection_timeout() -> None:
    """Record a detection timeout."""
    if DETECTION_TIMEOUTS is not None:
        DETECTION_TIMEOUTS.inc()


def record_forecast_timeout() -> None:
    """Record a forecast timeout."""
    if FORECAST_TIMEOUTS is not None:
        FORECAST_TIMEOUTS.inc()


def update_detector_counts(
    statistical: int = 0,
    isolation_forest: int = 0,
    autoencoder: int = 0,
) -> None:
    """Update active detector gauges."""
    if ACTIVE_DETECTORS is not None:
        ACTIVE_DETECTORS.labels(detector_type="statistical").set(statistical)
        ACTIVE_DETECTORS.labels(detector_type="isolation_forest").set(isolation_forest)
        ACTIVE_DETECTORS.labels(detector_type="autoencoder").set(autoencoder)


def update_ensemble_count(count: int) -> None:
    """Update active ensemble gauge."""
    if ACTIVE_ENSEMBLES is not None:
        ACTIVE_ENSEMBLES.set(count)


# ---------------------------------------------------------------------------
# Pipeline recording functions
# ---------------------------------------------------------------------------

def record_metric_consumed(tenant_id: str, metric_name: str) -> None:
    """Record a metric consumed by the pipeline."""
    if PIPELINE_METRICS_CONSUMED is not None:
        PIPELINE_METRICS_CONSUMED.labels(tenant_id=tenant_id, metric_name=metric_name).inc()


def record_pipeline_anomaly(tenant_id: str) -> None:
    """Record a pipeline-triggered anomaly detection."""
    if PIPELINE_ANOMALIES_TRIGGERED is not None:
        PIPELINE_ANOMALIES_TRIGGERED.labels(tenant_id=tenant_id).inc()


def record_pipeline_forecast(tenant_id: str) -> None:
    """Record a pipeline-triggered forecast."""
    if PIPELINE_FORECASTS_TRIGGERED is not None:
        PIPELINE_FORECASTS_TRIGGERED.labels(tenant_id=tenant_id).inc()


def record_drift_detection(metric_name: str, drift_type: str) -> None:
    """Record a concept drift detection event."""
    if PIPELINE_DRIFT_DETECTIONS is not None:
        PIPELINE_DRIFT_DETECTIONS.labels(metric_name=metric_name, drift_type=drift_type).inc()


def record_pipeline_error(stage: str) -> None:
    """Record a pipeline processing error."""
    if PIPELINE_ERRORS is not None:
        PIPELINE_ERRORS.labels(stage=stage).inc()


def update_pipeline_active_metrics(count: int) -> None:
    """Update the gauge of active pipeline metrics."""
    if PIPELINE_ACTIVE_METRICS is not None:
        PIPELINE_ACTIVE_METRICS.set(count)
