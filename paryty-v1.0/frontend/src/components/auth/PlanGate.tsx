/**
 * PlanGate — renders children only if the current plan has a feature.
 *
 * Otherwise renders an UpgradePrompt.
 *
 * @module components/auth/PlanGate
 */

import { type ReactNode } from 'react';
import { usePlanStore } from '../../stores/planStore';
import { UpgradePrompt } from '../common/UpgradePrompt';

interface PlanGateProps {
  /** Feature flag to check. */
  feature: string;
  /** Content to render when feature is available. */
  children: ReactNode;
  /** Optional custom message for the upgrade prompt. */
  upgradeMessage?: string;
}

/**
 * Feature gate component.
 *
 * Checks the current plan's feature flags. If the feature is enabled,
 * renders `children`. Otherwise renders an `UpgradePrompt`.
 */
export function PlanGate({ feature, children, upgradeMessage }: PlanGateProps) {
  const hasFeature = usePlanStore((s) => s.hasFeature(feature));
  const currentPlan = usePlanStore((s) => s.currentPlan);

  // While plan data is loading, render nothing (avoids flash)
  if (!currentPlan) {
    return null;
  }

  if (!hasFeature) {
    return <UpgradePrompt feature={feature} message={upgradeMessage} />;
  }

  return <>{children}</>;
}
