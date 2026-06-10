/**
 * Tests for planStore — plan metadata and feature flag checks.
 *
 * Verifies fetchPlans, fetchCurrentPlan, hasFeature, usageFor,
 * and isNearLimit with mocked API responses.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { usePlanStore } from '../stores/planStore';
import type { Plan, CurrentPlan } from '../types/plan';

// ─── Mock Factories ───────────────────────────────────────────────────

const mockPlans: Plan[] = [
  {
    name: 'basic',
    displayName: 'Basic',
    maxTwins: 3,
    creatable: false,
    features: { topology: true, forecasting: false, timeline: true },
    limits: {
      agentsPerTwin: 5,
      dataRetentionDays: 7,
      subUsers: 0,
      alertRulesPerTenant: 5,
      metricsResolution: '1m',
    },
    quotas: {
      ingestionBytesPerDay: 10_000_000,
      queryRequestsPerMinute: 10,
    },
  },
  {
    name: 'pro',
    displayName: 'Pro',
    maxTwins: 50,
    creatable: true,
    features: { topology: true, forecasting: true, timeline: true, anomalyDetection: true },
    limits: {
      agentsPerTwin: 20,
      dataRetentionDays: 30,
      subUsers: 5,
      alertRulesPerTenant: 50,
      metricsResolution: '10s',
    },
    quotas: {
      ingestionBytesPerDay: 500_000_000,
      queryRequestsPerMinute: 100,
    },
  },
];

const mockCurrentPlan: CurrentPlan = {
  planName: 'pro',
  features: { topology: true, forecasting: true, timeline: true, anomalyDetection: true },
  limits: {
    maxTwins: 50,
    agentsPerTwin: 20,
    subUsers: 5,
    alertRulesPerTenant: 50,
  },
  quotas: {
    ingestionBytesPerDay: 500_000_000,
    queryRequestsPerMinute: 100,
  },
  startedAt: '2025-01-01T00:00:00Z',
};

// ─── Mock RestClient ──────────────────────────────────────────────────

vi.mock('../api/rest', () => {
  const mockGet = vi.fn();
  const mockClient = {
    get: mockGet,
    post: vi.fn(),
  };
  return {
    getRestClient: vi.fn(() => mockClient),
    RestClient: vi.fn(() => mockClient),
    ApiClientError: class extends Error {
      status: number;
      code?: string;
      constructor(message: string, status: number, code?: string) {
        super(message);
        this.status = status;
        this.code = code;
        this.name = 'ApiClientError';
      }
    },
  };
});

// ─── Helpers ──────────────────────────────────────────────────────────

function resetStore() {
  usePlanStore.setState({
    plans: [],
    currentPlan: null,
    isLoading: false,
  });
}

// ─── Tests ────────────────────────────────────────────────────────────

describe('planStore', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetStore();
  });

  // ── fetchPlans ─────────────────────────────────────────────────

  describe('fetchPlans', () => {
    it('populates plans array on successful fetch', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockGet = getRestClient().get as ReturnType<typeof vi.fn>;
      mockGet.mockResolvedValueOnce(mockPlans);

      await usePlanStore.getState().fetchPlans();

      const state = usePlanStore.getState();
      expect(state.plans).toEqual(mockPlans);
      expect(state.plans).toHaveLength(2);
      expect(state.isLoading).toBe(false);
    });

    it('sets isLoading=true during fetch', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockGet = getRestClient().get as ReturnType<typeof vi.fn>;
      // Defer resolution to check loading state
      let resolve: (v: Plan[]) => void;
      const promise = new Promise<Plan[]>((r) => { resolve = r; });
      mockGet.mockReturnValueOnce(promise);

      const fetchPromise = usePlanStore.getState().fetchPlans();
      expect(usePlanStore.getState().isLoading).toBe(true);

      resolve!(mockPlans);
      await fetchPromise;

      expect(usePlanStore.getState().isLoading).toBe(false);
    });

    it('sets isLoading=false and error on fetch failure', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockGet = getRestClient().get as ReturnType<typeof vi.fn>;
      mockGet.mockRejectedValueOnce(new Error('Network error'));

      await usePlanStore.getState().fetchPlans();

      expect(usePlanStore.getState().isLoading).toBe(false);
      expect(usePlanStore.getState().plans).toEqual([]);
      expect(usePlanStore.getState().error).toBe('Network error');
    });

    it('sets a user-friendly error when backend is unreachable', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockGet = getRestClient().get as ReturnType<typeof vi.fn>;
      mockGet.mockRejectedValueOnce(new TypeError('Failed to fetch'));

      await usePlanStore.getState().fetchPlans();

      expect(usePlanStore.getState().error).toContain('not reachable');
    });

    it('calls GET /api/v1/plans', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockGet = getRestClient().get as ReturnType<typeof vi.fn>;
      mockGet.mockResolvedValueOnce(mockPlans);

      await usePlanStore.getState().fetchPlans();

      expect(mockGet).toHaveBeenCalledWith('/api/v1/plans', { skipAuth: true });
    });
  });

  // ── fetchCurrentPlan ───────────────────────────────────────────

  describe('fetchCurrentPlan', () => {
    it('populates currentPlan on successful fetch', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockGet = getRestClient().get as ReturnType<typeof vi.fn>;
      mockGet.mockResolvedValueOnce({ plan: mockCurrentPlan });

      await usePlanStore.getState().fetchCurrentPlan();

      const state = usePlanStore.getState();
      expect(state.currentPlan).toEqual(mockCurrentPlan);
      expect(state.currentPlan?.planName).toBe('pro');
      expect(state.isLoading).toBe(false);
    });

    it('sets isLoading=false and error on fetch failure', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockGet = getRestClient().get as ReturnType<typeof vi.fn>;
      mockGet.mockRejectedValueOnce(new Error('Network error'));

      await usePlanStore.getState().fetchCurrentPlan();

      expect(usePlanStore.getState().isLoading).toBe(false);
      expect(usePlanStore.getState().currentPlan).toBeNull();
      expect(usePlanStore.getState().error).toBe('Network error');
    });

    it('calls GET /api/v1/me', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockGet = getRestClient().get as ReturnType<typeof vi.fn>;
      mockGet.mockResolvedValueOnce({ plan: mockCurrentPlan });

      await usePlanStore.getState().fetchCurrentPlan();

      expect(mockGet).toHaveBeenCalledWith('/api/v1/me');
    });
  });

  // ── clearPlanError ────────────────────────────────────────────

  describe('clearPlanError', () => {
    it('clears the error state', () => {
      usePlanStore.setState({ error: 'Some error' });
      expect(usePlanStore.getState().error).toBe('Some error');

      usePlanStore.getState().clearPlanError();

      expect(usePlanStore.getState().error).toBeNull();
    });
  });

  // ── hasFeature ─────────────────────────────────────────────────

  describe('hasFeature', () => {
    it('returns false when currentPlan is null', () => {
      expect(usePlanStore.getState().hasFeature('forecasting')).toBe(false);
    });

    it('returns true for an enabled feature', () => {
      usePlanStore.setState({ currentPlan: mockCurrentPlan });
      expect(usePlanStore.getState().hasFeature('forecasting')).toBe(true);
    });

    it('returns false for a disabled feature', () => {
      usePlanStore.setState({ currentPlan: mockCurrentPlan });
      expect(usePlanStore.getState().hasFeature('nonexistent')).toBe(false);
    });

    it('returns true when feature value is explicitly true', () => {
      const planWithFlag: CurrentPlan = {
        ...mockCurrentPlan,
        features: { customFeature: true },
      };
      usePlanStore.setState({ currentPlan: planWithFlag });
      expect(usePlanStore.getState().hasFeature('customFeature')).toBe(true);
    });
  });

  // ── usageFor ───────────────────────────────────────────────────

  describe('usageFor', () => {
    beforeEach(() => {
      usePlanStore.setState({ currentPlan: mockCurrentPlan });
    });

    it('returns null when currentPlan is null', () => {
      usePlanStore.setState({ currentPlan: null });
      expect(usePlanStore.getState().usageFor('twins', 10)).toBeNull();
    });

    it('calculates usage percentage for twins', () => {
      const usage = usePlanStore.getState().usageFor('twins', 25);
      expect(usage).not.toBeNull();
      expect(usage!.resource).toBe('twins');
      expect(usage!.current).toBe(25);
      expect(usage!.max).toBe(50);
      expect(usage!.percentage).toBe(50); // 25/50 = 50%
    });

    it('calculates usage percentage for agents', () => {
      const usage = usePlanStore.getState().usageFor('agents', 10);
      expect(usage).not.toBeNull();
      expect(usage!.current).toBe(10);
      expect(usage!.max).toBe(20);
      expect(usage!.percentage).toBe(50);
    });

    it('calculates usage percentage for subUsers', () => {
      const usage = usePlanStore.getState().usageFor('subUsers', 3);
      expect(usage).not.toBeNull();
      expect(usage!.max).toBe(5);
      expect(usage!.percentage).toBe(60); // 3/5 = 60%
    });

    it('calculates usage percentage for alertRules', () => {
      const usage = usePlanStore.getState().usageFor('alertRules', 49);
      expect(usage).not.toBeNull();
      expect(usage!.max).toBe(50);
      expect(usage!.percentage).toBe(98);
    });

    it('caps percentage at 100 when over limit', () => {
      const usage = usePlanStore.getState().usageFor('twins', 100);
      expect(usage).not.toBeNull();
      expect(usage!.percentage).toBe(100);
    });

    it('returns null when max limit is 0 or negative', () => {
      const zeroLimitsPlan: CurrentPlan = {
        ...mockCurrentPlan,
        limits: { ...mockCurrentPlan.limits, maxTwins: 0 },
      };
      usePlanStore.setState({ currentPlan: zeroLimitsPlan });
      expect(usePlanStore.getState().usageFor('twins', 10)).toBeNull();
    });
  });

  // ── isNearLimit ────────────────────────────────────────────────

  describe('isNearLimit', () => {
    beforeEach(() => {
      usePlanStore.setState({ currentPlan: mockCurrentPlan });
    });

    it('returns false when currentPlan is null', () => {
      usePlanStore.setState({ currentPlan: null });
      expect(usePlanStore.getState().isNearLimit('twins', 40)).toBe(false);
    });

    it('returns false below 80% threshold', () => {
      // 39/50 = 78% — below 80%
      expect(usePlanStore.getState().isNearLimit('twins', 39)).toBe(false);
    });

    it('returns true at exactly 80%', () => {
      // 40/50 = 80%
      expect(usePlanStore.getState().isNearLimit('twins', 40)).toBe(true);
    });

    it('returns true above 80%', () => {
      // 41/50 = 82%
      expect(usePlanStore.getState().isNearLimit('twins', 41)).toBe(true);
    });

    it('returns true at 100%', () => {
      expect(usePlanStore.getState().isNearLimit('twins', 50)).toBe(true);
    });

    it('returns false when resource has no limit (max <= 0)', () => {
      const zeroLimitsPlan: CurrentPlan = {
        ...mockCurrentPlan,
        limits: { ...mockCurrentPlan.limits, maxTwins: 0 },
      };
      usePlanStore.setState({ currentPlan: zeroLimitsPlan });
      expect(usePlanStore.getState().isNearLimit('twins', 999)).toBe(false);
    });
  });
});
