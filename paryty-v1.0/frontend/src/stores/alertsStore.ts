import { create } from 'zustand';
import type { Alert, AlertRule, AlertState } from '../types/alert';

// ─── Grouping ──────────────────────────────────────────────────

export type AlertGroupBy = 'severity' | 'rule' | 'node';

// ─── Trend ─────────────────────────────────────────────────────

export type AlertTrend = 'increasing' | 'decreasing' | 'stable';

// ─── Store Interface ───────────────────────────────────────────

interface AlertsState {
  // Data
  alerts: Alert[];
  rules: AlertRule[];
  selectedAlert: Alert | null;
  filterState: AlertState | null;
  filterSeverity: string | null;
  isLoading: boolean;
  error: string | null;

  // Grouping
  groupBy: AlertGroupBy;
  setGroupBy: (field: AlertGroupBy) => void;

  // Silence tracking (local state)
  silencedAlerts: Map<string, number>; // alertId → silencedUntil timestamp

  // History
  alertHistory: Alert[];

  // Actions
  setAlerts: (alerts: Alert[]) => void;
  updateAlert: (alert: Alert) => void;
  removeAlert: (alertId: string) => void;
  setRules: (rules: AlertRule[]) => void;
  selectAlert: (alert: Alert | null) => void;
  setFilterState: (state: AlertState | null) => void;
  setFilterSeverity: (severity: string | null) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  acknowledgeAlert: (alertId: string) => void;
  silenceAlert: (alertId: string, durationMs: number) => void;

  // Computed
  filteredAlerts: () => Alert[];
  firingCount: () => number;
  groupedAlerts: () => Map<string, Alert[]>;
  activeAlertCount: () => number;
  alertTrend: () => AlertTrend;
}

export const useAlertsStore = create<AlertsState>()((set, get) => ({
  alerts: [],
  rules: [],
  selectedAlert: null,
  filterState: null,
  filterSeverity: null,
  isLoading: false,
  error: null,

  // Grouping defaults
  groupBy: 'severity',
  setGroupBy: (groupBy) => set({ groupBy }),

  // Silence tracking defaults
  silencedAlerts: new Map<string, number>(),

  // History defaults
  alertHistory: [],

  // Existing actions (preserved)
  setAlerts: (alerts) => set({ alerts, error: null }),

  updateAlert: (alert) =>
    set((state) => {
      const idx = state.alerts.findIndex((a) => a.id === alert.id);
      if (idx === -1) return { alerts: [...state.alerts, alert] };
      const updated = [...state.alerts];
      updated[idx] = alert;
      return { alerts: updated };
    }),

  removeAlert: (alertId) =>
    set((state) => ({
      alerts: state.alerts.filter((a) => a.id !== alertId),
    })),

  setRules: (rules) => set({ rules }),

  selectAlert: (alert) => set({ selectedAlert: alert }),

  setFilterState: (filterState) => set({ filterState }),

  setFilterSeverity: (filterSeverity) => set({ filterSeverity }),

  setLoading: (loading) => set({ isLoading: loading }),

  setError: (error) => set({ error }),

  /**
   * Acknowledge an alert. Updates alert state locally
   * and adds to history. In production, this also calls
   * the REST API: POST /api/v1/alerts/:id/acknowledge
   */
  acknowledgeAlert: (alertId) =>
    set((state) => {
      const alert = state.alerts.find((a) => a.id === alertId);
      if (!alert) return state;

      const acknowledged: Alert = {
        ...alert,
        state: 'resolved' as AlertState,
        resolvedAt: new Date().toISOString(),
      };

      return {
        alerts: state.alerts.filter((a) => a.id !== alertId),
        alertHistory: [acknowledged, ...state.alertHistory].slice(0, 100),
        selectedAlert:
          state.selectedAlert?.id === alertId ? null : state.selectedAlert,
      };
    }),

  /**
   * Silence an alert for a specified duration.
   * Stored locally — alert is hidden from active views until duration expires.
   * @param alertId - Alert ID to silence
   * @param durationMs - Silence duration in milliseconds
   */
  silenceAlert: (alertId, durationMs) =>
    set((state) => {
      const silencedUntil = Date.now() + durationMs;
      const next = new Map(state.silencedAlerts);
      next.set(alertId, silencedUntil);
      return { silencedAlerts: next };
    }),

  // Existing computed (preserved)
  filteredAlerts: () => {
    const { alerts, filterState, filterSeverity, silencedAlerts } = get();
    const now = Date.now();
    let result = alerts;

    // Filter out actively silenced alerts (show once silence expires)
    result = result.filter((a) => {
      const silencedUntil = silencedAlerts.get(a.id);
      if (silencedUntil === undefined) return true;
      if (now < silencedUntil) return false; // hide while silence is active
      return true; // show once silence expires
    });

    if (filterState) result = result.filter((a) => a.state === filterState);
    if (filterSeverity) result = result.filter((a) => a.severity === filterSeverity);
    return result;
  },

  firingCount: () => get().alerts.filter((a) => a.state === 'firing').length,

  /**
   * Returns alerts grouped by the selected field (severity, rule, or node).
   */
  groupedAlerts: () => {
    const { filteredAlerts, groupBy } = get();
    const alerts = filteredAlerts();
    const groups = new Map<string, Alert[]>();

    for (const alert of alerts) {
      let key: string;
      switch (groupBy) {
        case 'severity':
          key = alert.severity;
          break;
        case 'rule':
          key = alert.ruleName;
          break;
        case 'node': {
          const nodeId = alert.labels['node'] ?? alert.labels['host'] ?? 'unknown';
          key = nodeId;
          break;
        }
        default:
          key = 'all';
      }
      const group = groups.get(key) ?? [];
      group.push(alert);
      groups.set(key, group);
    }

    return groups;
  },

  /**
   * Returns the count of currently active (firing + pending) alerts,
   * excluding silenced ones.
   */
  activeAlertCount: () => {
    const { alerts, silencedAlerts } = get();
    const now = Date.now();
    return alerts.filter((a) => {
      if (a.state !== 'firing' && a.state !== 'pending') return false;
      const silencedUntil = silencedAlerts.get(a.id);
      if (silencedUntil !== undefined && now < silencedUntil) return false;
      return true;
    }).length;
  },

  /**
   * Calculates alert trend based on current firing count
   * vs recent history. Returns 'increasing', 'decreasing', or 'stable'.
   */
  alertTrend: () => {
    const { alerts, alertHistory } = get();
    const currentFiring = alerts.filter((a) => a.state === 'firing').length;
    // Look at last 10 history entries to gauge trend
    const recentHistory = alertHistory.slice(0, 10);
    if (recentHistory.length === 0) return 'stable';

    const recentFiring = recentHistory.filter(
      (a) => a.state === 'firing' || a.state === 'resolved',
    ).length;
    const avgRecent = recentFiring / Math.max(recentHistory.length, 1);
    const currentRatio = currentFiring / Math.max(alerts.length, 1);

    if (currentRatio > avgRecent + 0.1) return 'increasing';
    if (currentRatio < avgRecent - 0.1) return 'decreasing';
    return 'stable';
  },
}));
