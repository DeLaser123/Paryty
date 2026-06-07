/**
 * useIntel — React hook for Paryty Intelligence data.
 *
 * Fetches forecasts, anomalies, and model accuracy on mount
 * and at a configurable refresh interval.
 *
 * @module hooks/useIntel
 */

import { useEffect, useRef, useCallback } from 'react';
import { useIntelStore } from '../stores/intelStore';
import { KEY_METRICS } from '../types/intel';

/**
 * Hook that manages the intelligence data lifecycle.
 *
 * Fetches all key metric forecasts, anomaly detection status,
 * and model accuracy on mount. Re-fetches at the store's
 * configured refresh interval.
 *
 * Uses individual Zustand selectors to avoid infinite re-render loops
 * caused by the store object reference changing on every render.
 *
 * @returns The full intel store API
 */
export function useIntel() {
  // Use individual selectors to get stable references
  const fetchForecasts = useIntelStore((s) => s.fetchForecasts);
  const fetchDetectionStatus = useIntelStore((s) => s.fetchDetectionStatus);
  const fetchModelAccuracy = useIntelStore((s) => s.fetchModelAccuracy);
  const refreshInterval = useIntelStore((s) => s.refreshInterval);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchAll = useCallback(async () => {
    const metricNames = KEY_METRICS as unknown as string[];
    await Promise.all([
      fetchForecasts(metricNames),
      fetchDetectionStatus(),
      fetchModelAccuracy(),
    ]);
  }, [fetchForecasts, fetchDetectionStatus, fetchModelAccuracy]);

  // Initial fetch + interval refresh
  useEffect(() => {
    fetchAll();
    intervalRef.current = setInterval(fetchAll, refreshInterval);
    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [fetchAll, refreshInterval]);

  // Return the full store for component access to state
  // This is fine here since the consumer reads state, not actions
  return useIntelStore();
}
