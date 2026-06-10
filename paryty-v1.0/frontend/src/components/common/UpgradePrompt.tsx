/**
 * UpgradePrompt — shown when a feature is unavailable on the current plan.
 *
 * Renders a card with a message and a link to settings (plan tab).
 *
 * @module components/common/UpgradePrompt
 */

import { ArrowUpRight } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { usePlanStore } from '../../stores/planStore';

interface UpgradePromptProps {
  /** The feature that is unavailable. */
  feature?: string;
  /** Custom message override. */
  message?: string;
}

/**
 * Upgrade prompt card.
 *
 * Displays when a user tries to access a feature that their plan
 * does not include. Links to the settings page (plan tab) for upgrade.
 */
export function UpgradePrompt({ feature, message }: UpgradePromptProps) {
  const navigate = useNavigate();
  const planName = usePlanStore((s) => s.currentPlan?.planName);

  const defaultMessage = feature
    ? `This feature requires a Pro or Pro+ plan. Your current plan (${planName ?? 'Free'}) does not include "${feature}".`
    : `This feature requires a higher-tier plan. Your current plan is ${planName ?? 'Free'}.`;

  return (
    <div className="aef-container-card" style={{ margin: 'var(--aef-space-8)', maxWidth: 480 }}>
      <div className="aef-container-card__body">
        <p style={{
          fontFamily: 'var(--aef-font-body)',
          fontSize: 12,
          color: 'var(--aef-text-secondary)',
          lineHeight: 1.6,
        }}>
          {message ?? defaultMessage}
        </p>
        <button
          className="aef-btn aef-btn-active"
          onClick={() => navigate('/settings?tab=plan')}
          data-testid="upgrade-prompt-cta"
        >
          View Plans <ArrowUpRight size={12} />
        </button>
      </div>
    </div>
  );
}
