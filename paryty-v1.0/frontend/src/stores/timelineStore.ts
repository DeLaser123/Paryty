import { create } from 'zustand';
import type { TimelineSnapshot, TimelinePosition, TimelineSpeed, TimelineState, TimelineBookmark } from '../types/timeline';
import type { Timestamp } from '../types/common';

interface TimelineStore {
  // Data
  snapshots: TimelineSnapshot[];
  position: TimelinePosition;
  config: {
    startTime: Timestamp;
    endTime: Timestamp;
    speed: TimelineSpeed;
    stepMs: number;
  };
  isStreaming: boolean;
  error: string | null;

  // Snapshot management
  availableSnapshots: TimelineSnapshot[];
  selectedSnapshot: TimelineSnapshot | null;
  loadSnapshot: (id: string) => void;
  setAvailableSnapshots: (snapshots: TimelineSnapshot[]) => void;

  // Diff mode
  diffMode: boolean;
  diffFrom: string | null;
  diffTo: string | null;
  enableDiff: (fromId: string, toId: string) => void;
  disableDiff: () => void;

  // Bookmarks
  bookmarks: TimelineBookmark[];
  addBookmark: (label: string) => void;
  removeBookmark: (id: string) => void;

  // Actions
  setSnapshots: (snapshots: TimelineSnapshot[]) => void;
  appendSnapshot: (snapshot: TimelineSnapshot) => void;
  setPosition: (position: Partial<TimelinePosition>) => void;
  setConfig: (config: Partial<TimelineStore['config']>) => void;
  play: () => void;
  pause: () => void;
  stop: () => void;
  setSpeed: (speed: TimelineSpeed) => void;
  seekTo: (progress: number) => void;
  setStreaming: (streaming: boolean) => void;
  setError: (error: string | null) => void;
  clear: () => void;

  // Export
  exportSnapshot: (format: 'json' | 'pdf' | 'html') => void;
}

const DEFAULT_CONFIG = {
  startTime: new Date(Date.now() - 3600000).toISOString(),
  endTime: new Date().toISOString(),
  speed: 1 as TimelineSpeed,
  stepMs: 1000,
};

