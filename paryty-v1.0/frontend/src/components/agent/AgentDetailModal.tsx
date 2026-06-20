/**
 * AgentDetailModal — state-driven modal for Dual Reality agent management.
 *
 * Detects the agent's lifecycle state and renders different views:
 * - Unpaired: Pair with Cluster Agent prompt
 * - Active/Paired: Cluster agent info + action buttons
 * - Lost: Offline warning + Retire/Blacklist
 * - Retired: Read-only info
 * - Blacklisted: Blacklist reason + read-only
 *
 * @module components/agent/AgentDetailModal
 */

import { useState, useCallback } from 'react';
import {
  Server,
  Clock,
  Link2,
  Unlink,
  X,
  Trash2,
  Check,
  Pencil,
  Power,
  AlertTriangle,
  Ban,
  ShieldBan,
} from 'lucide-react';
import { getRestClient } from '../../api/rest';
import { useToastStore } from '../../stores/toastStore';
import type { AgentManagementInfo } from '../../types/agent';


// ─── Props ──────────────────────────────────────────────────────

interface AgentDetailModalProps {
  agent: AgentManagementInfo;
  onClose: () => void;
  onDelete: (agentId: string) => void;
  onUpdated: (agent: AgentManagementInfo) => void;
}

// ─── Confirmation Dialog ────────────────────────────────────────

interface ConfirmDialogProps {
  title: string;
  message: string;
  confirmLabel: string;
  confirmVariant?: 'danger' | 'warning';
  /** If true, show a text input for user to type a reason. */
  requireReason?: boolean;
  reasonLabel?: string;
  onConfirm: (reason?: string) => void;
  onCancel: () => void;
}

