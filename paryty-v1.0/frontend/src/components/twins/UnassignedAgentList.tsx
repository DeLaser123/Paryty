/**
 * UnassignedAgentList — list of agents available for assignment to this twin.
 *
 * Each agent shows OS/arch, connection status, and an "Assign to this twin" button.
 *
 * @module components/twins/UnassignedAgentList
 */

import { useCallback, useState } from 'react';
import { Server, Monitor, Wifi, ArrowRight } from 'lucide-react';
import type { TwinAgentInfo } from '../../types/digitalParyty';

interface UnassignedAgentListProps {
  /** List of unassigned agents. */
  agents: TwinAgentInfo[];
  /** Callback when assigning an agent. */
  onAssign: (agentId: string) => Promise<boolean>;
}

export function UnassignedAgentList({ agents, onAssign }: UnassignedAgentListProps) {
  const [assigningIds, setAssigningIds] = useState<Set<string>>(new Set());

  const handleAssign = useCallback(
    async (agentId: string) => {
      setAssigningIds((prev) => new Set(prev).add(agentId));
      await onAssign(agentId);
      setAssigningIds((prev) => {
        const next = new Set(prev);
        next.delete(agentId);
        return next;
      });
    },
    [onAssign],
  );

  if (agents.length === 0) {
    return (
      <div
        style={{
          padding: 'var(--aef-space-4)',
          textAlign: 'center',
          fontFamily: 'var(--aef-font-body)',
          fontSize: 11,
          color: 'var(--aef-text-secondary)',
        }}
      >
        No unassigned agents available.
      </div>
    );
  }

  return (
    <div data-testid="unassigned-agent-list">
      {agents.map((agent) => {
        const isAssigning = assigningIds.has(agent.agentId);

        return (
          <div
            key={agent.agentId}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--aef-space-3)',
              padding: 'var(--aef-space-3) 0',
              borderBottom: 'var(--aef-border-width) solid var(--aef-border)',
            }}
            data-testid={`unassigned-agent-${agent.agentId}`}
          >
            {/* Agent icon */}
            <div
              style={{
                width: 28,
                height: 28,
                borderRadius: 'var(--aef-radius-control)',
                background: 'var(--aef-surface-low)',
                border: 'var(--aef-border-width) solid var(--aef-border)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                flexShrink: 0,
              }}
            >
              <Server size={14} style={{ color: 'var(--aef-text-secondary)' }} />
            </div>

            {/* Agent info */}
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
                <span
                  style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 12,
                    fontWeight: 500,
                    color: 'var(--aef-text-primary)',
                  }}
                >
                  {agent.hostname}
                </span>
              </div>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--aef-space-2)',
                  marginTop: 2,
                }}
              >
                <Monitor size={11} style={{ color: 'var(--aef-text-secondary)' }} />
                <span
                  style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 10,
                    color: 'var(--aef-text-secondary)',
                  }}
                >
                  {agent.os} {agent.arch}
                </span>
                <Wifi
                  size={11}
                  style={{
                    color: agent.status === 'online' ? 'var(--aef-status-live)' : 'var(--aef-text-secondary)',
                  }}
                />
                <span
                  style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 10,
                    color: agent.status === 'online' ? 'var(--aef-status-live)' : 'var(--aef-text-secondary)',
                    textTransform: 'capitalize',
                  }}
                >
                  {agent.status === 'online' ? 'Connected' : agent.status}
                </span>
              </div>
            </div>

            {/* Assign button */}
            <button
              type="button"
              className="aef-btn aef-btn-active"
              onClick={() => handleAssign(agent.agentId)}
              disabled={isAssigning}
              style={{
                padding: '4px 12px',
                fontSize: 11,
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                flexShrink: 0,
                opacity: isAssigning ? 0.5 : 1,
              }}
              data-testid={`assign-agent-${agent.agentId}`}
            >
              <ArrowRight size={12} />
              {isAssigning ? 'Assigning…' : 'Assign to this twin'}
            </button>
          </div>
        );
      })}
    </div>
  );
}
