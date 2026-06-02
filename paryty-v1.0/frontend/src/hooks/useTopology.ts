import { useEffect, useRef, useCallback } from 'react';
import { useTopologyStore } from '../stores/topologyStore';
import { getRestClient } from '../api/rest';
import { getWsClient } from '../api/websocket';
import { useSettingsStore } from '../stores/settingsStore';

export function useTopology() {
  const store = useTopologyStore();
  const refreshMs = useSettingsStore((s) => s.topologyRefreshMs);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchTopology = useCallback(async () => {
    store.setLoading(true);
    try {
      const client = getRestClient();
      const topology = await client.getTopology();
      store.setTopology(topology);
    } catch (err) {
      store.setError((err as Error).message);
    } finally {
      store.setLoading(false);
    }
  }, [store]);

  // Initial fetch and polling
  useEffect(() => {
    fetchTopology();
    intervalRef.current = setInterval(fetchTopology, refreshMs);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchTopology, refreshMs]);

  // WebSocket real-time updates
  useEffect(() => {
    const ws = getWsClient();
    const subId = ws.subscribe('topology', (data) => {
      const diff = data as Parameters<typeof store.applyDiff>[0];
      store.applyDiff(diff);
    });
    ws.connect();
    return () => ws.unsubscribe(subId);
  }, [store]);

  return store;
}
