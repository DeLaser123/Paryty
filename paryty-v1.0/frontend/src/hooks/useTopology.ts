import { useEffect, useRef } from 'react';
import { useTopologyStore } from '../stores/topologyStore';
import { getRestClient } from '../api/rest';
import { getWsClient } from '../api/websocket';
import { useSettingsStore } from '../stores/settingsStore';

/**
 * Hook that subscribes to the topology store and triggers data fetching.
 *
 * IMPORTANT: Uses Zustand selectors to avoid infinite re-render loops.
 * Do NOT store a reference to the full store object — every state update
 * would change the reference, recreating callbacks and retriggering effects.
 */
export function useTopology() {
  // Select only the data fields we need for rendering
  const topology = useTopologyStore((s) => s.topology);
  const isLoading = useTopologyStore((s) => s.isLoading);
  const error = useTopologyStore((s) => s.error);
  const version = useTopologyStore((s) => s.version);

  // Select actions (stable references — never change)
  const setTopology = useTopologyStore((s) => s.setTopology);
  const applyDiff = useTopologyStore((s) => s.applyDiff);
  const setLoading = useTopologyStore((s) => s.setLoading);
  const setError = useTopologyStore((s) => s.setError);

  const refreshMs = useSettingsStore((s) => s.topologyRefreshMs);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const mountedRef = useRef(true);

  // Initial fetch and polling — runs once on mount
  useEffect(() => {
    mountedRef.current = true;

    const fetchTopology = async () => {
      setLoading(true);
      try {
        const client = getRestClient();
        const data = await client.getTopology();
        if (mountedRef.current) setTopology(data);
      } catch (err) {
        if (mountedRef.current) setError(err instanceof Error ? err.message : String(err));
      } finally {
        if (mountedRef.current) setLoading(false);
      }
    };

    fetchTopology();
    intervalRef.current = setInterval(fetchTopology, refreshMs);

    return () => {
      mountedRef.current = false;
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [refreshMs]); // Only depend on refresh interval — actions are stable

  // WebSocket real-time updates — runs once on mount
  useEffect(() => {
    const ws = getWsClient();
    const subId = ws.subscribe('topology', (data) => {
      applyDiff(data as Parameters<typeof applyDiff>[0]);
    });
    ws.connect();
    return () => ws.unsubscribe(subId);
  }, []); // Empty deps — connect once

  return { topology, isLoading, error, version };
}
