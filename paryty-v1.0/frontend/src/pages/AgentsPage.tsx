/**
 * AgentsPage — agent management page.
 *
 * Lists all agents with metadata, allows creating new agents,
 * and shows setup commands for deployment.
 *
 * @module pages/AgentsPage
 */

import { useState, useCallback, useEffect } from 'react';
import {
  Plus, Trash2, Copy, Check, Server, Clock, Link2, X,
} from 'lucide-react';
import { useToastStore } from '../stores/toastStore';
import { getRestClient } from '../api/rest';
import type { AgentManagementInfo, CreateAgentResponse } from '../types/agent';

// ─── Agent Table ─────────────────────────────────────────────────────

function AgentTable({
  agents,
  onDelete,
  onSelect,
}: {
  agents: AgentManagementInfo[];
  onDelete: (id: string) => void;
  onSelect: (agent: AgentManagementInfo) => void;
}) {
  if (agents.length === 0) {
    return (
      <div style={{
        padding: 'var(--aef-space-6)',
        textAlign: 'center',
        color: 'var(--aef-text-secondary)',
        fontSize: 13,
      }}>
        <Server size={32} style={{ marginBottom: 'var(--aef-space-3)', opacity: 0.4 }} />
        <p>No agents registered yet.</p>
        <p style={{ fontSize: 12, marginTop: 'var(--aef-space-1)' }}>
          Create an agent to start collecting infrastructure data.
        </p>
      </div>
    );
  }

  return (
    <div className="aef-table-card">
      <div className="aef-table-card__header">
        <Server size={14} />
        <span>Agents ({agents.length})</span>
      </div>
      <div style={{ overflowX: 'auto' }}>
        <table className="aef-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Agent ID</th>
              <th>Status</th>
              <th>Assigned Twin</th>
              <th>OS</th>
              <th>Location</th>
              <th>Last Seen</th>
              <th style={{ textAlign: 'right' }}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {agents.map((agent) => (
              <tr
                key={agent.agent_id}
                onClick={() => onSelect(agent)}
                style={{ cursor: 'pointer' }}
              >
                <td>
                  <span style={{ fontWeight: 500 }}>{agent.name || 'Unnamed'}</span>
                </td>
                <td>
                  <code style={{ fontSize: 11, color: 'var(--aef-text-secondary)' }}>{agent.agent_id}</code>
                </td>
                <td>
                  <span className={`aef-badge ${agent.status === 'deployed' ? 'badge-valid' : 'badge-pending'}`}>
                    {agent.status}
                  </span>
                </td>
                <td>
                  {agent.assigned_twin ? (
                    <span style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                      <Link2 size={12} />
                      <code style={{ fontSize: 11 }}>{agent.assigned_twin.slice(0, 8)}…</code>
                    </span>
                  ) : (
                    <span style={{ color: 'var(--aef-text-secondary)', fontSize: 11 }}>Unassigned</span>
                  )}
                </td>
                <td>
                  {agent.os ? `${agent.os}/${agent.arch}` : '—'}
                </td>
                <td>
                  {agent.location || agent.cloud_provider || '—'}
                </td>
                <td>
                  <span style={{ display: 'flex', alignItems: 'center', gap: '4px', color: 'var(--aef-text-secondary)' }}>
                    <Clock size={12} />
                    {new Date(agent.last_seen).toLocaleString()}
                  </span>
                </td>
                <td style={{ textAlign: 'right' }}>
                  <button
                    className="aef-btn aef-btn-inactive"
                    onClick={(e) => {
                      e.stopPropagation();
                      onDelete(agent.agent_id);
                    }}
                    aria-label={`Delete agent ${agent.name}`}
                    style={{ padding: '2px 6px' }}
                  >
                    <Trash2 size={12} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ─── Create Agent Modal ──────────────────────────────────────────────

function CreateAgentModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (response: CreateAgentResponse) => void;
}) {
  const [name, setName] = useState('');
  const [isCreating, setIsCreating] = useState(false);
  const addToast = useToastStore((s) => s.addToast);
  const client = getRestClient();

  const handleCreate = useCallback(async () => {
    if (!name.trim()) return;
    setIsCreating(true);
    try {
      const result = await client.post<CreateAgentResponse>('/api/v1/agents', { name: name.trim() });
      onCreated(result);
      addToast({ type: 'success', message: 'Agent created successfully.' });
    } catch {
      addToast({ type: 'error', message: 'Failed to create agent.' });
    } finally {
      setIsCreating(false);
    }
  }, [name, client, addToast, onCreated]);

  return (
    <div style={{
      position: 'fixed',
      inset: 0,
      background: 'rgba(0,0,0,0.6)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      zIndex: 1000,
    }} onClick={onClose}>
      <div
        className="aef-container-card"
        style={{ width: 480, maxWidth: '90vw' }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="aef-container-card__header">
          <h3 style={{ margin: 0, fontSize: 14 }}>Create New Agent</h3>
        </div>
        <div className="aef-container-card__body">
          <div style={{ marginBottom: 'var(--aef-space-4)' }}>
            <label className="dp-field__label" style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}>
              Agent Name
            </label>
            <input
              className="dp-field__input"
              type="text"
              placeholder="e.g. web-server-01, db-cluster-agent"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
              style={{ width: '100%' }}
            />
          </div>
          <div style={{ display: 'flex', gap: 'var(--aef-space-2)', justifyContent: 'flex-end' }}>
            <button className="aef-btn aef-btn-inactive" onClick={onClose}>
              Cancel
            </button>
            <button
              className="aef-btn aef-btn-active"
              onClick={handleCreate}
              disabled={isCreating || !name.trim()}
            >
              {isCreating ? 'Creating…' : 'Create Agent'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Setup Command Modal ─────────────────────────────────────────────

function SetupCommandModal({
  agentId,
  onClose,
}: {
  agentId: string;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const [copiedEnv, setCopiedEnv] = useState(false);

  // Generate setup commands
  const setupCommand = `paryty-agent --agent-id ${agentId}`;
  const envVarCommand = `export PARYTY_API_KEY="<YOUR_API_KEY>"`;

  const handleCopy = useCallback(async (text: string, setCopiedFn: (v: boolean) => void) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedFn(true);
      setTimeout(() => setCopiedFn(false), 3000);
    } catch {
      // Clipboard API not available
    }
  }, []);

  return (
    <div style={{
      position: 'fixed',
      inset: 0,
      background: 'rgba(0,0,0,0.6)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      zIndex: 1000,
    }} onClick={onClose}>
      <div
        className="aef-container-card"
        style={{ width: 600, maxWidth: '90vw' }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="aef-container-card__header">
          <h3 style={{ margin: 0, fontSize: 14 }}>Agent Setup Instructions</h3>
        </div>
        <div className="aef-container-card__body">
          <p style={{ fontSize: 12, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-3)' }}>
            To deploy this agent, you need to configure it with your API key.
            The API key authenticates the agent and binds it to your tenant.
          </p>

          {/* Method 1: Environment Variable */}
          <div style={{ marginBottom: 'var(--aef-space-4)' }}>
            <h4 style={{ fontSize: 12, marginBottom: 'var(--aef-space-2)', fontWeight: 600 }}>
              Method 1: Environment Variable (Recommended)
            </h4>
            <p style={{ fontSize: 12, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-2)' }}>
              Set the <code>PARYTY_API_KEY</code> environment variable before running the agent:
            </p>
            <div style={{
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--aef-space-2)',
              background: 'var(--aef-border)',
              borderRadius: 'var(--aef-radius-md)',
              padding: 'var(--aef-space-2) var(--aef-space-3)',
              marginBottom: 'var(--aef-space-2)',
            }}>
              <code style={{
                fontFamily: 'monospace',
                fontSize: 12,
                flex: 1,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}>
                {envVarCommand}
              </code>
              <button
                className="aef-btn aef-btn-active"
                onClick={() => handleCopy(envVarCommand, setCopiedEnv)}
              >
                {copiedEnv ? <><Check size={12} /> Copied</> : <><Copy size={12} /> Copy</>}
              </button>
            </div>
            <p style={{ fontSize: 11, color: 'var(--aef-text-secondary)', margin: 0 }}>
              Replace <code>&lt;YOUR_API_KEY&gt;</code> with the API key you created in Settings → API Keys.
            </p>
          </div>

          {/* Method 2: Configuration File */}
          <div style={{ marginBottom: 'var(--aef-space-4)' }}>
            <h4 style={{ fontSize: 12, marginBottom: 'var(--aef-space-2)', fontWeight: 600 }}>
              Method 2: Configuration File
            </h4>
            <p style={{ fontSize: 12, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-2)' }}>
              Add the API key to your agent configuration file (<code>configs/agent/agent.yaml</code>):
            </p>
            <pre style={{
              background: 'var(--aef-border)',
              padding: 'var(--aef-space-2) var(--aef-space-3)',
              borderRadius: 'var(--aef-radius-md)',
              overflow: 'auto',
              fontSize: 11,
              fontFamily: 'monospace',
              marginBottom: 'var(--aef-space-2)',
            }}>
{`agent:
  id: "${agentId}"
  api_key: "<YOUR_API_KEY>"
  cluster_endpoint: "grpc.paryty.io:443"`}
            </pre>
            <p style={{ fontSize: 11, color: 'var(--aef-text-secondary)', margin: 0 }}>
              Replace <code>&lt;YOUR_API_KEY&gt;</code> with your actual API key.
            </p>
          </div>

          {/* Run Command */}
          <div style={{ marginBottom: 'var(--aef-space-4)' }}>
            <h4 style={{ fontSize: 12, marginBottom: 'var(--aef-space-2)', fontWeight: 600 }}>
              Run the Agent
            </h4>
            <p style={{ fontSize: 12, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-2)' }}>
              After configuring the API key, run the agent:
            </p>
            <div style={{
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--aef-space-2)',
              background: 'var(--aef-border)',
              borderRadius: 'var(--aef-radius-md)',
              padding: 'var(--aef-space-2) var(--aef-space-3)',
              marginBottom: 'var(--aef-space-2)',
            }}>
              <code style={{
                fontFamily: 'monospace',
                fontSize: 12,
                flex: 1,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}>
                {setupCommand}
              </code>
              <button
                className="aef-btn aef-btn-active"
                onClick={() => handleCopy(setupCommand, setCopied)}
              >
                {copied ? <><Check size={12} /> Copied</> : <><Copy size={12} /> Copy</>}
              </button>
            </div>
          </div>

          {/* Installation Instructions */}
          <div style={{ marginBottom: 'var(--aef-space-4)' }}>
            <h4 style={{ fontSize: 12, marginBottom: 'var(--aef-space-2)', fontWeight: 600 }}>
              Installation
            </h4>
            <div style={{ fontSize: 12, color: 'var(--aef-text-secondary)' }}>
              <p style={{ marginBottom: 'var(--aef-space-2)' }}><strong>Linux/macOS:</strong></p>
              <pre style={{
                background: 'var(--aef-border)',
                padding: 'var(--aef-space-2)',
                borderRadius: 'var(--aef-radius-sm)',
                overflow: 'auto',
                fontSize: 11,
              }}>
{`# Download the agent binary
curl -fsSL https://get.paryty.io/agent.sh | sh

# Set your API key
${envVarCommand}

# Run the agent
${setupCommand}`}
              </pre>

              <p style={{ marginTop: 'var(--aef-space-3)', marginBottom: 'var(--aef-space-2)' }}><strong>Windows (PowerShell):</strong></p>
              <pre style={{
                background: 'var(--aef-border)',
                padding: 'var(--aef-space-2)',
                borderRadius: 'var(--aef-radius-sm)',
                overflow: 'auto',
                fontSize: 11,
              }}>
{`# Download the agent binary
Invoke-WebRequest -Uri "https://get.paryty.io/agent.ps1" -OutFile "install-agent.ps1"
.\install-agent.ps1

# Set your API key
$env:PARYTY_API_KEY = "<YOUR_API_KEY>"

# Run the agent
${setupCommand}`}
              </pre>
            </div>
          </div>

          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button className="aef-btn aef-btn-active" onClick={onClose}>
              Close
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Agent Detail Modal ──────────────────────────────────────────────

function AgentDetailModal({
  agent,
  onClose,
  onDelete,
}: {
  agent: AgentManagementInfo;
  onClose: () => void;
  onDelete: (id: string) => void;
}) {
  return (
    <div className="aef-modal-overlay" onClick={onClose}>
      <div
        className="aef-modal"
        style={{ maxWidth: 560 }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="aef-modal-header">
          <span className="aef-modal-title">Agent Details</span>
          <button className="aef-modal-close" onClick={onClose} aria-label="Close">
            <X size={14} />
          </button>
        </div>
        <div className="aef-modal-body">
          <div className="aef-container-card">
            <div className="aef-container-card__header">
              <Server size={14} />
              <span className="aef-container-card__title">{agent.name || 'Unnamed Agent'}</span>
              <span className={`aef-badge ${agent.status === 'deployed' ? 'badge-valid' : 'badge-pending'}`}>
                {agent.status}
              </span>
            </div>
            <div className="aef-container-card__body">
              {/* Agent ID */}
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">Agent ID</span>
                <code className="aef-stat-module__value" style={{ fontSize: 11 }}>{agent.agent_id}</code>
              </div>

              {/* Assigned Twin */}
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">Assigned Twin</span>
                <span className="aef-stat-module__value">
                  {agent.assigned_twin ? (
                    <span style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                      <Link2 size={12} />
                      <code style={{ fontSize: 11 }}>{agent.assigned_twin.slice(0, 8)}…</code>
                    </span>
                  ) : (
                    'Unassigned'
                  )}
                </span>
              </div>

              {/* OS/Arch */}
              {agent.os && (
                <div className="aef-stat-module">
                  <span className="aef-stat-module__label">OS/Arch</span>
                  <span className="aef-stat-module__value">{agent.os}/{agent.arch}</span>
                </div>
              )}

              {/* Location */}
              {(agent.location || agent.cloud_provider) && (
                <div className="aef-stat-module">
                  <span className="aef-stat-module__label">Location</span>
                  <span className="aef-stat-module__value">{agent.location || agent.cloud_provider}</span>
                </div>
              )}

              {/* Last Seen */}
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">Last Seen</span>
                <span className="aef-stat-module__value" style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                  <Clock size={12} />
                  {new Date(agent.last_seen).toLocaleString()}
                </span>
              </div>

              {/* Tags */}
              <div style={{ marginTop: 'var(--aef-space-2)' }}>
                <span className="aef-stat-module__label" style={{ marginBottom: 'var(--aef-space-2)', display: 'block' }}>Tags</span>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--aef-space-1)' }}>
                  <span className="aef-meta-pill">infrastructure</span>
                  <span className="aef-meta-pill">metrics</span>
                  <span className="aef-meta-pill">telemetry</span>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div className="aef-modal-footer">
          <button
            className="aef-btn aef-btn-inactive"
            onClick={() => {
              onDelete(agent.agent_id);
              onClose();
            }}
          >
            <Trash2 size={12} /> Delete Agent
          </button>
          <button className="aef-btn aef-btn-active" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Main Page ───────────────────────────────────────────────────────

export default function AgentsPage() {
  const [agents, setAgents] = useState<AgentManagementInfo[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [setupAgentId, setSetupAgentId] = useState<string | null>(null);
  const [selectedAgent, setSelectedAgent] = useState<AgentManagementInfo | null>(null);
  const addToast = useToastStore((s) => s.addToast);
  const client = getRestClient();

  const fetchAgents = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await client.get<{ data: AgentManagementInfo[] }>('/api/v1/agents/all');
      setAgents(data.data ?? []);
    } catch {
      addToast({ type: 'error', message: 'Failed to load agents.' });
    } finally {
      setIsLoading(false);
    }
  }, [client, addToast]);

  useEffect(() => {
    fetchAgents();
  }, [fetchAgents]);

  const handleDelete = useCallback(async (agentId: string) => {
    if (!window.confirm('Are you sure you want to delete this agent? This action cannot be undone.')) {
      return;
    }
    try {
      await client.delete(`/api/v1/agents/${agentId}`);
      setAgents((prev) => prev.filter((a) => a.agent_id !== agentId));
      addToast({ type: 'success', message: 'Agent deleted.' });
    } catch {
      addToast({ type: 'error', message: 'Failed to delete agent.' });
    }
  }, [client, addToast]);

  const handleCreated = useCallback((response: CreateAgentResponse) => {
    setShowCreateModal(false);
    setSetupAgentId(response.agent_id);
    fetchAgents();
  }, [fetchAgents]);

  return (
    <div style={{ maxWidth: 1200, margin: '0 auto', padding: 'var(--aef-space-6)' }}>
      {/* Header */}
      <div style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        marginBottom: 'var(--aef-space-6)',
      }}>
        <div>
          <h1 style={{ fontSize: 18, fontWeight: 600, marginBottom: 'var(--aef-space-1)' }}>
            Agents
          </h1>
          <p style={{ fontSize: 13, color: 'var(--aef-text-secondary)', margin: 0 }}>
            Manage your infrastructure agents. Deploy agents to collect metrics and telemetry.
          </p>
        </div>
        <button
          className="aef-btn aef-btn-active"
          onClick={() => setShowCreateModal(true)}
        >
          <Plus size={14} /> Create Agent
        </button>
      </div>

      {/* Content */}
      {isLoading ? (
        <div className="aef-container-card">
          <div className="aef-container-card__body" style={{ textAlign: 'center' }}>
            Loading agents…
          </div>
        </div>
      ) : (
        <AgentTable agents={agents} onDelete={handleDelete} onSelect={setSelectedAgent} />
      )}

      {/* Modals */}
      {showCreateModal && (
        <CreateAgentModal
          onClose={() => setShowCreateModal(false)}
          onCreated={handleCreated}
        />
      )}
      {setupAgentId && (
        <SetupCommandModal
          agentId={setupAgentId}
          onClose={() => setSetupAgentId(null)}
        />
      )}
      {selectedAgent && (
        <AgentDetailModal
          agent={selectedAgent}
          onClose={() => setSelectedAgent(null)}
          onDelete={handleDelete}
        />
      )}
    </div>
  );
}
