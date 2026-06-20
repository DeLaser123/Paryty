/**
 * AgentList — table of agents assigned to a Digital Paryty.
 *
 * Shows each agent's hostname, status dot, throughput indicator,
 * backlog status, and action buttons for offline agents with backlog.
 *
 * @module components/twins/AgentList
 */

import { useCallback } from 'react';
import { Server, Wifi, WifiOff, AlertTriangle } from 'lucide-react';
import clsx from 'clsx';
import type { TwinAgentInfo } from '../../types/digitalParyty';
import { BacklogAlert } from './BacklogAlert';

interface AgentListProps {
  /** List of assigned agents. */
  agents: TwinAgentInfo[];
  /** Callback when accepting a backlog. */
  onAcceptBacklog: (agentId: string) => Promise<boolean>;
  /** Callback when rejecting a backlog. */
  onRejectBacklog: (agentId: string) => Promise<boolean>;
}

function StatusDot({ status }: { status: TwinAgentInfo['status'] }) {
  const variant = status === 'online' ? 'healthy' : status === 'offline' ? 'unhealthy' : 'degraded';
  return (
    <span
      className={clsx('dp-card__health-dot', `dp-card__health-dot--${variant}`)}
      style={{ marginTop: 0, flexShrink: 0 }}
      title={status}
      aria-label={`Status: ${status}`}
    />
  );
}

function StatusIcon({ status }: { status: TwinAgentInfo['status'] }) {
  if (status === 'online') return <Wifi size={12} style={{ color: 'var(--aef-status-live)' }} />;
  if (status === 'offline') return <WifiOff size={12} style={{ color: 'var(--aef-status-error)' }} />;
  return <AlertTriangle size={12} style={{ color: 'var(--aef-status-warning)' }} />;
}

function timeSince(iso: string | undefined): string {
  // BUGFIX: Guard against undefined/null ISO string
  if (!iso) return 'unknown';
  const timestamp = new Date(iso).getTime();
  // BUGFIX: Guard against invalid date strings that produce NaN
  if (isNaN(timestamp)) return 'unknown';
  const diff = Date.now() - timestamp;
  const mins = Math.floor(diff / 60_000);
  const hrs = Math.floor(mins / 60);
  const days = Math.floor(hrs / 24);
  if (days > 0) return `${days}d ago`;
  if (hrs > 0) return `${hrs}h ago`;
  if (mins > 0) return `${mins}m ago`;
  return 'just now';
}

export function AgentList({ agents, onAcceptBacklog, onRejectBacklog }: AgentListProps) {
  const handleAccept = useCallback(
    (agentId: string) => onAcceptBacklog(agentId),
    [onAcceptBacklog],
  );
  const handleReject = useCallback(
    (agentId: string) => onRejectBacklog(agentId),
    [onRejectBacklog],
  );

  // BUGFIX: Guard against undefined/null agents array
  if (!agents || agents.length === 0) {
    return (
      <div
        style={{
          padding: 'var(--aef-space-4)',
          textAlign: 'center',
          fontFamily: 'var(--aef-font-body)',
          fontSize: 'var(--aef-font-size-xs)',
          color: 'var(--aef-text-secondary)',
        }}
        data-testid="agent-list-empty"
      >
        No agents assigned to this twin.
      </div>
    );
  }

  return (
    <div data-testid="assigned-agent-list">
      {agents.map((agent) => {
        const isOffline = agent.status === 'offline';
        // BUGFIX: Guard against undefined backlogBytes
        const hasBacklog = (agent.backlogBytes ?? 0) > 0;

        return (
          <div
            key={agent.agentId}
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 'var(--aef-space-3)',
              padding: 'var(--aef-space-3) 0',
              borderBottom: 'var(--aef-border-width) solid var(--aef-border)',
            }}
            data-testid={`assigned-agent-${agent.agentId}`}
          >
            {/* Status icon */}
            <StatusIcon status={agent.status} />

            {/* Agent info */}
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
                <Server size={12} style={{ color: 'var(--aef-text-secondary)' }} />
                <span
                  style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 'var(--aef-font-size-xs)',
                    fontWeight: 500,
                    color: 'var(--aef-text-primary)',
                  }}
                >
                  {agent.hostname}
                </span>
                <StatusDot status={agent.status} />
                <span
                  style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 'var(--aef-font-size-2xs)',
                    color: isOffline ? 'var(--aef-status-error)' : 'var(--aef-text-secondary)',
                    textTransform: 'capitalize',
                  }}
                >
                  {agent.status}
                  {isOffline && ` (${timeSince(agent.lastHeartbeat)})`}
                </span>
              </div>

              {/* Throughput + backlog row */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--aef-space-3)',
                  marginTop: 'var(--aef-space-1)',
                }}
              >
                <span style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 'var(--aef-font-size-2xs)',
                  color: 'var(--aef-text-secondary)',
                }}>
                  {/* Real throughput would come from metrics; show placeholder for now */}
                  {agent.status === 'online' ? 'Active' : 'Inactive'}
                </span>
                {hasBacklog && (
                  <span style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 'var(--aef-font-size-2xs)',
                    color: 'var(--aef-status-warning)',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 'var(--aef-space-1)',
                  }}>
                    <AlertTriangle size={10} /> Backlog
                  </span>
                )}
              </div>

              {/* Backlog actions */}
              {isOffline && hasBacklog && (
                <BacklogAlert
                  agentId={agent.agentId}
                  backlogBytes={agent.backlogBytes}
                  onAccept={handleAccept}
                  onReject={handleReject}
                />
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
