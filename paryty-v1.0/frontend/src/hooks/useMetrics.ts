import { useEffect, useRef, useCallback } from 'react';
import { useMetricsStore } from '../stores/metricsStore';
import { getRestClient } from '../api/rest';
import { useSettingsStore } from '../stores/settingsStore';
import type { MetricQuery } from '../types/metric';

export function useMetrics(metricName?: string) {
  const store = useMetricsStore();
  const refreshMs = useSettingsStore((s) => s.metricsRefreshMs);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchMetrics = useCallback(async () => {
    if (!metricName) return;
    store.setLoading(true);
    try {
      const client = getRestClient();
      const query: MetricQuery = {
        name: metricName,
        startTime: store.timeRange.start,
        endTime: store.timeRange.end,
        step: '1m',
      };
      const series = await client.queryMetrics(query);
      store.appendSeries(series);
    } catch (err) {
      store.setError(err instanceof Error ? err.message : String(err));
    } finally {
      store.setLoading(false);
    }
  }, [store, metricName]);

  useEffect(() => {
    if (metricName) {
      fetchMetrics();
      intervalRef.current = setInterval(fetchMetrics, refreshMs);
    }
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchMetrics, refreshMs, metricName]);

  return store;
}
