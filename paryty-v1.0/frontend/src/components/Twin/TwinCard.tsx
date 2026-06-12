/**
 * TwinCard — card component for displaying a Digital Paryty in list views.
 *
 * Shows twin name, status badge, agent count, system label,
 * and a clickable area that navigates to the twin detail page.
 *
 * @module components/Twin/TwinCard
 */

import { memo, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Server, Calendar } from 'lucide-react';
import clsx from 'clsx';
import type { TwinDetails } from '../../types/digitalParyty';
import { TwinStatusBadge } from './TwinStatusBadge';

// ─── Types ──────────────────────────────────────────────────────────────────

interface TwinCardProps {
  /** The twin data to render. */
  twin: TwinDetails;
  /** Optional click handler override (defaults to navigate). */
  onClick?: (twinId: string) => void;
  /** Optional additional CSS classes. */
  className?: string;
  /** Whether the card should render in a compact variant. */
  compact?: boolean;
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    });
  } catch {
    return iso;
  }
}

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * A clickable card representing a single Digital Paryty twin.
 *
 * Uses the `aef-container-card` class for the outer shell and
 * the design system typography variables for consistent styling.
 */
export const TwinCard = memo(function TwinCard({
  twin,
  onClick,
  className,
  compact = false,
}: TwinCardProps) {
  const navigate = useNavigate();

  const handleClick = useCallback(() => {
    if (onClick) {
      onClick(twin.id);
    } else {
      navigate(`/twins/${twin.id}`);
    }
  }, [onClick, twin.id, navigate]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        handleClick();
      }
    },
    [handleClick],
  );

  return (
    <div
      className={clsx('aef-container-card', 'twin-card', className)}
      style={{
        padding: compact ? 'var(--aef-space-3) var(--aef-space-4)' : 'var(--aef-space-4) var(--aef-space-5)',
        cursor: 'pointer',
        transition: 'border-color 0.15s ease',
      }}
      role="button"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      data-testid={`twin-card-${twin.id}`}
      aria-label={`View twin: ${twin.name}`}
    >
      {/* Header row: name + status */}
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-start',
          justifyContent: 'space-between',
          gap: 'var(--aef-space-3)',
          marginBottom: compact ? 'var(--aef-space-2)' : 'var(--aef-space-3)',
        }}
      >
        <h3
          style={{
            margin: 0,
            fontFamily: 'var(--aef-font-heading)',
            fontSize: compact ? 13 : 15,
            fontWeight: 600,
            color: 'var(--aef-text-primary)',
            lineHeight: 1.3,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            flex: 1,
            minWidth: 0,
          }}
        >
          {twin.name}
        </h3>
        <TwinStatusBadge status={twin.status as 'healthy' | 'degraded' | 'unhealthy' | 'unknown'} />
      </div>

      {/* System label */}
      {twin.description && !compact && (
        <p
          style={{
            margin: 0,
            fontFamily: 'var(--aef-font-body)',
            fontSize: 11,
            color: 'var(--aef-text-secondary)',
            lineHeight: 1.5,
            marginBottom: 'var(--aef-space-3)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {twin.description}
        </p>
      )}

      {/* Stats row */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--aef-space-4)',
          flexWrap: 'wrap',
        }}
      >
        {/* Agent count */}
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 4,
            fontFamily: 'var(--aef-font-body)',
            fontSize: 10,
            color: 'var(--aef-text-secondary)',
          }}
        >
          <Server size={11} />
          {twin.agentCount} agent{twin.agentCount !== 1 ? 's' : ''}
        </span>

        {/* Updated date */}
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 4,
            fontFamily: 'var(--aef-font-body)',
            fontSize: 10,
            color: 'var(--aef-text-secondary)',
            marginLeft: 'auto',
          }}
        >
          <Calendar size={10} />
          {formatDate(twin.updatedAt)}
        </span>
      </div>
    </div>
  );
});
