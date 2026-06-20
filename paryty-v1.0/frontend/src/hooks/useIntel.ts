/**
 * useIntel — React hook for Paryty Intelligence data.
 *
 * Fetches forecasts, anomalies, and model accuracy on mount
 * and at a configurable refresh interval.  Subscribes to
 * real-time WebSocket channels for live anomaly, forecast,
 * and drift updates.
 *
 * @module hooks/useIntel
 */

import { useEffect, useRef, useCallback } from 'react';
import { useIntelStore } from '../stores/intelStore';
import { getWsClient } from '../api/websocket';
import type { WsAnomalyEvent, WsForecastEvent, WsDriftEvent } from '../types/intel';

/**
 * Hook that manages the intelligence data lifecycle.
 *
 * Strategy: REST for initial load + periodic refresh,
 * WebSocket for real-time push updates between refreshes.
 *
 * Uses individual Zustand selectors to avoid infinite re-render loops
 * caused by the store object reference changing on every render.
 */
export function useIntel() {
  // Stable action references via selectors
  const fetchForecasts = useIntelStore((s) => s.fetchForecasts);
  const fetchDetectionStatus = useIntelStore((s) => s.fetchDetectionStatus);
  const fetchModelAccuracy = useIntelStore((s) => s.fetchModelAccuracy);
  const handleWsAnomaly = useIntelStore((s) => s.handleWsAnomaly);
  const handleWsForecast = useIntelStore((s) => s.handleWsForecast);
  const handleWsDrift = useIntelStore((s) => s.handleWsDrift);
  const setWsConnected = useIntelStore((s) => s.setWsConnected);
  const knownMetrics = useIntelStore((s) => s.knownMetrics);
  const refreshInterval = useIntelStore((s) => s.refreshInterval);

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // ── REST: initial load + periodic refresh ───────────────────────
  const fetchAll = useCallback(async () => {
    await Promise.all([
      fetchForecasts(knownMetrics.length > 0 ? knownMetrics : []),
      fetchDetectionStatus(),
      fetchModelAccuracy(),
    ]);
  }, [fetchForecasts, fetchDetectionStatus, fetchModelAccuracy, knownMetrics]);

  useEffect(() => {
    fetchAll();
    intervalRef.current = setInterval(fetchAll, refreshInterval);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchAll, refreshInterval]);

  // ── WebSocket: real-time subscription ───────────────────────────
  useEffect(() => {
    const ws = getWsClient();

    const unsubAnomaly = ws.subscribe('intel.anomalies', (data) => {
      handleWsAnomaly(data as WsAnomalyEvent);
    });
    const unsubForecast = ws.subscribe('intel.forecasts', (data) => {
      handleWsForecast(data as WsForecastEvent);
    });
    const unsubDrift = ws.subscribe('intel.drift', (data) => {
      handleWsDrift(data as WsDriftEvent);
    });

    ws.connect();
    setWsConnected(true);

    return () => {
      ws.unsubscribe(unsubAnomaly);
      ws.unsubscribe(unsubForecast);
      ws.unsubscribe(unsubDrift);
      setWsConnected(false);
    };
  }, [handleWsAnomaly, handleWsForecast, handleWsDrift, setWsConnected]);

  // Return the full store for component access to state
  return useIntelStore();
}
