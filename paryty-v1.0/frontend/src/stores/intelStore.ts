/**
 * Intel Store — Zustand state for Paryty Intelligence.
 *
 * Manages forecast data, anomaly detection results,
 * model accuracy metrics, and UI state for the Intel view.
 *
 * @module stores/intelStore
 */

import { create } from 'zustand';
import type {
  ForecastSeries,
  ForecastQuery,
  Anomaly,
  AnomalyDetectionStatus,
  ModelInfo,
  AnomalySeverity,
  WsAnomalyEvent,
  WsForecastEvent,
  WsDriftEvent,
} from '../types/intel';
import { SEVERITY_ORDER } from '../types/intel';
import { getRestClient } from '../api/rest';
import { useToastStore } from './toastStore';

// ─── Constants ──────────────────────────────────────────────────

/** Maximum forecast points per series (evenly sampled). */
const MAX_FORECAST_POINTS = 500;

/**
 * Evenly samples an array down to `limit` elements by stride.
 * Used for forecast point capping since ForecastPoint has extra fields
 * (lowerBound, upperBound) that LTTB doesn't handle.
 */
function sampleEvenly<T>(arr: T[], limit: number): T[] {
  if (arr.length <= limit) return arr;
  const result: T[] = [arr[0]];
  const step = (arr.length - 2) / (limit - 2);
  for (let i = 1; i < limit - 1; i++) {
    result.push(arr[Math.round(i * step)]);
  }
  result.push(arr[arr.length - 1]);
  return result;
}

// ─── Store Interface ─────────────────────────────────────────────

interface IntelState {
  // Data
  forecasts: Map<string, ForecastSeries>;
  anomalies: Anomaly[];
  detectionStatus: AnomalyDetectionStatus | null;
  modelAccuracy: Record<string, ModelInfo> | null;
  driftEvents: WsDriftEvent[];

  // Connection state
  isWsConnected: boolean;
  /** True once any data (REST or WS) has been received. */
  dataReceived: boolean;
  /** Metric names discovered from real data (no hardcoded list). */
  knownMetrics: string[];

  // UI State
  isLoading: boolean;
  isRetraining: boolean;
  error: string | null;
  selectedMetric: string;
  horizonSeconds: number;
  refreshInterval: number; // ms

  // Filters
  severityFilter: AnomalySeverity | null;
  metricFilter: string | null;

  // Actions — REST
  fetchForecasts: (metricNames: string[]) => Promise<void>;
  fetchAnomalies: () => Promise<void>;
  fetchDetectionStatus: () => Promise<void>;
  fetchModelAccuracy: () => Promise<void>;
  retrainModels: (metricName?: string) => Promise<void>;
  setSelectedMetric: (metric: string) => void;
  setHorizon: (seconds: number) => void;
  setRefreshInterval: (ms: number) => void;
  setSeverityFilter: (severity: AnomalySeverity | null) => void;
  setMetricFilter: (metric: string | null) => void;
  clear: () => void;

  // Actions — WebSocket
  handleWsAnomaly: (event: WsAnomalyEvent) => void;
  handleWsForecast: (event: WsForecastEvent) => void;
  handleWsDrift: (event: WsDriftEvent) => void;
  setWsConnected: (connected: boolean) => void;

  // Computed
  filteredAnomalies: () => Anomaly[];
  sortedAnomalies: () => Anomaly[];
  forecastForMetric: (metricName: string) => ForecastSeries | undefined;
}

// ─── Defaults ────────────────────────────────────────────────────

const DEFAULT_HORIZON_SECONDS = 604800; // 7 days
const DEFAULT_REFRESH_INTERVAL = 60000; // 60s

// ─── Store ───────────────────────────────────────────────────────

