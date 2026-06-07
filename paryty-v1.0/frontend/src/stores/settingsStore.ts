import { create } from 'zustand';
import { persist } from 'zustand/middleware';

// ─── Node Size ─────────────────────────────────────────────────

export type NodeSize = 'small' | 'medium' | 'large';

// ─── Edge Style ────────────────────────────────────────────────

export type EdgeStyle = 'straight' | 'curved';

// ─── Label Visibility ──────────────────────────────────────────

export type LabelVisibility = 'always' | 'hover' | 'never';

// ─── Store Interface ───────────────────────────────────────────

interface SettingsState {
  // Display
  theme: 'light' | 'dark';
  language: string;

  // Theme customization
  accentColor: string;
  fontScale: number;
  reducedMotion: boolean;

  // Layout preferences
  nodeSize: NodeSize;
  edgeStyle: EdgeStyle;
  labelVisibility: LabelVisibility;

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
  setAccentColor: (color: string) => void;
  setFontScale: (scale: number) => void;
  setReducedMotion: (enabled: boolean) => void;
  setNodeSize: (size: NodeSize) => void;
  setEdgeStyle: (style: EdgeStyle) => void;
  setLabelVisibility: (visibility: LabelVisibility) => void;
  setApiBaseUrl: (url: string) => void;
  setWsUrl: (url: string) => void;
  setTopologyRefreshMs: (ms: number) => void;
  setMetricsRefreshMs: (ms: number) => void;
  setAlertsRefreshMs: (ms: number) => void;
  resetDefaults: () => void;
}

/** Default values for all settings */
const DEFAULT_SETTINGS = {
  theme: 'dark' as const,
  language: 'en',
  accentColor: '#ffffff',
  fontScale: 1.0,
  reducedMotion: false,
  nodeSize: 'medium' as NodeSize,
  edgeStyle: 'curved' as EdgeStyle,
  labelVisibility: 'hover' as LabelVisibility,
  apiBaseUrl: '',
  wsUrl: '/ws',
  topologyRefreshMs: 5000,
  metricsRefreshMs: 10000,
  alertsRefreshMs: 15000,
};

export const useSettingsStore = create<SettingsState>()(
  persist(
    (set) => ({
      ...DEFAULT_SETTINGS,

      setTheme: (theme) => set({ theme }),
      setLanguage: (lang) => set({ language: lang }),
      setAccentColor: (accentColor) => set({ accentColor }),
      setFontScale: (fontScale) => set({ fontScale: Math.max(0.5, Math.min(2.0, fontScale)) }),
      setReducedMotion: (reducedMotion) => set({ reducedMotion }),
      setNodeSize: (nodeSize) => set({ nodeSize }),
      setEdgeStyle: (edgeStyle) => set({ edgeStyle }),
      setLabelVisibility: (labelVisibility) => set({ labelVisibility }),
      setApiBaseUrl: (url) => set({ apiBaseUrl: url }),
      setWsUrl: (url) => set({ wsUrl: url }),
      setTopologyRefreshMs: (ms) => set({ topologyRefreshMs: ms }),
      setMetricsRefreshMs: (ms) => set({ metricsRefreshMs: ms }),
      setAlertsRefreshMs: (ms) => set({ alertsRefreshMs: ms }),

      /**
       * Reset all settings to their default values.
       * Clears persisted state in localStorage as well.
       */
      resetDefaults: () => set({ ...DEFAULT_SETTINGS }),
    }),
    {
      name: 'paryty-settings',
      partialize: (state) => ({
        // Only persist user preferences, not connection settings
        theme: state.theme,
        language: state.language,
        accentColor: state.accentColor,
        fontScale: state.fontScale,
        reducedMotion: state.reducedMotion,
        nodeSize: state.nodeSize,
        edgeStyle: state.edgeStyle,
        labelVisibility: state.labelVisibility,
        topologyRefreshMs: state.topologyRefreshMs,
        metricsRefreshMs: state.metricsRefreshMs,
        alertsRefreshMs: state.alertsRefreshMs,
      }),
    },
  ),
);
