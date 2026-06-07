/**
 * Particle Store — Zustand bridge between PixiJS particle engine and React UI.
 *
 * The PixiJS SemanticParticleSystem runs on the GPU render loop and pushes
 * per-frame counts here. React components (StatusBar) subscribe to these
 * selectors without coupling to the engine.
 *
 * Counts updated by:
 *   visualParticleCount  → PixiTopologyApp.animate() (viewport-culled)
 *   totalEventsReceived  → EventIngest on each successful poll
 *   particlesActive      → PixiTopologyApp (budget-level dependent)
 */

import { create } from 'zustand';
import type { TransportMode } from '../types/event';

// ─── State Interface ────────────────────────────────────────────

export interface ParticleState {
  /** Number of particles currently visible on-screen (viewport-culled, per-frame). */
  visualParticleCount: number;
  /** Cumulative total of distinct ParytyEvents received since app start. */
  totalEventsReceived: number;
  /** Whether the particle system is currently active (not paused by budget). */
  particlesActive: boolean;
  /** Current transport mode for the event ingestion pipeline. */
  transportMode: TransportMode;

  // Actions
  setVisualParticleCount: (count: number) => void;
  setTotalEventsReceived: (count: number) => void;
  incrementTotalEventsReceived: (delta: number) => void;
  setParticlesActive: (active: boolean) => void;
  setTransportMode: (mode: TransportMode) => void;
}

// ─── Store ──────────────────────────────────────────────────────

export const useParticleStore = create<ParticleState>()((set) => ({
  visualParticleCount: 0,
  totalEventsReceived: 0,
  particlesActive: true,
  transportMode: 'ws',

  setVisualParticleCount: (count) => set({ visualParticleCount: count }),

  setTotalEventsReceived: (count) => set({ totalEventsReceived: count }),

  incrementTotalEventsReceived: (delta) =>
    set((state) => ({ totalEventsReceived: state.totalEventsReceived + delta })),

  setParticlesActive: (active) => set({ particlesActive: active }),

  setTransportMode: (mode) => set({ transportMode: mode }),
}));
