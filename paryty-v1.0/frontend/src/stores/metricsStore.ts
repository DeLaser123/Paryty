import { create } from 'zustand';
import type { MetricSeries, AggregatedMetric } from '../types/metric';
import type { TimeRange } from '../types/common';

interface MetricsState {
  // Data
  series: MetricSeries[];
  aggregated: AggregatedMetric[];
  selectedMetric: string | null;
  timeRange: TimeRange;
  refreshInterval: number; // ms
  isLoading: boolean;
  error: string | null;

  // Actions
  setSeries: (series: MetricSeries[]) => void;
  appendSeries: (series: MetricSeries[]) => void;
  setAggregated: (metrics: AggregatedMetric[]) => void;
  selectMetric: (name: string | null) => void;
  setTimeRange: (range: TimeRange) => void;
  setRefreshInterval: (interval: number) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  clear: () => void;

  // Computed
  selectedSeries: () => MetricSeries[];
}

const DEFAULT_TIME_RANGE: TimeRange = {
  start: new Date(Date.now() - 3600000).toISOString(),
  end: new Date().toISOString(),
};

export const useMetricsStore = create<MetricsState>()((set, get) => ({
  series: [],
  aggregated: [],
  selectedMetric: null,
  timeRange: DEFAULT_TIME_RANGE,
  refreshInterval: 10000,
  isLoading: false,
  error: null,

  setSeries: (series) => set({ series, error: null }),

  appendSeries: (newSeries) =>
    set((state) => {
      // Keep last 1000 points per series
      const merged = new Map(state.series.map((s) => [s.name, s]));
      for (const s of newSeries) {
        const existing = merged.get(s.name);
        if (existing) {
          merged.set(s.name, {
            ...s,
            points: [...existing.points, ...s.points].slice(-1000),
          });
        } else {
          merged.set(s.name, s);
        }
      }
      return { series: Array.from(merged.values()) };
    }),

  setAggregated: (aggregated) => set({ aggregated }),

  selectMetric: (name) => set({ selectedMetric: name }),

  setTimeRange: (range) => set({ timeRange: range }),

  setRefreshInterval: (interval) => set({ refreshInterval: interval }),

  setLoading: (loading) => set({ isLoading: loading }),

  setError: (error) => set({ error }),

  clear: () => set({ series: [], aggregated: [], error: null }),

  selectedSeries: () => {
    const { series, selectedMetric } = get();
    if (!selectedMetric) return series;
    return series.filter((s) => s.name === selectedMetric);
  },
}));