export const useIntelStore = create<IntelState>()((set, get) => ({
  // Data defaults
  forecasts: new Map<string, ForecastSeries>(),
  anomalies: [],
  detectionStatus: null,
  modelAccuracy: null,
  driftEvents: [],

  // Connection state
  isWsConnected: false,
  dataReceived: false,
  knownMetrics: [],

  // UI defaults
  isLoading: false,
  isRetraining: false,
  error: null,
  selectedMetric: '',
  horizonSeconds: DEFAULT_HORIZON_SECONDS,
  refreshInterval: DEFAULT_REFRESH_INTERVAL,

  // Filter defaults
  severityFilter: null,
  metricFilter: null,

  // ─── Actions ───────────────────────────────────────────────────

  /**
   * Fetch forecast series for the given metric names.
   * Uses batch endpoint when available for efficiency.
   */
  fetchForecasts: async (metricNames: string[]) => {
    const { horizonSeconds } = get();
    set({ isLoading: true, error: null });
    try {
      const client = getRestClient();
      const queries: ForecastQuery[] = metricNames.map((name) => ({
        metricName: name,
        horizonSeconds,
      }));
      const series = await client.getForecastBatch(queries);

      set((state) => {
        // Mutate existing Map in-place to avoid allocation,
        // then shallow-clone for Zustand reactivity trigger.
        for (const s of series) {
          const cappedPoints =
            s.points.length > MAX_FORECAST_POINTS
              ? sampleEvenly(s.points, MAX_FORECAST_POINTS)
              : s.points;
          state.forecasts.set(s.metricName, { ...s, points: cappedPoints });
        }
        return { forecasts: new Map(state.forecasts), isLoading: false };
      });
    } catch (err) {
      set({ error: (err as Error).message, isLoading: false });
    }
  },

  /**
   * Fetch recent anomalies from the detection API.
   */
  fetchAnomalies: async () => {
    set({ isLoading: true, error: null });
    try {
      const client = getRestClient();
      const status = await client.getAnomalyStatus();
      // If we have a status with anomaly count, also get detail
      // For now we get status and individual anomaly explanations
      // In a real flow, we'd have a list endpoint
      set({
        detectionStatus: status,
        isLoading: false,
      });
    } catch (err) {
      set({ error: (err as Error).message, isLoading: false });
    }
  },

  /**
   * Fetch anomaly detection subsystem status.
   */
  fetchDetectionStatus: async () => {
    try {
      const client = getRestClient();
      const status = await client.getAnomalyStatus();
      set({ detectionStatus: status });
    } catch (err) {
      set({ error: (err as Error).message });
    }
  },

  /**
   * Fetch model accuracy metrics.
   */
  fetchModelAccuracy: async () => {
    try {
      const client = getRestClient();
      const accuracy = await client.getModelAccuracy();
      set({ modelAccuracy: accuracy });
    } catch (err) {
      set({ error: (err as Error).message });
    }
  },

  /**
   * Trigger model retraining.
   */
  retrainModels: async (metricName?: string) => {
    set({ isRetraining: true, error: null });
    try {
      const client = getRestClient();
      await client.retrainModels(metricName, true);
      // Refresh accuracy after retraining
      const accuracy = await client.getModelAccuracy(metricName);
      set({ modelAccuracy: accuracy, isRetraining: false });
    } catch (err) {
      set({ error: (err as Error).message, isRetraining: false });
    }
  },

  // ─── WebSocket Event Handlers ──────────────────────────────────

  handleWsAnomaly: (event: WsAnomalyEvent) => {
    const newAnomalies: Anomaly[] = event.anomalies.map((a, idx) => ({
      id: `ws_${event.metric_name}_${event.agent_id}_${a.timestamp}_${idx}`,
      timestamp: new Date(a.timestamp * 1000).toISOString(),
      value: a.value,
      score: a.score,
      type: (a.type as Anomaly['type']) ?? 'point',
      explanation: a.explanation,
      contributingFactors: [],
      detectionMethod: a.detection_method,
      severity: (a.severity as AnomalySeverity) ?? 'medium',
      metricName: event.metric_name,
      agentId: event.agent_id,
    }));
    set((state) => {
      // Deduplicate by id
      const existingIds = new Set(state.anomalies.map((a) => a.id));
      const unique = newAnomalies.filter((a) => !existingIds.has(a.id));
      const merged = [...unique, ...state.anomalies].slice(0, 500); // cap at 500
      const known = state.knownMetrics.includes(event.metric_name)
        ? state.knownMetrics
        : [...state.knownMetrics, event.metric_name];

      // Show toast for new anomalies
      if (unique.length > 0) {
        const highestSeverity = unique.reduce(
          (max, a) => (SEVERITY_ORDER[a.severity] < SEVERITY_ORDER[max] ? a.severity : max),
          unique[0].severity,
        );
        const toastType = highestSeverity === 'critical' || highestSeverity === 'high'
          ? 'error'
          : highestSeverity === 'medium'
            ? 'warning'
            : 'info';
        useToastStore.getState().addToast({
          type: toastType,
          message: `${unique.length} new anomal${unique.length === 1 ? 'y' : 'ies'} detected on ${event.metric_name} (highest: ${highestSeverity})`,
          duration: 8000,
        });
      }

      return { anomalies: merged, dataReceived: true, knownMetrics: known };
    });
  },

  handleWsForecast: (event: WsForecastEvent) => {
    const series: ForecastSeries = {
      metricName: event.metric_name,
      agentId: event.agent_id,
      tenantId: event.tenant_id,
      points: event.forecast.forecast_points.map((p) => ({
        timestamp: new Date(p.timestamp * 1000).toISOString(),
        value: p.value,
        lowerBound: p.lower_bound,
        upperBound: p.upper_bound,
      })),
      modelInfo: {
        bestModel: event.forecast.model_info.best_model,
        weights: event.forecast.model_info.weights,
        accuracy: event.forecast.model_info.accuracy,
        lastTrained: new Date(event.forecast.model_info.last_trained * 1000).toISOString(),
        trainingSamples: event.forecast.model_info.training_samples,
      },
      overallConfidence: event.forecast.overall_confidence,
    };
    set((state) => {
      const newMap = new Map(state.forecasts);
      newMap.set(event.metric_name, series);
      const known = state.knownMetrics.includes(event.metric_name)
        ? state.knownMetrics
        : [...state.knownMetrics, event.metric_name];
      return { forecasts: newMap, dataReceived: true, knownMetrics: known };
    });
  },

  handleWsDrift: (event: WsDriftEvent) => {
    set((state) => ({
      driftEvents: [event, ...state.driftEvents].slice(0, 100),
      dataReceived: true,
    }));
  },

  setWsConnected: (connected: boolean) => {
    set({ isWsConnected: connected });
  },

  setSelectedMetric: (metric) => set({ selectedMetric: metric }),

  setHorizon: (seconds) => set({ horizonSeconds: seconds }),

  setRefreshInterval: (ms) => set({ refreshInterval: ms }),

  setSeverityFilter: (severity) => set({ severityFilter: severity }),

  setMetricFilter: (metric) => set({ metricFilter: metric }),

  clear: () =>
    set({
      forecasts: new Map(),
      anomalies: [],
      detectionStatus: null,
      modelAccuracy: null,
      driftEvents: [],
      error: null,
      dataReceived: false,
    }),

  // ─── Computed ──────────────────────────────────────────────────

  /**
   * Anomalies filtered by active severity and metric filters.
   */
  filteredAnomalies: () => {
    const { anomalies, severityFilter, metricFilter } = get();
    let result = anomalies;
    if (severityFilter) {
      result = result.filter((a) => a.severity === severityFilter);
    }
    if (metricFilter) {
      result = result.filter((a) => a.metricName === metricFilter);
    }
    return result;
  },

  /**
   * Anomalies sorted by severity (critical first), then by score descending.
   */
  sortedAnomalies: () => {
    const { filteredAnomalies } = get();
    const filtered = filteredAnomalies();
    return [...filtered].sort((a, b) => {
      const severityDiff =
        SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity];
      if (severityDiff !== 0) return severityDiff;
      return b.score - a.score;
    });
  },

  /**
   * Get the forecast series for a specific metric.
   */
  forecastForMetric: (metricName: string) => {
    return get().forecasts.get(metricName);
  },
}));
