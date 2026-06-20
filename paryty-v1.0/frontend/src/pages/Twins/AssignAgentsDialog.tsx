/**
 * AssignAgentsDialog — modal for batch-assigning unassigned agents to a twin.
 *
 * Renders a selectable list of agents with checkboxes, a "Select All" toggle,
 * and a confirm button that assigns all selected agents sequentially. Follows
 * the DS modal composition established by TwinDeleteDialog.
 *
 * @module pages/Twins/AssignAgentsDialog
 */

import { useState, useCallback, useEffect, useRef, memo } from 'react';
import { UserPlus, X, Server, Monitor, Wifi, Check } from 'lucide-react';
import type { TwinAgentInfo } from '../../types/digitalParyty';

// ─── Types ──────────────────────────────────────────────────────────────────

interface AssignAgentsDialogProps {
  /** The twin to assign agents to. */
  twinId: string;
  /** Pre-fetched list of unassigned agents. */
  agents: TwinAgentInfo[];
  /** Callback to assign a single agent (reuses parent's handleAssign with toast). */
  onAssign: (agentId: string) => Promise<boolean>;
  /** Called when the dialog should close without action. */
  onClose: () => void;
  /** Called after at least one agent was successfully assigned. */
  onAssigned: () => void;
}

// ─── Component ──────────────────────────────────────────────────────────────

