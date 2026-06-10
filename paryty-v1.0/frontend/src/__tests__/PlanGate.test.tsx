/**
 * @vitest-environment jsdom
 *
 * Tests for PlanGate — feature gate component.
 *
 * Verifies that children render when a feature is available,
 * and UpgradePrompt renders when a feature is unavailable.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { CurrentPlan } from '../types/plan';

// ─── Mock State (vi.hoisted to work with vi.mock hoisting) ───────────

const mockPlanState = vi.hoisted(() => ({
  currentPlan: null as CurrentPlan | null,
  features: {} as Record<string, boolean>,
}));

const defaultCurrentPlan = vi.hoisted((): CurrentPlan => ({
  planName: 'pro',
  features: {
    topology: true,
    forecasting: true,
    timeline: true,
    anomalyDetection: false,
  },
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
}));

// ─── Mocks ───────────────────────────────────────────────────────────
// NOTE: Mock paths are relative to this test file (src/__tests__/).
// PlanGate at src/components/auth/ imports from ../../stores/planStore,
// which resolves to src/stores/planStore, same as ../stores/planStore here.

vi.mock('../stores/planStore', () => ({
  usePlanStore: (selector: (state: Record<string, unknown>) => unknown) => {
    return selector({
      currentPlan: mockPlanState.currentPlan,
      hasFeature: (feature: string) => mockPlanState.features[feature] ?? false,
    });
  },
}));

vi.mock('../components/common/UpgradePrompt', () => ({
  UpgradePrompt: ({ feature, message }: { feature?: string; message?: string }) => (
    <div data-testid="upgrade-prompt">
      UpgradePrompt — feature: {feature ?? 'none'}, message: {message ?? 'default'}
    </div>
  ),
}));

// ─── Component Under Test ─────────────────────────────────────────────

import { PlanGate } from '../components/auth/PlanGate';

// ─── Helpers ──────────────────────────────────────────────────────────

function renderPlanGate(feature: string, children?: React.ReactNode) {
  return render(
    <MemoryRouter>
      <PlanGate feature={feature}>
        {children ?? <div data-testid="gated-content">Gated Content</div>}
      </PlanGate>
    </MemoryRouter>,
  );
}

// ─── Tests ────────────────────────────────────────────────────────────

describe('PlanGate', () => {
  beforeEach(() => {
    mockPlanState.currentPlan = { ...defaultCurrentPlan };
    mockPlanState.features = {};
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('renders children when feature is available', () => {
    mockPlanState.features = { forecasting: true };

    renderPlanGate('forecasting');

    expect(screen.getByTestId('gated-content')).toBeDefined();
    expect(screen.getByText('Gated Content')).toBeDefined();
    expect(screen.queryByTestId('upgrade-prompt')).toBeNull();
  });

  it('shows UpgradePrompt when feature is unavailable', () => {
    mockPlanState.features = { anomalyDetection: false };

    renderPlanGate('anomalyDetection');

    expect(screen.getByTestId('upgrade-prompt')).toBeDefined();
    expect(screen.queryByTestId('gated-content')).toBeNull();
  });

  it('shows UpgradePrompt for an unknown feature (not in feature map)', () => {
    mockPlanState.features = {};

    renderPlanGate('nonexistentFeature');

    expect(screen.getByTestId('upgrade-prompt')).toBeDefined();
    expect(screen.queryByTestId('gated-content')).toBeNull();
  });

  it('renders null while plan data is loading (currentPlan=null)', () => {
    mockPlanState.currentPlan = null;

    const { container } = renderPlanGate('forecasting');

    expect(screen.queryByTestId('upgrade-prompt')).toBeNull();
    expect(screen.queryByTestId('gated-content')).toBeNull();
    expect(container.innerHTML).toBe('');
  });

  it('passes upgradeMessage to UpgradePrompt when feature unavailable', () => {
    mockPlanState.features = { customAnalytics: false };

    render(
      <MemoryRouter>
        <PlanGate feature="customAnalytics" upgradeMessage="Please upgrade to Pro+">
          <div data-testid="gated-content">Analytics Dashboard</div>
        </PlanGate>
      </MemoryRouter>,
    );

    const prompt = screen.getByTestId('upgrade-prompt');
    expect(prompt).toBeDefined();
    expect(prompt.textContent).toContain('Please upgrade to Pro+');
  });

  it('renders complex children when feature available', () => {
    mockPlanState.features = { topology: true };

    renderPlanGate('topology', (
      <div data-testid="complex-child">
        <h1>Topology View</h1>
        <button>Refresh</button>
      </div>
    ));

    expect(screen.getByTestId('complex-child')).toBeDefined();
    expect(screen.getByText('Topology View')).toBeDefined();
    expect(screen.getByText('Refresh')).toBeDefined();
  });
});
