/**
 * Twin store — manages selected Digital Paryty detail state.
 *
 * Provides fetch, assign, and backlog accept/reject actions for the
 * TwinDetailPage. Uses the shared RestClient for API calls.
 *
 * @module stores/twinStore
 */

import { create } from 'zustand';
import { getRestClient } from '../api/rest';
import type { TwinDetails, TwinAgentInfo } from '../types/digitalParyty';

// ─── Store Shape ────────────────────────────────────────────────────────────

interface TwinState {
  /** Currently selected twin detail. */
  selectedTwin: TwinDetails | null;
  /** Agents assigned to the selected twin. */
  assignedAgents: TwinAgentInfo[];
  /** Unassigned agents available for assignment. */
  unassignedAgents: TwinAgentInfo[];
  /** Whether a fetch is in progress. */
  isLoading: boolean;
  /** Last fetch error message, if any. */
  error: string | null;

  // ─── Actions ──────────────────────────────────────────────────────────────

  /** Fetch twin metadata by ID. */
  fetchTwin: (twinId: string) => Promise<void>;
  /** Fetch agents (assigned + unassigned) for a twin. */
  fetchAgents: (twinId: string) => Promise<void>;
  /** Assign an unassigned agent to this twin. */
  assignAgent: (twinId: string, agentId: string) => Promise<boolean>;
  /** Accept a backlog for an offline agent. */
  acceptBacklog: (twinId: string, agentId: string) => Promise<boolean>;
  /** Reject a backlog for an offline agent. */
  rejectBacklog: (twinId: string, agentId: string) => Promise<boolean>;
  /** Clear selected twin state (e.g. on unmount). */
  clearSelection: () => void;
}

// ─── Store ─────────────────────────────────────────────────────────────────

export const useTwinStore = create<TwinState>()((set, get) => ({
  selectedTwin: null,
  assignedAgents: [],
  unassignedAgents: [],
  isLoading: false,
  error: null,

  fetchTwin: async (twinId: string) => {
    set({ isLoading: true, error: null });
    try {
      const client = getRestClient();
      const twin = await client.get<TwinDetails>(`/api/v1/twins/${encodeURIComponent(twinId)}`);
      set({ selectedTwin: twin, isLoading: false });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to fetch twin';
      set({ error: message, isLoading: false });
    }
  },

  fetchAgents: async (twinId: string) => {
    set({ isLoading: true, error: null });
    try {
      const client = getRestClient();

      // Fetch assigned agents for this twin
      const assigned = await client.get<TwinAgentInfo[]>(
        `/api/v1/twins/${encodeURIComponent(twinId)}/agents`,
      );

      // Fetch unassigned agents available for assignment
      const unassigned = await client.get<TwinAgentInfo[]>(
        '/api/v1/agents?unassigned=true',
      );

      set({ assignedAgents: assigned, unassignedAgents: unassigned, isLoading: false });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to fetch agents';
      set({ error: message, isLoading: false });
    }
  },

  assignAgent: async (twinId: string, agentId: string) => {
    try {
      const client = getRestClient();
      await client.post(`/api/v1/twins/${encodeURIComponent(twinId)}/agents`, {
        agent_id: agentId,
      });

      // Refresh agent lists after assignment
      const { fetchAgents: refetch } = get();
      await refetch(twinId);
      return true;
    } catch {
      return false;
    }
  },

  acceptBacklog: async (twinId: string, agentId: string) => {
    try {
      const client = getRestClient();
      await client.post(
        `/api/v1/twins/${encodeURIComponent(twinId)}/backlogs/${encodeURIComponent(agentId)}/accept`,
      );

      // Refresh agent lists after accepting backlog
      const { fetchAgents: refetch } = get();
      await refetch(twinId);
      return true;
    } catch {
      return false;
    }
  },

  rejectBacklog: async (twinId: string, agentId: string) => {
    try {
      const client = getRestClient();
      await client.post(
        `/api/v1/twins/${encodeURIComponent(twinId)}/backlogs/${encodeURIComponent(agentId)}/reject`,
      );

      // Refresh agent lists after rejecting backlog
      const { fetchAgents: refetch } = get();
      await refetch(twinId);
      return true;
    } catch {
      return false;
    }
  },

  clearSelection: () => {
    set({
      selectedTwin: null,
      assignedAgents: [],
      unassignedAgents: [],
      isLoading: false,
      error: null,
    });
  },
}));
