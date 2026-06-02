import { create } from 'zustand';

interface SettingsState {
  // Display
  theme: 'light' | 'dark';
  language: string;

  // Connections
  apiBaseUrl: string;
  wsUrl: string;

  // Refresh rates
  topologyRefreshMs: number;
  metricsRefreshMs: number;
  alertsRefreshMs: number;

  // Actions
  setTheme: (theme: 'light' | 'dark') => void;
  setLanguage: (lang: string) => void;
  setApiBaseUrl: (url: string) => void;
  setWsUrl: (url: string) => void;
  setTopologyRefreshMs: (ms: number) => void;
  setMetricsRefreshMs: (ms: number) => void;
  setAlertsRefreshMs: (ms: number) => void;
}

export const useSettingsStore = create<SettingsState>()((set) => ({
  theme: 'dark',
  language: 'en',
  apiBaseUrl: '',
  wsUrl: '/ws',
  topologyRefreshMs: 5000,
  metricsRefreshMs: 10000,
  alertsRefreshMs: 15000,

  setTheme: (theme) => set({ theme }),
  setLanguage: (lang) => set({ language: lang }),
  setApiBaseUrl: (url) => set({ apiBaseUrl: url }),
  setWsUrl: (url) => set({ wsUrl: url }),
  setTopologyRefreshMs: (ms) => set({ topologyRefreshMs: ms }),
  setMetricsRefreshMs: (ms) => set({ metricsRefreshMs: ms }),
  setAlertsRefreshMs: (ms) => set({ alertsRefreshMs: ms }),
}));