export const AssignAgentsDialog = memo(function AssignAgentsDialog({
  twinId: _twinId,
  agents,
  onAssign,
  onClose,
  onAssigned,
}: AssignAgentsDialogProps) {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [isAssigning, setIsAssigning] = useState(false);
  const [assignProgress, setAssignProgress] = useState({ done: 0, total: 0 });
  const [error, setError] = useState<string | null>(null);
  const modalRef = useRef<HTMLDivElement>(null);

  // ─── Focus management ───────────────────────────────────────────────

  useEffect(() => {
    modalRef.current?.focus();
  }, []);

  // ─── Keyboard handling ──────────────────────────────────────────────

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !isAssigning) {
        onClose();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isAssigning, onClose]);

  // ─── Body scroll lock ───────────────────────────────────────────────

  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = prev; };
  }, []);

  // ─── Selection handlers ─────────────────────────────────────────────

  const toggleAgent = useCallback((agentId: string) => {
    if (isAssigning) return;
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(agentId)) {
        next.delete(agentId);
      } else {
        next.add(agentId);
      }
      return next;
    });
  }, [isAssigning]);

  const toggleAll = useCallback(() => {
    if (isAssigning) return;
    setSelectedIds((prev) => {
      if (prev.size === agents.length) {
        return new Set();
      }
      return new Set(agents.map((a) => a.agentId));
    });
  }, [agents, isAssigning]);

  // ─── Assignment handler ─────────────────────────────────────────────

  const handleConfirm = useCallback(async () => {
    const selectedArray = Array.from(selectedIds);
    if (selectedArray.length === 0) return;

    setIsAssigning(true);
    setError(null);
    setAssignProgress({ done: 0, total: selectedArray.length });

    let succeeded = 0;
    let failed = 0;

    for (const agentId of selectedArray) {
      const ok = await onAssign(agentId);
      if (ok) {
        succeeded++;
      } else {
        failed++;
      }
      setAssignProgress((prev) => ({ ...prev, done: prev.done + 1 }));
    }

    setIsAssigning(false);

    if (succeeded > 0) {
      onAssigned();
    } else {
      setError(`Failed to assign ${failed} agent${failed !== 1 ? 's' : ''}. Please try again.`);
    }
  }, [selectedIds, onAssign, onAssigned]);

  // ─── Backdrop click ─────────────────────────────────────────────────

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (e.target === e.currentTarget && !isAssigning) {
        onClose();
      }
    },
    [isAssigning, onClose],
  );

  // ─── Derived state ──────────────────────────────────────────────────

  const allSelected = agents.length > 0 && selectedIds.size === agents.length;
  const someSelected = selectedIds.size > 0 && selectedIds.size < agents.length;
  const selectionCount = selectedIds.size;

  // ─── Render ─────────────────────────────────────────────────────────

  return (
    <div
      className="aef-modal-overlay"
      onClick={handleBackdropClick}
      data-testid="assign-agents-dialog"
    >
      <div
        ref={modalRef}
        className="aef-modal"
        role="dialog"
        aria-modal="true"
        aria-label="Assign agents to twin"
        tabIndex={-1}
        style={{ width: '560px', outline: 'none' }}
      >
        {/* Header */}
        <div className="aef-modal-header">
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
            <UserPlus size={14} style={{ color: 'var(--aef-text-secondary)' }} />
            <span className="aef-modal-title">Assign Agents</span>
          </div>
          <button
            type="button"
            className="aef-modal-close"
            onClick={onClose}
            disabled={isAssigning}
            aria-label="Close dialog"
            data-testid="aa-close-btn"
          >
            <X size={14} />
          </button>
        </div>

        {/* Body */}
        <div
          className="aef-modal-body"
          style={{ maxHeight: 'calc(100vh - 240px)' }}
        >
          {agents.length === 0 ? (
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
          ) : (
            <>
              {/* Select All */}
              <div className="aef-select-all-row">
                <div className="aef-agent-checkbox">
                  <input
                    type="checkbox"
                    checked={allSelected}
                    ref={(el) => {
                      if (el) el.indeterminate = someSelected;
                    }}
                    onChange={toggleAll}
                    disabled={isAssigning}
                    aria-label={allSelected ? 'Deselect all agents' : 'Select all agents'}
                    data-testid="aa-select-all"
                  />
                </div>
                <label htmlFor={undefined} style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
                  <span style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 'var(--aef-font-size-xs)',
                    color: 'var(--aef-text-secondary)',
                    userSelect: 'none',
                  }}>
                    {selectionCount > 0
                      ? `${selectionCount} of ${agents.length} selected`
                      : `${agents.length} agent${agents.length !== 1 ? 's' : ''} available`}
                  </span>
                </label>
              </div>

              {/* Agent rows */}
              {agents.map((agent) => {
                const isSelected = selectedIds.has(agent.agentId);

                return (
                  <div
                    key={agent.agentId}
                    className={`aef-agent-select-row${isSelected ? ' aef-agent-select-row--selected' : ''}`}
                    onClick={() => toggleAgent(agent.agentId)}
                    data-testid={`aa-agent-${agent.agentId}`}
                    role="checkbox"
                    aria-checked={isSelected}
                    aria-label={`Assign ${agent.hostname}`}
                  >
                    {/* Checkbox */}
                    <div className="aef-agent-checkbox">
                      <input
                        type="checkbox"
                        checked={isSelected}
                        onChange={() => toggleAgent(agent.agentId)}
                        disabled={isAssigning}
                        aria-label={`Select ${agent.hostname}`}
                        tabIndex={-1}
                        onClick={(e) => e.stopPropagation()}
                      />
                    </div>

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

                    {/* Check indicator */}
                    {isSelected && (
                      <Check size={14} style={{ color: 'var(--aef-status-live)', flexShrink: 0 }} />
                    )}
                  </div>
                );
              })}
            </>
          )}

          {/* Assignment progress */}
          {isAssigning && (
            <div className="aef-assign-progress">
              <span
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 'var(--aef-font-size-xs)',
                  color: 'var(--aef-text-secondary)',
                  whiteSpace: 'nowrap',
                }}
              >
                Assigning {assignProgress.done}/{assignProgress.total}…
              </span>
              <div className="aef-assign-progress__bar">
                <div
                  className="aef-assign-progress__fill"
                  style={{ width: `${assignProgress.total > 0 ? (assignProgress.done / assignProgress.total) * 100 : 0}%` }}
                />
              </div>
            </div>
          )}

          {/* Error message */}
          {error && (
            <div
              style={{
                padding: 'var(--aef-space-2) var(--aef-space-3)',
                background: 'var(--aef-status-error-soft)',
                border: 'var(--aef-border-width) solid var(--aef-status-error)',
                borderRadius: 'var(--aef-radius-control)',
              }}
            >
              <span
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 'var(--aef-font-size-xs)',
                  color: 'var(--aef-status-error)',
                }}
              >
                {error}
              </span>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="aef-modal-footer">
          <button
            type="button"
            className="aef-btn aef-btn-inactive"
            onClick={onClose}
            disabled={isAssigning}
            data-testid="aa-cancel-btn"
          >
            Cancel
          </button>
          <button
            type="button"
            className="aef-btn"
            onClick={handleConfirm}
            disabled={selectionCount === 0 || isAssigning}
            style={{
              background: selectionCount > 0 ? 'var(--aef-status-live)' : 'var(--aef-surface-low)',
              color: 'var(--aef-btn-active-text)',
              border: 'none',
              opacity: isAssigning ? 'var(--aef-disabled-opacity)' : 1,
            }}
            data-testid="aa-assign-btn"
          >
            {isAssigning ? (
              'Assigning…'
            ) : (
              <>
                <UserPlus size={10} /> Assign {selectionCount > 0 ? `${selectionCount} ` : ''}Agent{selectionCount !== 1 ? 's' : ''}
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
});
