import { create } from 'zustand';
import type { Alert, AlertRule, AlertState } from '../types/alert';

interface AlertsState {
  // Data
  alerts: Alert[];
  rules: AlertRule[];
  selectedAlert: Alert | null;
  filterState: AlertState | null;
  filterSeverity: string | null;
  isLoading: boolean;
  error: string | null;

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

  // Computed
  filteredAlerts: () => Alert[];
  firingCount: () => number;
}

export const useAlertsStore = create<AlertsState>()((set, get) => ({
  alerts: [],
  rules: [],
  selectedAlert: null,
  filterState: null,
  filterSeverity: null,
  isLoading: false,
  error: null,

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

  filteredAlerts: () => {
    const { alerts, filterState, filterSeverity } = get();
    let result = alerts;
    if (filterState) result = result.filter((a) => a.state === filterState);
    if (filterSeverity) result = result.filter((a) => a.severity === filterSeverity);
    return result;
  },

  firingCount: () => get().alerts.filter((a) => a.state === 'firing').length,
}));
