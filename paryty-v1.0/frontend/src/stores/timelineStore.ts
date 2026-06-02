import { create } from 'zustand';
import type { TimelineSnapshot, TimelinePosition, TimelineSpeed, TimelineState } from '../types/timeline';
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
    }),
}));