function ConfirmDialog({
  title,
  message,
  confirmLabel,
  confirmVariant = 'danger',
  requireReason = false,
  reasonLabel = 'Reason',
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const [reason, setReason] = useState('');
  const [confirmText, setConfirmText] = useState('');

  const borderColor = confirmVariant === 'danger'
    ? 'var(--aef-color-error, #ef4444)'
    : 'var(--aef-color-warning, #f59e0b)';

  return (
    <div className="aef-modal-overlay" onClick={onCancel}>
      <div
        className="aef-modal"
        style={{ maxWidth: 420 }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="aef-modal-header" style={{ borderBottomColor: borderColor }}>
          <span className="aef-modal-title" style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
            <AlertTriangle size={16} style={{ color: borderColor }} />
            {title}
          </span>
          <button className="aef-modal-close" onClick={onCancel} aria-label="Close">
            <X size={14} />
          </button>
        </div>

        <div className="aef-modal-body">
          <p style={{ fontSize: 13, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-3)' }}>
            {message}
          </p>

          {requireReason && (
            <div style={{ marginBottom: 'var(--aef-space-3)' }}>
              <label
                className="dp-field__label"
                style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}
              >
                {reasonLabel}
              </label>
              <input
                className="dp-field__input"
                type="text"
                placeholder="Enter a reason…"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                style={{ width: '100%' }}
                autoFocus
              />
            </div>
          )}

          {confirmVariant === 'danger' && (
            <div style={{ marginBottom: 'var(--aef-space-3)' }}>
              <label
                className="dp-field__label"
                style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}
              >
                Type <strong>CONFIRM</strong> to proceed
              </label>
              <input
                className="dp-field__input"
                type="text"
                placeholder="CONFIRM"
                value={confirmText}
                onChange={(e) => setConfirmText(e.target.value)}
                style={{ width: '100%' }}
              />
            </div>
          )}
        </div>

        <div className="aef-modal-footer">
          <button className="aef-btn aef-btn-inactive" onClick={onCancel}>
            Cancel
          </button>
          <button
            className={`aef-btn ${confirmVariant === 'danger' ? 'aef-btn-inactive' : 'aef-btn-active'}`}
            style={confirmVariant === 'danger' ? {
              background: 'var(--aef-color-error, #ef4444)',
              color: '#fff',
              borderColor: 'var(--aef-color-error, #ef4444)',
            } : undefined}
            onClick={() => onConfirm(requireReason ? reason : undefined)}
            disabled={
              (requireReason && !reason.trim()) ||
              (confirmVariant === 'danger' && confirmText !== 'CONFIRM')
            }
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Confirm Action Type ────────────────────────────────────────

interface ConfirmActionConfig {
  type: 'unpair' | 'retire' | 'blacklist' | 'unregister' | 'delete';
  title: string;
  message: string;
  confirmLabel: string;
  confirmVariant: 'danger' | 'warning';
  requireReason?: boolean;
}

// ─── Main Component ─────────────────────────────────────────────

export function AgentDetailModal({
  agent, onClose, onDelete, onUpdated,
}: AgentDetailModalProps) {
  const addToast = useToastStore((s) => s.addToast);
  const client = getRestClient();

  // ── Rename state ─────────────────────────────────────────────────
  const [isRenaming, setIsRenaming] = useState(false);
  const [newName, setNewName] = useState(agent.name || agent.hostname || '');
  const [isSaving, setIsSaving] = useState(false);

  // ── Confirmation dialog state ────────────────────────────────────
  const [confirmAction, setConfirmAction] = useState<ConfirmActionConfig | null>(null);

  // ── Determine lifecycle state ────────────────────────────────────
  const status = agent.status;
  const isPaired = Boolean(agent.assigned_twin);
  const isLost = status === 'lost';
  const isRetired = status === 'retired';
  const isBlacklisted = status === 'blacklisted';
  const isRogue = status === 'rogue';

  // ── Handlers ────────────────────────────────────────────────────

  const handleRename = useCallback(async () => {
    const trimmed = newName.trim();
    if (!trimmed || trimmed === agent.name) return;
    setIsSaving(true);
    try {
      const updated = await client.updateAgentName(agent.agent_id, trimmed);
      onUpdated(updated as AgentManagementInfo);
      addToast({ type: 'success', message: 'Edge agent renamed.' });
      setIsRenaming(false);
    } catch {
      addToast({ type: 'error', message: 'Failed to rename edge agent.' });
    } finally {
      setIsSaving(false);
    }
  }, [newName, agent, client, onUpdated, addToast]);

  const handleUnpair = useCallback(async () => {
    try {
      await client.unpairAgent(agent.agent_id);
      addToast({ type: 'success', message: 'Edge agent unpaired. It is now rogue.' });
      const resp = await client.get<{ data: AgentManagementInfo[] }>('/api/v1/agents/all');
      const updated = (resp.data ?? []).find((a: AgentManagementInfo) => a.agent_id === agent.agent_id);
      if (updated) onUpdated(updated);
    } catch {
      addToast({ type: 'error', message: 'Failed to unpair edge agent.' });
    }
  }, [agent, client, addToast, onUpdated]);

  const handleRetire = useCallback(async () => {
    try {
      await client.retireAgent(agent.agent_id);
      addToast({ type: 'success', message: 'Edge agent retired.' });
      const resp = await client.get<{ data: AgentManagementInfo[] }>('/api/v1/agents/all');
      const updated = (resp.data ?? []).find((a: AgentManagementInfo) => a.agent_id === agent.agent_id);
      if (updated) onUpdated(updated);
    } catch {
      addToast({ type: 'error', message: 'Failed to retire edge agent.' });
    }
  }, [agent, client, addToast, onUpdated]);

  const handleBlacklist = useCallback(async (reason?: string) => {
    if (!reason?.trim()) return;
    try {
      await client.blacklistAgent(agent.agent_id, reason.trim());
      addToast({ type: 'success', message: 'Edge agent blacklisted.' });
      const resp = await client.get<{ data: AgentManagementInfo[] }>('/api/v1/agents/all');
      const updated = (resp.data ?? []).find((a: AgentManagementInfo) => a.agent_id === agent.agent_id);
      if (updated) onUpdated(updated);
    } catch {
      addToast({ type: 'error', message: 'Failed to blacklist edge agent.' });
    }
  }, [agent, client, addToast, onUpdated]);

  const handleUnregister = useCallback(async () => {
    try {
      await client.unregisterAgent(agent.agent_id);
      addToast({ type: 'success', message: 'Edge agent unregistered and all data erased.' });
      onDelete(agent.agent_id);
      onClose();
    } catch {
      addToast({ type: 'error', message: 'Failed to unregister edge agent.' });
    }
  }, [agent, client, addToast, onDelete, onClose]);

  const handleDelete = useCallback(async () => {
    try {
      await client.delete(`/api/v1/agents/${agent.agent_id}`);
      addToast({ type: 'success', message: 'Edge agent deleted.' });
      onDelete(agent.agent_id);
      onClose();
    } catch {
      addToast({ type: 'error', message: 'Failed to delete edge agent.' });
    }
  }, [agent, client, addToast, onDelete, onClose]);

  const handleConfirmAction = useCallback((reason?: string) => {
    if (!confirmAction) return;
    setConfirmAction(null);

    switch (confirmAction.type) {
      case 'unpair':
        void handleUnpair();
        break;
      case 'retire':
        void handleRetire();
        break;
      case 'blacklist':
        void handleBlacklist(reason);
        break;
      case 'unregister':
        void handleUnregister();
        break;
      case 'delete':
        void handleDelete();
        break;
    }
  }, [confirmAction, handleUnpair, handleRetire, handleBlacklist, handleUnregister, handleDelete]);

  // ── Render helpers ──────────────────────────────────────────────

  const displayName = agent.name || agent.hostname || 'Unnamed Agent';

  const getStatusBadgeClass = (): string => {
    if (isRetired) return 'badge-inactive';
    if (isBlacklisted) return 'badge-invalid';
    if (isLost || isRogue) return 'badge-warning';
    if (isPaired) return 'badge-valid';
    return 'badge-pending';
  };

  const getStatusLabel = (): string => {
    if (isRetired) return 'Retired';
    if (isBlacklisted) return 'Blacklisted';
    if (isLost) return 'Lost';
    if (isRogue) return 'Rogue';
    if (isPaired) return 'Active';
    return status || 'Unconfigured';
  };

  // ── Render: Unpaired state ──────────────────────────────────────

  const renderUnpairedView = (): JSX.Element => (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      padding: 'var(--aef-space-6) var(--aef-space-4)',
      textAlign: 'center',
    }}>
      <Server size={40} style={{ color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-3)', opacity: 0.5 }} />
      <h3 style={{ fontSize: 15, fontWeight: 600, marginBottom: 'var(--aef-space-2)' }}>
        This edge agent is not paired with a cluster agent
      </h3>
      <p style={{ fontSize: 13, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-4)', maxWidth: 360 }}>
        To pair this edge agent, first create a Paryty Twin and assign a cluster agent to it.
        Then pair this edge agent with that cluster agent from the Twin detail page.
      </p>
      <div style={{
        fontSize: 12,
        color: 'var(--aef-text-secondary)',
        background: 'var(--aef-border)',
        borderRadius: 'var(--aef-radius-md)',
        padding: 'var(--aef-space-3)',
        textAlign: 'center',
        width: '100%',
        maxWidth: 360,
      }}>
        Go to <strong>Twins</strong> to create a Paryty Twin and assign cluster agents.
      </div>
    </div>
  );

  // ── Render: Active/Paired state ─────────────────────────────────

  const renderPairedView = (): JSX.Element => (
    <div>
      <div style={{
        background: 'var(--aef-bg-secondary, rgba(34, 197, 94, 0.08))',
        border: '1px solid var(--aef-color-success-border, rgba(34, 197, 94, 0.2))',
        borderRadius: 'var(--aef-radius-md)',
        padding: 'var(--aef-space-3)',
        marginBottom: 'var(--aef-space-4)',
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--aef-space-3)',
      }}>
        <Link2 size={18} style={{ color: 'var(--aef-color-success, #22c55e)', flexShrink: 0 }} />
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 'var(--aef-space-1)' }}>
            Paired Cluster Agent
          </div>
          <div style={{ fontSize: 12, color: 'var(--aef-text-secondary)' }}>
            <code>{agent.assigned_twin}</code>
            {agent.paired_at && (
              <span style={{ marginLeft: 'var(--aef-space-2)' }}>
                — paired {new Date(agent.paired_at).toLocaleString()}
              </span>
            )}
          </div>
        </div>
      </div>

      {/* Action buttons */}
      <div style={{
        display: 'grid',
        gridTemplateColumns: '1fr 1fr',
        gap: 'var(--aef-space-2)',
      }}>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={() => setConfirmAction({
            type: 'unpair',
            title: 'Unpair Edge Agent',
            message: 'This will disconnect the edge agent from its cluster agent. The edge agent will become rogue and the cluster agent will become unconfigured. This action can be reversed by re-pairing.',
            confirmLabel: 'Unpair',
            confirmVariant: 'warning',
          })}
          style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 'var(--aef-space-1)' }}
        >
          <Unlink size={12} /> Unpair
        </button>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={() => setConfirmAction({
            type: 'retire',
            title: 'Retire Edge Agent',
            message: 'This will gracefully decommission the edge agent. The agent will stop sending telemetry and enter a retired state. This is reversible.',
            confirmLabel: 'Retire Agent',
            confirmVariant: 'warning',
          })}
          style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 'var(--aef-space-1)' }}
        >
          <Power size={12} /> Retire
        </button>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={() => setConfirmAction({
            type: 'blacklist',
            title: 'Blacklist Edge Agent',
            message: 'Blacklisting permanently marks this agent as untrusted. Provide a reason for the blacklist entry.',
            confirmLabel: 'Blacklist Agent',
            confirmVariant: 'danger',
            requireReason: true,
          })}
          style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 'var(--aef-space-1)' }}
        >
          <ShieldBan size={12} /> Blacklist
        </button>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={() => setConfirmAction({
            type: 'unregister',
            title: 'Unregister Edge Agent',
            message: '⚠️ WARNING: This will permanently erase ALL data associated with this edge agent including metrics, traces, events, and configuration. This action CANNOT be undone.',
            confirmLabel: 'Unregister & Erase Data',
            confirmVariant: 'danger',
          })}
          style={{
            display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 'var(--aef-space-1)',
            color: 'var(--aef-color-error, #ef4444)',
            borderColor: 'var(--aef-color-error, #ef4444)',
          }}
        >
          <Trash2 size={12} /> Unregister
        </button>
      </div>
    </div>
  );

  // ── Render: Lost state ──────────────────────────────────────────

  const renderLostView = (): JSX.Element => (
    <div>
      <div style={{
        background: 'var(--aef-bg-warning, rgba(245, 158, 11, 0.08))',
        border: '1px solid var(--aef-color-warning-border, rgba(245, 158, 11, 0.3))',
        borderRadius: 'var(--aef-radius-md)',
        padding: 'var(--aef-space-3)',
        marginBottom: 'var(--aef-space-4)',
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--aef-space-3)',
      }}>
        <AlertTriangle size={18} style={{ color: 'var(--aef-color-warning, #f59e0b)', flexShrink: 0 }} />
        <div>
          <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 'var(--aef-space-1)' }}>
            Agent is Offline
          </div>
          <div style={{ fontSize: 12, color: 'var(--aef-text-secondary)' }}>
            This edge agent has not sent a heartbeat recently. It may have lost network connectivity or crashed.
          </div>
        </div>
      </div>

      {isPaired && (
        <div style={{
          marginBottom: 'var(--aef-space-4)',
          fontSize: 12,
          color: 'var(--aef-text-secondary)',
        }}>
          Paired with: <code>{agent.assigned_twin}</code>
        </div>
      )}

      <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={() => setConfirmAction({
            type: 'retire',
            title: 'Retire Edge Agent',
            message: 'This will gracefully decommission the lost edge agent.',
            confirmLabel: 'Retire Agent',
            confirmVariant: 'warning',
          })}
          style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 'var(--aef-space-1)' }}
        >
          <Power size={12} /> Retire
        </button>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={() => setConfirmAction({
            type: 'blacklist',
            title: 'Blacklist Edge Agent',
            message: 'Blacklisting permanently marks this agent as untrusted.',
            confirmLabel: 'Blacklist Agent',
            confirmVariant: 'danger',
            requireReason: true,
          })}
          style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 'var(--aef-space-1)' }}
        >
          <ShieldBan size={12} /> Blacklist
        </button>
      </div>
    </div>
  );

  // ── Render: Retired state ───────────────────────────────────────

  const renderRetiredView = (): JSX.Element => (
    <div>
      <div style={{
        background: 'var(--aef-bg-secondary, rgba(148, 163, 184, 0.08))',
        border: '1px solid var(--aef-border)',
        borderRadius: 'var(--aef-radius-md)',
        padding: 'var(--aef-space-3)',
        marginBottom: 'var(--aef-space-4)',
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--aef-space-3)',
      }}>
        <Power size={18} style={{ color: 'var(--aef-text-secondary)', flexShrink: 0 }} />
        <div>
          <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 'var(--aef-space-1)' }}>
            Edge Agent Retired
          </div>
          <div style={{ fontSize: 12, color: 'var(--aef-text-secondary)' }}>
            This edge agent has been gracefully decommissioned.
            {agent.retired_at && (
              <span> Retired on {new Date(agent.retired_at).toLocaleString()}.</span>
            )}
          </div>
        </div>
      </div>

      <div style={{
        fontSize: 12,
        color: 'var(--aef-text-secondary)',
        background: 'var(--aef-border)',
        borderRadius: 'var(--aef-radius-md)',
        padding: 'var(--aef-space-3)',
        textAlign: 'center',
      }}>
        <Ban size={14} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 'var(--aef-space-1)' }} />
        Cannot re-pair a retired agent. Create a new agent instead.
      </div>
    </div>
  );

  // ── Render: Blacklisted state ───────────────────────────────────

  const renderBlacklistedView = (): JSX.Element => (
    <div>
      <div style={{
        background: 'var(--aef-bg-error, rgba(239, 68, 68, 0.08))',
        border: '1px solid var(--aef-color-error-border, rgba(239, 68, 68, 0.3))',
        borderRadius: 'var(--aef-radius-md)',
        padding: 'var(--aef-space-3)',
        marginBottom: 'var(--aef-space-4)',
        display: 'flex',
        alignItems: 'flex-start',
        gap: 'var(--aef-space-3)',
      }}>
        <ShieldBan size={18} style={{ color: 'var(--aef-color-error, #ef4444)', flexShrink: 0, marginTop: 2 }} />
        <div>
          <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 'var(--aef-space-1)' }}>
            Edge Agent Blacklisted
          </div>
          <div style={{ fontSize: 12, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-1)' }}>
            This agent has been permanently marked as untrusted and cannot reconnect.
          </div>
          {agent.blacklist_reason && (
            <div style={{
              fontSize: 12,
              marginTop: 'var(--aef-space-2)',
              padding: 'var(--aef-space-2)',
              background: 'rgba(0,0,0,0.1)',
              borderRadius: 'var(--aef-radius-sm)',
            }}>
              <strong>Reason:</strong> {agent.blacklist_reason}
            </div>
          )}
          {agent.blacklisted_at && (
            <div style={{ fontSize: 11, color: 'var(--aef-text-secondary)', marginTop: 'var(--aef-space-1)' }}>
              Blacklisted on {new Date(agent.blacklisted_at).toLocaleString()}
            </div>
          )}
        </div>
      </div>

      <div style={{
        fontSize: 12,
        color: 'var(--aef-text-secondary)',
        background: 'var(--aef-border)',
        borderRadius: 'var(--aef-radius-md)',
        padding: 'var(--aef-space-3)',
        textAlign: 'center',
      }}>
        <Ban size={14} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 'var(--aef-space-1)' }} />
        This agent is read-only. No actions are available.
      </div>
    </div>
  );

  // ── Render: Lifecycle-specific content ──────────────────────────

  const renderLifecycleContent = (): JSX.Element => {
    if (isBlacklisted) return renderBlacklistedView();
    if (isRetired) return renderRetiredView();
    if (isLost) return renderLostView();
    if (isPaired) return renderPairedView();
    return renderUnpairedView();
  };

  // ── Render: Info grid (shared across all states) ────────────────

  const renderInfoGrid = (): JSX.Element => (
    <div style={{
      display: 'grid',
      gridTemplateColumns: '1fr 1fr',
      gap: 'var(--aef-space-3)',
      marginBottom: 'var(--aef-space-4)',
    }}>
      <div className="aef-stat-module">
        <span className="aef-stat-module__label">Agent ID</span>
        <code className="aef-stat-module__value" style={{ fontSize: 10 }}>{agent.agent_id}</code>
      </div>

      <div className="aef-stat-module">
        <span className="aef-stat-module__label">Hostname</span>
        <span className="aef-stat-module__value">{agent.hostname || '—'}</span>
      </div>

      <div className="aef-stat-module">
        <span className="aef-stat-module__label">OS / Arch</span>
        <span className="aef-stat-module__value">
          {agent.os ? `${agent.os}/${agent.arch}` : '—'}
        </span>
      </div>

      <div className="aef-stat-module">
        <span className="aef-stat-module__label">Last Seen</span>
        <span className="aef-stat-module__value" style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <Clock size={11} />
          {agent.last_seen ? new Date(agent.last_seen).toLocaleString() : 'Never'}
        </span>
      </div>

      {agent.identity_token && (
        <div className="aef-stat-module" style={{ gridColumn: '1 / -1' }}>
          <span className="aef-stat-module__label">Identity Token</span>
          <code className="aef-stat-module__value" style={{ fontSize: 10, wordBreak: 'break-all' }}>
            {agent.identity_token}
          </code>
        </div>
      )}
    </div>
  );

  // ── Main render ─────────────────────────────────────────────────

  return (
    <>
      <div className="aef-modal-overlay" onClick={onClose}>
        <div
          className="aef-modal"
          style={{ maxWidth: 540 }}
          onClick={(e) => e.stopPropagation()}
        >
          {/* Header */}
          <div className="aef-modal-header">
            <span className="aef-modal-title">Edge Agent Details</span>
            <button className="aef-modal-close" onClick={onClose} aria-label="Close">
              <X size={14} />
            </button>
          </div>

          <div className="aef-modal-body">
            {/* ── Name (editable) ──────────────────────────────── */}
            {isRenaming ? (
              <div style={{ marginBottom: 'var(--aef-space-4)' }}>
                <label
                  className="dp-field__label"
                  style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}
                >
                  Agent Name
                </label>
                <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
                  <input
                    className="dp-field__input"
                    type="text"
                    value={newName}
                    onChange={(e) => setNewName(e.target.value)}
                    autoFocus
                    style={{ flex: 1 }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') void handleRename();
                      if (e.key === 'Escape') setIsRenaming(false);
                    }}
                  />
                  <button
                    className="aef-btn aef-btn-active"
                    onClick={() => void handleRename()}
                    disabled={isSaving || !newName.trim()}
                  >
                    {isSaving ? 'Saving…' : <><Check size={12} /> Save</>}
                  </button>
                  <button
                    className="aef-btn aef-btn-inactive"
                    onClick={() => setIsRenaming(false)}
                  >
                    Cancel
                  </button>
                </div>
              </div>
            ) : (
              <div style={{
                display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)',
                marginBottom: 'var(--aef-space-4)',
              }}>
                <Server size={16} />
                <span style={{ fontWeight: 600, fontSize: 15, flex: 1 }}>{displayName}</span>
                <span className={`aef-badge ${getStatusBadgeClass()}`}>
                  {getStatusLabel()}
                </span>
                {!isRetired && !isBlacklisted && (
                  <button
                    className="aef-btn aef-btn-inactive"
                    onClick={() => { setNewName(displayName); setIsRenaming(true); }}
                    style={{ padding: '2px 6px' }}
                    title="Rename edge agent"
                  >
                    <Pencil size={12} />
                  </button>
                )}
              </div>
            )}

            {/* ── Info grid ────────────────────────────────────── */}
            {renderInfoGrid()}

            {/* ── Lifecycle-specific content ────────────────────── */}
            {renderLifecycleContent()}
          </div>

          <div className="aef-modal-footer">
            {!isRetired && !isBlacklisted && !isPaired && (
              <button
                className="aef-btn aef-btn-inactive"
                onClick={() => setConfirmAction({
                  type: 'delete',
                  title: 'Delete Edge Agent',
                  message: 'Are you sure you want to delete this unregistered edge agent? This action cannot be undone.',
                  confirmLabel: 'Delete Agent',
                  confirmVariant: 'danger',
                })}
              >
                <Trash2 size={12} /> Delete Agent
              </button>
            )}
            <button className="aef-btn aef-btn-active" onClick={onClose}>
              Close
            </button>
          </div>
        </div>
      </div>

      {/* ── Confirmation Dialog ─────────────────────────────────── */}
      {confirmAction && (
        <ConfirmDialog
          title={confirmAction.title}
          message={confirmAction.message}
          confirmLabel={confirmAction.confirmLabel}
          confirmVariant={confirmAction.confirmVariant}
          requireReason={confirmAction.requireReason}
          onConfirm={handleConfirmAction}
          onCancel={() => setConfirmAction(null)}
        />
      )}
    </>
  );
}
