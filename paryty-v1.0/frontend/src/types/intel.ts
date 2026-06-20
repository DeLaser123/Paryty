/**
 * Intel Types — Paryty Intelligence domain types.
 *
 * Forecast series, anomaly detection, and model accuracy
 * types for the intelligence pipeline.
 *
 * @module types/intel
 */

// ─── Forecast ────────────────────────────────────────────────────

/** A single point in a forecast series with confidence bounds. */
export interface ForecastPoint {
  /** ISO 8601 timestamp */
  timestamp: string;
  /** Predicted value at this timestamp */
  value: number;
  /** Lower bound of confidence interval */
  lowerBound: number;
  /** Upper bound of confidence interval */
  upperBound: number;
}

/** Metadata about the model that produced a forecast. */
export interface ModelInfo {
  /** Name of the best-performing model */
  bestModel: string;
  /** Ensemble weight per model name (0-1) */
  weights: Record<string, number>;
  /** Accuracy metric per model name (MAPE %) */
  accuracy: Record<string, number>;
  /** ISO 8601 timestamp of last training run */
  lastTrained: string;
  /** Number of training samples used */
  trainingSamples: number;
}

/** A complete forecast series for a single metric on a single agent. */
export interface ForecastSeries {
  /** Metric name, e.g. "cpu_usage_percent" */
  metricName: string;
  /** Agent that produced the source data */
  agentId: string;
  /** Tenant scope */
  tenantId: string;
  /** Ordered forecast data points */
  points: ForecastPoint[];
  /** Model metadata */
  modelInfo: ModelInfo;
  /** Overall confidence score (0-1) */
  overallConfidence: number;
}

// ─── Anomaly ─────────────────────────────────────────────────────

/** Anomaly type classification. */
export type AnomalyType = 'point' | 'contextual' | 'collective' | 'trend';

/** Anomaly severity level. */
export type AnomalySeverity = 'critical' | 'high' | 'medium' | 'low' | 'info';

/** A single detected anomaly. */
export interface Anomaly {
  /** Unique anomaly identifier */
  id: string;
  /** ISO 8601 timestamp when the anomaly occurred */
  timestamp: string;
  /** Metric value at the anomaly point */
  value: number;
  /** Anomaly score (0-1, higher = more anomalous) */
  score: number;
  /** Classification of the anomaly */
  type: AnomalyType;
  /** Human-readable explanation */
  explanation: string;
  /** List of contributing factors */
  contributingFactors: string[];
  /** Detection algorithm used */
  detectionMethod: string;
  /** Severity classification */
  severity: AnomalySeverity;
  /** Metric that was anomalous */
  metricName: string;
  /** Agent that reported the metric */
  agentId: string;
}

/** Status of the anomaly detection subsystem. */
export interface AnomalyDetectionStatus {
  /** Per-model detection status */
  models: Record<string, AnomalyModelStatus>;
  /** ISO 8601 timestamp of last training cycle */
  lastTraining: string;
  /** Number of anomalies detected in the last 24 hours */
  anomaliesDetected24h: number;
  /** Current false positive rate (0-1) */
  falsePositiveRate: number;
}

/** Status of a single anomaly detection model. */
export interface AnomalyModelStatus {
  /** Display name of the model */
  name: string;
  /** Whether the model has been trained */
  trained: boolean;
  /** Model accuracy (0-1) */
  accuracy: number;
  /** ISO 8601 timestamp of last model update */
  lastUpdated: string;
}

// ─── Query Parameters ────────────────────────────────────────────

/** Parameters for requesting a forecast. */
export interface ForecastQuery {
  /** Filter by agent ID */
  agentId?: string;
  /** Filter by tenant ID */
  tenantId?: string;
  /** Metric to forecast, e.g. "cpu_usage_percent" */
  metricName: string;
  /** Forecast horizon in seconds */
  horizonSeconds: number;
  /** Confidence level for intervals (default 0.95) */
  confidenceLevel?: number;
  /** Step size in seconds between forecast points */
  stepSeconds?: number;
}

// ─── Derived Helpers ─────────────────────────────────────────────

/** Severity sort order (critical first). */
export const SEVERITY_ORDER: Record<AnomalySeverity, number> = {
  critical: 0,
  high: 1,
  medium: 2,
  low: 3,
  info: 4,
};

/** Horizon presets for the UI selector. */
export interface HorizonPreset {
  label: string;
  seconds: number;
}

export const HORIZON_PRESETS: readonly HorizonPreset[] = [
  { label: '1h', seconds: 3600 },
  { label: '6h', seconds: 21600 },
  { label: '24h', seconds: 86400 },
  { label: '3d', seconds: 259200 },
  { label: '7d', seconds: 604800 },
] as const;

/** Key metrics for forecast summary cards. */
export const KEY_METRICS = [
  'cpu_usage_percent',
  'memory_usage_percent',
  'disk_usage_percent',
  'network_io_bytes',
] as const;

export type KeyMetric = (typeof KEY_METRICS)[number];

/** Display metadata for key metrics. */
export const KEY_METRIC_LABELS: Record<KeyMetric, string> = {
  cpu_usage_percent: 'CPU Usage',
  memory_usage_percent: 'Memory Usage',
  disk_usage_percent: 'Disk Usage',
  network_io_bytes: 'Network I/O',
};

// ─── WebSocket Payloads ─────────────────────────────────────────

/** Real-time anomaly event pushed via WebSocket. */
export interface WsAnomalyEvent {
  tenant_id: string;
  agent_id: string;
  metric_name: string;
  anomalies: Array<{
    timestamp: number;
    value: number;
    score: number;
    type: string;
    severity: string;
    explanation: string;
    detection_method: string;
  }>;
  source: string;
}

/** Real-time forecast event pushed via WebSocket. */
export interface WsForecastEvent {
  tenant_id: string;
  agent_id: string;
  metric_name: string;
  forecast: {
    overall_confidence: number;
    model_info: {
      best_model: string;
      weights: Record<string, number>;
      accuracy: Record<string, number>;
      last_trained: number;
      training_samples: number;
    };
    forecast_points: Array<{
      timestamp: number;
      value: number;
      lower_bound: number;
      upper_bound: number;
    }>;
  };
  source: string;
}

/** Real-time concept drift event pushed via WebSocket. */
export interface WsDriftEvent {
  tenant_id: string;
  agent_id: string;
  metric_name: string;
  drift_type: string;
  confidence: number;
  timestamp: number;
  source: string;
}
