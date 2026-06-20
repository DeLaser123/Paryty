import { useEffect, useCallback, useRef } from 'react';
import { useAlertsStore } from '../stores/alertsStore';
import { getRestClient } from '../api/rest';
import { getWsClient } from '../api/websocket';
import { useSettingsStore } from '../stores/settingsStore';

export function useAlerts() {
  const store = useAlertsStore();
  const refreshMs = useSettingsStore((s) => s.alertsRefreshMs);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchAlerts = useCallback(async () => {
    store.setLoading(true);
    try {
      const client = getRestClient();
      const [alerts, rules] = await Promise.all([
        client.getAlerts(),
        client.getAlertRules(),
      ]);
      store.setAlerts(alerts);
      store.setRules(rules);
    } catch (err) {
      store.setError(err instanceof Error ? err.message : String(err));
    } finally {
      store.setLoading(false);
    }
  }, [store]);

  useEffect(() => {
    fetchAlerts();
    intervalRef.current = setInterval(fetchAlerts, refreshMs);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchAlerts, refreshMs]);

  // Real-time alert updates
  useEffect(() => {
    const ws = getWsClient();
    const subId = ws.subscribe('alerts', (data) => {
      const alert = data as Parameters<typeof store.updateAlert>[0];
      store.updateAlert(alert);
    });
    ws.connect();
    return () => ws.unsubscribe(subId);
  }, [store]);

  return store;
}
