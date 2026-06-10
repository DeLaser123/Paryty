/**
 * Plan store — manages plan metadata and feature flag checks.
 *
 * Fetches available plans and the current tenant's plan from the API.
 * Provides helpers for feature gating and usage tracking.
 *
 * @module stores/planStore
 */

import { create } from 'zustand';
import { getRestClient } from '../api/rest';
import type { Plan, CurrentPlan, UsageInfo, ResourceType } from '../types/plan';

// ─── Store Shape ─────────────────────────────────────────────────────

interface PlanState {
  /** Available plans from /api/v1/plans. */
  plans: Plan[];
  /** Current tenant's plan from /api/v1/me. */
  currentPlan: CurrentPlan | null;
  /** Whether plan data is being fetched. */
  isLoading: boolean;
  /** Set when fetchPlans or fetchCurrentPlan fails. */
  error: string | null;

  // Actions
  fetchPlans: () => Promise<void>;
  fetchCurrentPlan: () => Promise<void>;
  /** Clear the error state. */
  clearPlanError: () => void;
  /** Check if the current plan has a specific feature. */
  hasFeature: (feature: string) => boolean;
  /** Get usage info for a resource type. */
  usageFor: (resource: ResourceType, current: number) => UsageInfo | null;
  /** Check if a resource is at >= 80% of its limit. */
  isNearLimit: (resource: ResourceType, current: number) => boolean;
}

// ─── Store ───────────────────────────────────────────────────────────

export const usePlanStore = create<PlanState>()((set, get) => ({
  plans: [],
  currentPlan: null,
  isLoading: false,
  error: null,

  fetchPlans: async () => {
    set({ isLoading: true, error: null });
    try {
      const client = getRestClient();
      const plans = await client.get<Plan[]>('/api/v1/plans', { skipAuth: true });
      set({ plans, isLoading: false, error: null });
    } catch (err) {
      const message =
        err instanceof TypeError && err.message === 'Failed to fetch'
          ? 'Backend service is not reachable. Please ensure the query service is running.'
          : err instanceof Error
            ? err.message
            : 'Failed to load plans.';
      set({ isLoading: false, error: message });
    }
  },

  fetchCurrentPlan: async () => {
    set({ isLoading: true, error: null });
    try {
      const client = getRestClient();
      const data = await client.get<{ plan: CurrentPlan }>('/api/v1/me');
      set({ currentPlan: data.plan, isLoading: false, error: null });
    } catch (err) {
      console.error('[planStore] fetchCurrentPlan failed:', err);
      const message =
        err instanceof TypeError && err.message === 'Failed to fetch'
          ? 'Backend service is not reachable.'
          : err instanceof Error
            ? err.message
            : 'Failed to load plan info.';
      set({ isLoading: false, error: message });
    }
  },

  clearPlanError: () => set({ error: null }),

  hasFeature: (feature: string): boolean => {
    const { currentPlan } = get();
    if (!currentPlan) return false;
    return currentPlan.features[feature] === true;
  },

  usageFor: (resource: ResourceType, current: number): UsageInfo | null => {
    const { currentPlan } = get();
    if (!currentPlan) return null;

    const limitMap: Record<ResourceType, keyof typeof currentPlan.limits> = {
      twins: 'maxTwins' as const,
      agents: 'agentsPerTwin' as const,
      subUsers: 'subUsers' as const,
      alertRules: 'alertRulesPerTenant' as const,
    };

    const limitKey = limitMap[resource];
    const max = currentPlan.limits[limitKey] ?? 0;

    if (max <= 0) return null;

    return {
      resource,
      current,
      max,
      percentage: Math.min(100, Math.round((current / max) * 100)),
    };
  },

  isNearLimit: (resource: ResourceType, current: number): boolean => {
    const usage = get().usageFor(resource, current);
    if (!usage) return false;
    // Near limit = 80% or more
    return usage.percentage >= 80;
  },
}));
