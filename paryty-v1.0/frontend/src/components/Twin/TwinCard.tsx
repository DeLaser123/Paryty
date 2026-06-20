/**
 * TwinCard — card component for displaying a Digital Paryty in list views.
 *
 * Uses DS aef-container-card composition with __header (name + badge)
 * and __body (description + stats). Hover lift animation, DS typography.
 *
 * @module components/Twin/TwinCard
 */

import { memo, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Server, Calendar } from 'lucide-react';
import clsx from 'clsx';
import type { TwinDetails } from '../../types/digitalParyty';
import { mapTwinStatus } from '../../types/digitalParyty';
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
 * Composed from DS primitives: aef-container-card with __header/__body,
 * aef-badge for status, aef-meta-pill for metadata, aef-motion-hover-lift
 * for interactive feedback.
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
      className={clsx(
        'aef-container-card',
        'aef-motion-hover-lift',
        className,
      )}
      style={{ cursor: 'pointer' }}
      role="button"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      data-testid={`twin-card-${twin.id}`}
      aria-label={`View twin: ${twin.name}`}
    >
      {/* Header: twin name + status badge */}
      <div className="aef-container-card__header">
        <span className="aef-container-card__title">
          {twin.name}
        </span>
        <TwinStatusBadge status={mapTwinStatus(twin.status)} />
      </div>

      {/* Body: description + stats */}
      <div className="aef-container-card__body" style={{ gap: 'var(--aef-space-2)' }}>
        {/* Description */}
        {twin.description && !compact && (
          <p
            style={{
              margin: 0,
              fontFamily: 'var(--aef-font-body)',
              fontSize: 'var(--aef-font-size-xs)',
              color: 'var(--aef-text-secondary)',
              lineHeight: 'var(--aef-line-height-normal)',
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
            gap: 'var(--aef-space-2)',
            flexWrap: 'wrap',
          }}
        >
          <span className="aef-meta-pill">
            <Server size={10} style={{ marginRight: 'var(--aef-space-1)' }} />
            {twin.agentCount} agent{twin.agentCount !== 1 ? 's' : ''}
          </span>

          <span className="aef-meta-pill" style={{ marginLeft: 'auto' }}>
            <Calendar size={10} style={{ marginRight: 'var(--aef-space-1)' }} />
            {formatDate(twin.updatedAt)}
          </span>
        </div>
      </div>
    </div>
  );
});
