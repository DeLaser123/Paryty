/**
 * StepIndicator — reusable step dots component for multi-step wizards.
 *
 * Renders a row of dots with the active step highlighted.
 *
 * @module components/common/StepIndicator
 */

import clsx from 'clsx';

interface StepIndicatorProps {
  /** Total number of steps. */
  total: number;
  /** Zero-based index of the current step. */
  current: number;
}

/**
 * Horizontally-arranged step dots for wizard navigation.
 *
 * Each dot is a small circle. The active step dot has a distinct style.
 */
export function StepIndicator({ total, current }: StepIndicatorProps) {
  return (
    <div
      className="dp-step-dots"
      aria-label={`Step ${current + 1} of ${total}`}
      data-testid="step-indicator"
    >
      {Array.from({ length: total }).map((_, i) => (
        <span
          key={i}
          className={clsx('dp-step-dot', i === current && 'dp-step-dot--active')}
          aria-current={i === current ? 'step' : undefined}
        />
      ))}
    </div>
  );
}
