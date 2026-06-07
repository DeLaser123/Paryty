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
} from '../types/intel';
import { SEVERITY_ORDER } from '../types/intel';
import { getRestClient } from '../api/rest';

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

  // Actions
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

  // UI defaults
  isLoading: false,
  isRetraining: false,
  error: null,
  selectedMetric: 'cpu_usage_percent',
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
      error: null,
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
