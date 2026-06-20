/**
 * TwinHeader — displays twin name, IDs with copy buttons, and status badge.
 *
 * Uses DS aef-container-card composition: __header (name + status badge)
 * and __body (ID stat-modules + agent count).
 *
 * @module components/twins/TwinHeader
 */

import { useCallback, useState } from 'react';
import { Copy, Check, Server } from 'lucide-react';
import type { TwinDetails } from '../../types/digitalParyty';
import { DetachableCard } from '../common/DetachableCard';

interface TwinHeaderProps {
  twin: TwinDetails;
}

const ID_CHARS = 7;

function truncateId(id: string): string {
  return id.length > ID_CHARS + 3 ? `${id.slice(0, ID_CHARS)}…` : id;
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard unavailable — silently fail
    }
  }, [text]);

  return (
    <button
      type="button"
      className="aef-btn aef-btn-inactive"
      onClick={handleCopy}
      aria-label={`Copy ${label}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 'var(--aef-space-1)',
        padding: 'var(--aef-space-0-5) var(--aef-space-2)',
        fontSize: 'var(--aef-font-size-2xs)',
      }}
      data-testid={`copy-${label.toLowerCase().replace(/\s+/g, '-')}`}
    >
      {copied ? <Check size={10} /> : <Copy size={10} />}
      {copied ? 'Copied' : 'Copy'}
    </button>
  );
}

/** Status-to-badge-variant mapping */
const STATUS_BADGE: Record<string, { variant: string; label: string }> = {
  active: { variant: 'badge-valid', label: 'Healthy' },
  healthy: { variant: 'badge-valid', label: 'Healthy' },
  degraded: { variant: 'badge-warning', label: 'Degraded' },
  pending: { variant: 'badge-pending', label: 'Pending' },
  inactive: { variant: 'badge-pending', label: 'Inactive' },
  unhealthy: { variant: 'badge-warning', label: 'Unhealthy' },
  unknown: { variant: 'badge-pending', label: 'Unknown' },
};

export function TwinHeader({ twin }: TwinHeaderProps) {
  const badge = STATUS_BADGE[twin.status] ?? STATUS_BADGE['unknown'];

  return (
    <DetachableCard
      title={twin.name}
      icon={<Server size={14} />}
      headerAction={
        <span className={`aef-badge ${badge.variant}`}>
          {badge.label}
        </span>
      }
      testId="twin-header"
    >
      {/* Twin ID row */}
      <div className="aef-stat-module">
          <span className="aef-stat-module__label">Twin ID</span>
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
            <code
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 'var(--aef-font-size-xs)',
                color: 'var(--aef-text-primary)',
              }}
            >
              {truncateId(twin.id)}
            </code>
            <CopyButton text={twin.id} label="Twin ID" />
          </div>
        </div>

        {/* Client ID row */}
        <div className="aef-stat-module">
          <span className="aef-stat-module__label">Client ID</span>
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
            <code
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 'var(--aef-font-size-xs)',
                color: 'var(--aef-text-primary)',
              }}
            >
              {truncateId(twin.tenantId)}
            </code>
            <CopyButton text={twin.tenantId} label="Client ID" />
          </div>
        </div>

        {/* Agent count */}
        <div className="aef-stat-module">
          <span className="aef-stat-module__label">Connected Agents</span>
          <span className="aef-stat-module__value">
            {twin.agentCount} agent{twin.agentCount !== 1 ? 's' : ''}
          </span>
        </div>
    </DetachableCard>
  );
}
