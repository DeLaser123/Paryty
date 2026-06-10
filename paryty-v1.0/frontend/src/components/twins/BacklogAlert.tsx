/**
 * BacklogAlert — inline alert for agents with pending backlog.
 *
 * Shows the backlog amount and provides Accept / Reject action buttons.
 *
 * @module components/twins/BacklogAlert
 */

import { useCallback, useState } from 'react';
import { AlertTriangle, Check, X } from 'lucide-react';

interface BacklogAlertProps {
  /** The agent with backlog. */
  agentId: string;
  /** Backlog size in bytes. */
  backlogBytes: number;
  /** Callback to accept the backlog. */
  onAccept: (agentId: string) => Promise<boolean>;
  /** Callback to reject the backlog. */
  onReject: (agentId: string) => Promise<boolean>;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function BacklogAlert({ agentId, backlogBytes, onAccept, onReject }: BacklogAlertProps) {
  const [isAccepting, setIsAccepting] = useState(false);
  const [isRejecting, setIsRejecting] = useState(false);
  const [resolved, setResolved] = useState(false);

  const handleAccept = useCallback(async () => {
    setIsAccepting(true);
    const ok = await onAccept(agentId);
    setIsAccepting(false);
    if (ok) setResolved(true);
  }, [agentId, onAccept]);

  const handleReject = useCallback(async () => {
    setIsRejecting(true);
    const ok = await onReject(agentId);
    setIsRejecting(false);
    if (ok) setResolved(true);
  }, [agentId, onReject]);

  if (resolved) return null;

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--aef-space-3)',
        padding: 'var(--aef-space-2) var(--aef-space-3)',
        background: 'rgba(245, 158, 11, 0.08)',
        border: '1px solid rgba(245, 158, 11, 0.3)',
        borderRadius: 'var(--aef-radius-control)',
        marginTop: 'var(--aef-space-2)',
      }}
      data-testid={`backlog-alert-${agentId}`}
    >
      <AlertTriangle size={14} style={{ color: 'var(--aef-warning)', flexShrink: 0 }} />
      <span
        style={{
          fontFamily: 'var(--aef-font-body)',
          fontSize: 11,
          color: 'var(--aef-text-primary)',
          flex: 1,
        }}
      >
        {formatBytes(backlogBytes)} backlog
      </span>
      <button
        type="button"
        className="aef-btn aef-btn-active"
        onClick={handleAccept}
        disabled={isAccepting || isRejecting}
        style={{ padding: '2px 10px', fontSize: 11, display: 'inline-flex', alignItems: 'center', gap: 4 }}
        data-testid={`backlog-accept-${agentId}`}
      >
        <Check size={12} />
        {isAccepting ? '…' : 'Accept'}
      </button>
      <button
        type="button"
        className="aef-btn aef-btn-inactive"
        onClick={handleReject}
        disabled={isAccepting || isRejecting}
        style={{ padding: '2px 10px', fontSize: 11, display: 'inline-flex', alignItems: 'center', gap: 4 }}
        data-testid={`backlog-reject-${agentId}`}
      >
        <X size={12} />
        {isRejecting ? '…' : 'Reject'}
      </button>
    </div>
  );
}
