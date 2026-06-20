/**
 * TwinStatusBadge — compact status indicator for Digital Paryty twins.
 *
 * Uses DS aef-badge primitive with badge-valid/badge-warning/badge-pending
 * variants for consistent status indication across the application.
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

const STATUS_MAP: Record<TwinStatus, { badge: string; label: string }> = {
  healthy: { badge: 'badge-valid', label: 'Healthy' },
  degraded: { badge: 'badge-warning', label: 'Degraded' },
  unhealthy: { badge: 'badge-warning', label: 'Unhealthy' },
  unknown: { badge: 'badge-pending', label: 'Unknown' },
};

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Renders a DS aef-badge with the appropriate status variant.
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
      className={clsx('aef-badge', config.badge, className)}
      data-testid={testId ?? 'twin-status-badge'}
    >
      {displayLabel}
    </span>
  );
});
