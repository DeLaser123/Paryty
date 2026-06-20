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
                width: '28px',
                height: '28px',
                borderRadius: 'var(--aef-radius-control)',
                background: 'var(--aef-surface-low)',
                border: 'var(--aef-border-width) solid var(--aef-border)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                flexShrink: 0,
              }}
            >
              <Server size={12} style={{ color: 'var(--aef-text-secondary)' }} />
            </div>

            {/* Agent info */}
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
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
              </div>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--aef-space-2)',
                  marginTop: 'var(--aef-space-0-5)',
                }}
              >
                <Monitor size={10} style={{ color: 'var(--aef-text-secondary)' }} />
                <span
                  style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 'var(--aef-font-size-2xs)',
                    color: 'var(--aef-text-secondary)',
                  }}
                >
                  {agent.os} {agent.arch}
                </span>
                <Wifi
                  size={10}
                  style={{
                    color: agent.status === 'online' ? 'var(--aef-status-live)' : 'var(--aef-text-secondary)',
                  }}
                />
                <span
                  style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 'var(--aef-font-size-2xs)',
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
                padding: 'var(--aef-space-1) var(--aef-space-3)',
                fontSize: 'var(--aef-font-size-xs)',
                display: 'inline-flex',
                alignItems: 'center',
                gap: 'var(--aef-space-1)',
                flexShrink: 0,
                opacity: isAssigning ? 'var(--aef-disabled-opacity)' : 1,
              }}
              data-testid={`assign-agent-${agent.agentId}`}
            >
              <ArrowRight size={10} />
              {isAssigning ? 'Assigning…' : 'Assign to this twin'}
            </button>
          </div>
        );
      })}
    </div>
  );
}