export const useTimelineStore = create<TimelineStore>()((set, get) => ({
  snapshots: [],
  position: {
    currentTime: DEFAULT_CONFIG.startTime,
    progress: 0,
    state: 'stopped' as TimelineState,
    speed: 1 as TimelineSpeed,
  },
  config: DEFAULT_CONFIG,
  isStreaming: false,
  error: null,

  // Snapshot management defaults
  availableSnapshots: [],
  selectedSnapshot: null,
  loadSnapshot: (id) => {
    const { availableSnapshots } = get();
    const snapshot = availableSnapshots.find((s) => s.id === id) ?? null;
    set({ selectedSnapshot: snapshot });
  },
  setAvailableSnapshots: (availableSnapshots) => set({ availableSnapshots }),

  // Diff mode defaults
  diffMode: false,
  diffFrom: null,
  diffTo: null,
  enableDiff: (fromId, toId) =>
    set({ diffMode: true, diffFrom: fromId, diffTo: toId }),
  disableDiff: () =>
    set({ diffMode: false, diffFrom: null, diffTo: null }),

  // Bookmarks defaults
  bookmarks: [],

  /**
   * Add a bookmark at the current playback position.
   * @param label - User-provided label for the bookmark
   */
  addBookmark: (label) =>
    set((state) => {
      const bookmark: TimelineBookmark = {
        id: `bm_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
        timestamp: state.position.currentTime,
        label,
      };
      return { bookmarks: [...state.bookmarks, bookmark] };
    }),

  /**
   * Remove a bookmark by ID.
   */
  removeBookmark: (id) =>
    set((state) => ({
      bookmarks: state.bookmarks.filter((b) => b.id !== id),
    })),

  // Existing actions (preserved)
  setSnapshots: (snapshots) => set({ snapshots, error: null }),

  appendSnapshot: (snapshot) =>
    set((state) => ({
      snapshots: [...state.snapshots, snapshot].slice(-5000), // Keep last 5000
    })),

  setPosition: (pos) =>
    set((state) => ({ position: { ...state.position, ...pos } })),

  setConfig: (cfg) =>
    set((state) => ({ config: { ...state.config, ...cfg } })),

  play: () =>
    set((state) => ({
      position: { ...state.position, state: 'playing' as TimelineState },
    })),

  pause: () =>
    set((state) => ({
      position: { ...state.position, state: 'paused' as TimelineState },
    })),

  stop: () =>
    set((state) => ({
      position: {
        ...state.position,
        state: 'stopped' as TimelineState,
        progress: 0,
        currentTime: state.config.startTime,
      },
    })),

  setSpeed: (speed) =>
    set((state) => ({
      config: { ...state.config, speed },
      position: { ...state.position, speed },
    })),

  seekTo: (progress) => {
    const { config } = get();
    const start = new Date(config.startTime).getTime();
    const end = new Date(config.endTime).getTime();
    const currentTime = new Date(start + (end - start) * progress).toISOString();
    set((state) => ({
      position: { ...state.position, progress, currentTime },
    }));
  },

  setStreaming: (streaming) => set({ isStreaming: streaming }),
  setError: (error) => set({ error }),
  clear: () =>
    set({
      snapshots: [],
      position: {
        currentTime: DEFAULT_CONFIG.startTime,
        progress: 0,
        state: 'stopped' as TimelineState,
        speed: 1 as TimelineSpeed,
      },
      error: null,
      availableSnapshots: [],
      selectedSnapshot: null,
      diffMode: false,
      diffFrom: null,
      diffTo: null,
      bookmarks: [],
    }),

  /**
   * Export the selected snapshot in the specified format.
   * Creates a download via Blob URL.
   * @param format - Export format: 'json', 'pdf', or 'html'
   */
  exportSnapshot: (format) => {
    const { selectedSnapshot } = get();
    if (!selectedSnapshot) return;

    const filename = `paryty-snapshot-${selectedSnapshot.id}`;
    let content: string;
    let mimeType: string;

    switch (format) {
      case 'json': {
        content = JSON.stringify(selectedSnapshot, null, 2);
        mimeType = 'application/json';
        break;
      }
      case 'html': {
        content = buildSnapshotHtml(selectedSnapshot);
        mimeType = 'text/html';
        break;
      }
      case 'pdf': {
        // PDF generation would require a library like jsPDF.
        // For now, export as HTML which can be printed to PDF.
        content = buildSnapshotHtml(selectedSnapshot);
        mimeType = 'text/html';
        break;
      }
      default:
        return;
    }

    const blob = new Blob([content], { type: mimeType });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${filename}.${format}`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  },
}));

// ─── Helpers ───────────────────────────────────────────────────

/**
 * Build a simple HTML report from a snapshot for PDF/HTML export.
 */
function buildSnapshotHtml(snapshot: TimelineSnapshot): string {
  const metricRows = Object.entries(snapshot.metrics)
    .map(([key, value]) => `<tr><td>${escapeHtml(key)}</td><td>${value}</td></tr>`)
    .join('\n');

  return `<!DOCTYPE html>
<html>
<head>
  <title>Paryty Snapshot — ${escapeHtml(snapshot.id)}</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 800px; margin: 2rem auto; color: #e0e0e0; background: #111; }
    h1 { font-size: 1.5rem; }
    table { width: 100%; border-collapse: collapse; margin: 1rem 0; }
    th, td { padding: 0.5rem 1rem; text-align: left; border-bottom: 1px solid #333; }
    .meta { color: #888; font-size: 0.875rem; }
  </style>
</head>
<body>
  <h1>Paryty Snapshot Export</h1>
  <p class="meta">ID: ${escapeHtml(snapshot.id)} | Timestamp: ${escapeHtml(snapshot.timestamp)}</p>
  <p>Nodes: ${snapshot.nodes.length} | Edges: ${snapshot.edges.length} | Alerts: ${snapshot.alertCount} | Events: ${snapshot.eventCount}</p>
  <h2>Metrics</h2>
  <table>
    <thead><tr><th>Metric</th><th>Value</th></tr></thead>
    <tbody>${metricRows}</tbody>
  </table>
</body>
</html>`;
}

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}
