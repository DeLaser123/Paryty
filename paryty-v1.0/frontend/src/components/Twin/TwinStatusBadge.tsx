/**
 * TwinStatusBadge — compact status indicator for Digital Paryty twins.
 *
 * Renders a colored dot + label based on the twin's aggregate health status.
 * Supports three variants: healthy, degraded, and unhealthy.
 *
 * @module components/Twin/TwinStatusBadge
 */

import { memo } from 'react';
import clsx from 'clsx';

// ─── Types ──────────────────────────────────────────────────────────────────

type TwinStatus = 'healthy' | 'degraded' | 'unhealthy' | 'unknown';

interface TwinStatusBadgeProps {
  /** The twin's aggregate health status. */
  status: TwinStatus;
  /** Optional override for the label text. */
  label?: string;
  /** Optional additional CSS classes. */
  className?: string;
  /** Optional data-testid prefix. */
  testId?: string;
}

// ─── Helpers ────────────────────────────────────────────────────────────────

const STATUS_MAP: Record<TwinStatus, { dot: string; label: string }> = {
  healthy: { dot: 'dp-card__health-dot--healthy', label: 'Healthy' },
  degraded: { dot: 'dp-card__health-dot--degraded', label: 'Degraded' },
  unhealthy: { dot: 'dp-card__health-dot--unhealthy', label: 'Unhealthy' },
  unknown: { dot: 'dp-card__health-dot--degraded', label: 'Unknown' },
};

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Renders a status badge with a colored health dot and text label.
 *
 * Uses the existing `dp-card__health-dot` CSS class pattern for the indicator
 * and follows the aef-* design system typography.
 */
export const TwinStatusBadge = memo(function TwinStatusBadge({
  status,
  label,
  className,
  testId,
}: TwinStatusBadgeProps) {
  const config = STATUS_MAP[status] ?? STATUS_MAP.unknown;
  const displayLabel = label ?? config.label;

  return (
    <span
      className={clsx('twin-status-badge', className)}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 'var(--aef-space-2)',
        whiteSpace: 'nowrap',
      }}
      data-testid={testId ?? 'twin-status-badge'}
    >
      <span
        className={clsx('dp-card__health-dot', config.dot)}
        style={{ marginTop: 0, flexShrink: 0 }}
        aria-hidden="true"
      />
      <span
        style={{
          fontFamily: 'var(--aef-font-body)',
          fontSize: 11,
          fontWeight: 500,
          color: 'var(--aef-text-primary)',
          textTransform: 'capitalize',
        }}
      >
        {displayLabel}
      </span>
    </span>
  );
});
