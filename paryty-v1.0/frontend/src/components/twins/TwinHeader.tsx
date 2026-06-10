/**
 * TwinHeader — displays twin name, IDs with copy buttons, and status badge.
 *
 * @module components/twins/TwinHeader
 */

import { useCallback, useState } from 'react';
import { Copy, Check } from 'lucide-react';
import clsx from 'clsx';
import type { TwinDetails } from '../../types/digitalParyty';

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
      title={`Copy ${label}`}
      aria-label={`Copy ${label}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        padding: '2px 8px',
        fontSize: 11,
      }}
      data-testid={`copy-${label.toLowerCase().replace(/\s+/g, '-')}`}
    >
      {copied ? <Check size={11} /> : <Copy size={11} />}
      {copied ? 'Copied' : 'Copy'}
    </button>
  );
}

export function TwinHeader({ twin }: TwinHeaderProps) {
  const statusLabel = twin.status === 'healthy' ? '● Healthy' : `● ${twin.status}`;

  return (
    <div
      className="aef-container-card"
      style={{ padding: 'var(--aef-space-4) var(--aef-space-5)' }}
      data-testid="twin-header"
    >
      {/* Twin name */}
      <div style={{ marginBottom: 'var(--aef-space-3)' }}>
        <h2
          style={{
            fontFamily: 'var(--aef-font-heading)',
            fontSize: 18,
            fontWeight: 600,
            color: 'var(--aef-text-primary)',
            margin: 0,
            lineHeight: 1.3,
          }}
        >
          Twin: &ldquo;{twin.name}&rdquo;
        </h2>
      </div>

      {/* IDs row */}
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: 'var(--aef-space-4)',
          marginBottom: 'var(--aef-space-3)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
          <span
            style={{
              fontFamily: 'var(--aef-font-body)',
              fontSize: 11,
              color: 'var(--aef-text-secondary)',
            }}
          >
            Twin ID:
          </span>
          <code
            style={{
              fontFamily: 'var(--aef-font-mono, monospace)',
              fontSize: 11,
              color: 'var(--aef-text-primary)',
              background: 'var(--aef-surface-low)',
              padding: '1px 6px',
              borderRadius: 'var(--aef-radius-control)',
            }}
          >
            {truncateId(twin.id)}
          </code>
          <CopyButton text={twin.id} label="Twin ID" />
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
          <span
            style={{
              fontFamily: 'var(--aef-font-body)',
              fontSize: 11,
              color: 'var(--aef-text-secondary)',
            }}
          >
            Client ID:
          </span>
          <code
            style={{
              fontFamily: 'var(--aef-font-mono, monospace)',
              fontSize: 11,
              color: 'var(--aef-text-primary)',
              background: 'var(--aef-surface-low)',
              padding: '1px 6px',
              borderRadius: 'var(--aef-radius-control)',
            }}
          >
            {truncateId(twin.tenantId)}
          </code>
          <CopyButton text={twin.tenantId} label="Client ID" />
        </div>
      </div>

      {/* Status row */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--aef-space-3)',
        }}
      >
        <span
          className={clsx('dp-card__health-dot', `dp-card__health-dot--${twin.status === 'healthy' ? 'healthy' : 'degraded'}`)}
          style={{ marginTop: 0 }}
        />
        <span
          style={{
            fontFamily: 'var(--aef-font-body)',
            fontSize: 12,
            fontWeight: 500,
            color: 'var(--aef-text-primary)',
          }}
        >
          {statusLabel}
        </span>
        <span
          style={{
            fontFamily: 'var(--aef-font-body)',
            fontSize: 11,
            color: 'var(--aef-text-secondary)',
          }}
        >
          {twin.agentCount} agent{twin.agentCount !== 1 ? 's' : ''} connected
        </span>
      </div>
    </div>
  );
}
