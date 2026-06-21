/**
 * AgentsPage — Edge Agent management page.
 *
 * Lists all edge agents with metadata, allows creating new agents,
 * and shows a carousel-style setup wizard for OS-specific deployment.
 *
 * @module pages/AgentsPage
 */

import { useState, useCallback, useEffect, useRef, useMemo } from 'react';
import {
  Plus, Trash2, Copy, Check, Server, Clock, Link2,
  Monitor, Terminal, Loader2, ChevronRight, ChevronLeft,
  ArrowUp, ArrowDown,
} from 'lucide-react';
import { useToastStore } from '../stores/toastStore';
import { getRestClient } from '../api/rest';
import { AgentDetailModal } from '../components/agent/AgentDetailModal';
import type { AgentManagementInfo, CreateAgentResponse, AgentPairingStatus } from '../types/agent';

// ─── Agent Table ─────────────────────────────────────────────────────

type SortKey = 'name' | 'status' | 'os' | 'location' | 'last_seen';
type SortDir = 'asc' | 'desc';

function AgentTable({
  agents,
  onDelete,
  onSelect,
}: {
  agents: AgentManagementInfo[];
  onDelete: (id: string) => void;
  onSelect: (agent: AgentManagementInfo) => void;
}) {
  const [sortKey, setSortKey] = useState<SortKey>('name');
  const [sortDir, setSortDir] = useState<SortDir>('asc');

  const handleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortKey(key);
      setSortDir('asc');
    }
  };

  const sortedAgents = useMemo(() => {
    const sorted = [...agents].sort((a, b) => {
      let cmp = 0;
      switch (sortKey) {
        case 'name':
          cmp = (a.name || '').localeCompare(b.name || '');
          break;
        case 'status':
          cmp = (a.status || '').localeCompare(b.status || '');
          break;
        case 'os':
          cmp = `${a.os}/${a.arch}`.localeCompare(`${b.os}/${b.arch}`);
          break;
        case 'location':
          cmp = (a.location || a.cloud_provider || '').localeCompare(b.location || b.cloud_provider || '');
          break;
        case 'last_seen':
          cmp = new Date(a.last_seen).getTime() - new Date(b.last_seen).getTime();
          break;
      }
      return sortDir === 'asc' ? cmp : -cmp;
    });
    return sorted;
  }, [agents, sortKey, sortDir]);

  const SortIcon = ({ columnKey }: { columnKey: SortKey }) => {
    if (sortKey !== columnKey) return null;
    return sortDir === 'asc' ? <ArrowUp size={10} /> : <ArrowDown size={10} />;
  };

  if (agents.length === 0) {
    return (
      <div style={{
        padding: 'var(--aef-space-6)',
        textAlign: 'center',
        color: 'var(--aef-text-secondary)',
        fontSize: 13,
      }}>
        <Server size={32} style={{ marginBottom: 'var(--aef-space-3)', opacity: 0.4 }} />
        <p>No edge agents registered yet.</p>
        <p style={{ fontSize: 12, marginTop: 'var(--aef-space-1)' }}>
          Create an edge agent to start collecting infrastructure data.
        </p>
      </div>
    );
  }

  return (
    <div className="aef-table-card">
      <div className="aef-table-card__header">
        <Server size={14} />
        <span>Edge Agents ({agents.length})</span>
      </div>
      <div style={{ overflowX: 'auto' }}>
        <table className="aef-table">
          <thead>
            <tr>
              <th onClick={() => handleSort('name')} style={{ cursor: 'pointer' }}>Name <SortIcon columnKey="name" /></th>
              <th>Agent ID</th>
              <th onClick={() => handleSort('status')} style={{ cursor: 'pointer' }}>Status <SortIcon columnKey="status" /></th>
              <th>Paired Cluster Agent</th>
              <th onClick={() => handleSort('os')} style={{ cursor: 'pointer' }}>OS <SortIcon columnKey="os" /></th>
              <th onClick={() => handleSort('location')} style={{ cursor: 'pointer' }}>Location <SortIcon columnKey="location" /></th>
              <th onClick={() => handleSort('last_seen')} style={{ cursor: 'pointer' }}>Last Seen <SortIcon columnKey="last_seen" /></th>
              <th style={{ textAlign: 'right' }}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {sortedAgents.map((agent) => (
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
                  <span className={`aef-badge ${agent.status === 'deployed' || agent.status === 'active' ? 'badge-valid' : agent.status === 'lost' || agent.status === 'rogue' ? 'badge-warning' : agent.status === 'blacklisted' ? 'badge-invalid' : agent.status === 'retired' ? 'badge-inactive' : 'badge-pending'}`}>
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
                    <span style={{ color: 'var(--aef-text-secondary)', fontSize: 11 }}>Unpaired</span>
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
                    aria-label={`Unregister edge agent ${agent.name}`}
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
      // BUGFIX: Backend wraps response in {"data": ...} envelope. Extract .data.
      const resp = await client.post<{ data: CreateAgentResponse }>('/api/v1/agents', { name: name.trim() });
      onCreated(resp.data);
      addToast({ type: 'success', message: 'Edge agent created successfully.' });
    } catch {
      addToast({ type: 'error', message: 'Failed to create edge agent.' });
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
          <h3 style={{ margin: 0, fontSize: 14 }}>Create Edge Agent</h3>
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
              {isCreating ? 'Creating…' : 'Create Edge Agent'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Setup Wizard Types ──────────────────────────────────────────────

type WizardStep = 'select-os' | 'windows-setup' | 'linux-setup' | 'verification';
type TargetOS = 'windows' | 'linux';

// ─── Setup Wizard Modal (Carousel-style) ─────────────────────────────

function SetupCommandModal({
  agentId,
  onClose,
}: {
  agentId: string;
  onClose: () => void;
}) {
  const [step, setStep] = useState<WizardStep>('select-os');
  const [targetOS, setTargetOS] = useState<TargetOS | null>(null);
  const [copied, setCopied] = useState(false);
  const [clusterAgentId, setClusterAgentId] = useState('');
  const [apiKey, setApiKey] = useState('<YOUR_API_KEY>');
  const [pairingStatus, setPairingStatus] = useState<AgentPairingStatus | null>(null);
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const addToast = useToastStore((s) => s.addToast);
  const client = getRestClient();

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (pollingRef.current) {
        clearInterval(pollingRef.current);
      }
    };
  }, []);

  const handleCopy = useCallback(async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 3000);
    } catch (err) {
      console.error('Failed to copy to clipboard:', err);
    }
  }, []);

  const handleSelectOS = useCallback((os: TargetOS) => {
    setTargetOS(os);
    setStep(os === 'windows' ? 'windows-setup' : 'linux-setup');
  }, []);

  const startVerification = useCallback(() => {
    setStep('verification');

    // Poll immediately once
    client.getAgentPairingStatus(agentId)
      .then((status) => {
        setPairingStatus(status);
        if (status.edge_status === 'active') {
          return;
        }
      })
      .catch((err) => {
        console.error('Failed to fetch initial pairing status:', err);
      });

    // Then poll every 5 seconds
    pollingRef.current = setInterval(async () => {
      try {
        const status = await client.getAgentPairingStatus(agentId);
        setPairingStatus(status);
        if (status.edge_status === 'active') {
          if (pollingRef.current) {
            clearInterval(pollingRef.current);
            pollingRef.current = null;
          }
          addToast({ type: 'success', message: 'Edge agent connected successfully!' });
        }
      } catch (err) {
        console.error('Pairing status poll failed, will retry:', err);
      }
    }, 5000);
  }, [agentId, client, addToast]);

  const buildLinuxCommand = (): string => {
    const baseUrl = window.location.origin;
    const parts = [`--key ${apiKey}`];
    if (clusterAgentId.trim()) {
      parts.push(`--cluster-agent-id ${clusterAgentId.trim()}`);
    }
    return `curl -fsSL "${baseUrl}/api/v1/agents/download/linux?key=${apiKey}" -o paryty-agent && chmod +x paryty-agent && ./paryty-agent ${parts.join(' ')}`;
  };

  const buildWindowsCommand = (): string => {
    const baseUrl = window.location.origin;
    const parts = [`-Key "${apiKey}"`];
    if (clusterAgentId.trim()) {
      parts.push(`-ClusterAgentId "${clusterAgentId.trim()}"`);
    }
    return `Invoke-WebRequest -Uri "${baseUrl}/api/v1/agents/download/windows?key=${apiKey}" -OutFile "paryty-agent.exe"; .\\paryty-agent.exe ${parts.join(' ')}`;
  };

  // ── Step indicators ────────────────────────────────────────────

  const renderStepIndicator = (): JSX.Element => {
    const steps = ['Select OS', 'Setup', 'Verify'];
    const currentIndex = step === 'select-os' ? 0 : step === 'verification' ? 2 : 1;

    return (
      <div style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 'var(--aef-space-2)',
        marginBottom: 'var(--aef-space-4)',
      }}>
        {steps.map((label, i) => (
          <div key={label} style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
            <div style={{
              width: 24,
              height: 24,
              borderRadius: '50%',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 11,
              fontWeight: 600,
              background: i <= currentIndex ? 'var(--aef-color-primary, #6366f1)' : 'var(--aef-border)',
              color: i <= currentIndex ? '#fff' : 'var(--aef-text-secondary)',
            }}>
              {i + 1}
            </div>
            <span style={{
              fontSize: 12,
              fontWeight: i === currentIndex ? 600 : 400,
              color: i === currentIndex ? 'var(--aef-text-primary)' : 'var(--aef-text-secondary)',
            }}>
              {label}
            </span>
            {i < steps.length - 1 && (
              <ChevronRight size={12} style={{ color: 'var(--aef-text-secondary)' }} />
            )}
          </div>
        ))}
      </div>
    );
  };

  // ── Step 1: Select OS ──────────────────────────────────────────

  const renderSelectOS = (): JSX.Element => (
    <div>
      <p style={{
        fontSize: 13,
        color: 'var(--aef-text-secondary)',
        marginBottom: 'var(--aef-space-4)',
        textAlign: 'center',
      }}>
        Select the operating system of the target machine where you want to deploy the edge agent.
      </p>
      <div style={{
        display: 'grid',
        gridTemplateColumns: '1fr 1fr',
        gap: 'var(--aef-space-3)',
      }}>
        {/* Windows Card */}
        <button
          onClick={() => handleSelectOS('windows')}
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            gap: 'var(--aef-space-2)',
            padding: 'var(--aef-space-5) var(--aef-space-4)',
            background: 'var(--aef-bg-primary)',
            border: '2px solid var(--aef-border)',
            borderRadius: 'var(--aef-radius-lg)',
            cursor: 'pointer',
            transition: 'border-color 0.15s, background 0.15s',
            color: 'var(--aef-text-primary)',
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.borderColor = 'var(--aef-color-primary, #6366f1)';
            e.currentTarget.style.background = 'var(--aef-bg-hover, rgba(99, 102, 241, 0.04))';
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.borderColor = 'var(--aef-border)';
            e.currentTarget.style.background = 'var(--aef-bg-primary)';
          }}
        >
          <Monitor size={32} style={{ color: 'var(--aef-color-primary, #6366f1)' }} />
          <span style={{ fontSize: 14, fontWeight: 600 }}>Windows</span>
          <span style={{ fontSize: 11, color: 'var(--aef-text-secondary)' }}>
            PowerShell installer
          </span>
        </button>

        {/* Linux Card */}
        <button
          onClick={() => handleSelectOS('linux')}
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            gap: 'var(--aef-space-2)',
            padding: 'var(--aef-space-5) var(--aef-space-4)',
            background: 'var(--aef-bg-primary)',
            border: '2px solid var(--aef-border)',
            borderRadius: 'var(--aef-radius-lg)',
            cursor: 'pointer',
            transition: 'border-color 0.15s, background 0.15s',
            color: 'var(--aef-text-primary)',
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.borderColor = 'var(--aef-color-primary, #6366f1)';
            e.currentTarget.style.background = 'var(--aef-bg-hover, rgba(99, 102, 241, 0.04))';
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.borderColor = 'var(--aef-border)';
            e.currentTarget.style.background = 'var(--aef-bg-primary)';
          }}
        >
          <Terminal size={32} style={{ color: 'var(--aef-color-primary, #6366f1)' }} />
          <span style={{ fontSize: 14, fontWeight: 600 }}>Linux</span>
          <span style={{ fontSize: 11, color: 'var(--aef-text-secondary)' }}>
            Shell installer
          </span>
        </button>
      </div>
    </div>
  );

  // ── Step 2A: Windows Setup ─────────────────────────────────────

  const renderWindowsSetup = (): JSX.Element => (
    <div>
      <p style={{ fontSize: 12, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-3)' }}>
        Download and install the Paryty Edge Agent for Windows using PowerShell.
      </p>

      {/* API Key */}
      <div style={{ marginBottom: 'var(--aef-space-3)' }}>
        <label className="dp-field__label" style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}>
          API Key
        </label>
        <input
          className="dp-field__input"
          type="text"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder="<YOUR_API_KEY>"
          style={{ width: '100%' }}
        />
      </div>

      {/* Optional Cluster Agent ID */}
      <div style={{ marginBottom: 'var(--aef-space-3)' }}>
        <label className="dp-field__label" style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}>
          Cluster Agent ID <span style={{ fontWeight: 400, color: 'var(--aef-text-secondary)' }}>(optional)</span>
        </label>
        <input
          className="dp-field__input"
          type="text"
          value={clusterAgentId}
          onChange={(e) => setClusterAgentId(e.target.value)}
          placeholder="Auto-assigned if empty"
          style={{ width: '100%' }}
        />
      </div>

      {/* PowerShell Command */}
      <div style={{ marginBottom: 'var(--aef-space-4)' }}>
        <label className="dp-field__label" style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}>
          PowerShell Command
        </label>
        <div style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--aef-space-2)',
          background: 'var(--aef-border)',
          borderRadius: 'var(--aef-radius-md)',
          padding: 'var(--aef-space-2) var(--aef-space-3)',
        }}>
          <code style={{
            fontFamily: 'monospace',
            fontSize: 11,
            flex: 1,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            lineHeight: 1.5,
          }}>
            {buildWindowsCommand()}
          </code>
          <button
            className="aef-btn aef-btn-active"
            onClick={() => handleCopy(buildWindowsCommand())}
            style={{ flexShrink: 0 }}
          >
            {copied ? <><Check size={12} /> Copied</> : <><Copy size={12} /> Copy</>}
          </button>
        </div>
      </div>

      {/* Navigation */}
      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={() => { setStep('select-os'); setTargetOS(null); }}
        >
          <ChevronLeft size={12} /> Back
        </button>
        <button
          className="aef-btn aef-btn-active"
          onClick={startVerification}
        >
          Verify Connection <ChevronRight size={12} />
        </button>
      </div>
    </div>
  );

  // ── Step 2B: Linux Setup ───────────────────────────────────────

  const renderLinuxSetup = (): JSX.Element => (
    <div>
      <p style={{ fontSize: 12, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-3)' }}>
        Run the following command on your Linux server to install and configure the Paryty Edge Agent.
      </p>

      {/* API Key */}
      <div style={{ marginBottom: 'var(--aef-space-3)' }}>
        <label className="dp-field__label" style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}>
          API Key
        </label>
        <input
          className="dp-field__input"
          type="text"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder="<YOUR_API_KEY>"
          style={{ width: '100%' }}
        />
      </div>

      {/* Optional Cluster Agent ID */}
      <div style={{ marginBottom: 'var(--aef-space-3)' }}>
        <label className="dp-field__label" style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}>
          Cluster Agent ID <span style={{ fontWeight: 400, color: 'var(--aef-text-secondary)' }}>(optional)</span>
        </label>
        <input
          className="dp-field__input"
          type="text"
          value={clusterAgentId}
          onChange={(e) => setClusterAgentId(e.target.value)}
          placeholder="Auto-assigned if empty"
          style={{ width: '100%' }}
        />
      </div>

      {/* Linux Command */}
      <div style={{ marginBottom: 'var(--aef-space-4)' }}>
        <label className="dp-field__label" style={{ marginBottom: 'var(--aef-space-1)', display: 'block' }}>
          Setup Command
        </label>
        <div style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--aef-space-2)',
          background: 'var(--aef-border)',
          borderRadius: 'var(--aef-radius-md)',
          padding: 'var(--aef-space-2) var(--aef-space-3)',
        }}>
          <code style={{
            fontFamily: 'monospace',
            fontSize: 11,
            flex: 1,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            lineHeight: 1.5,
          }}>
            {buildLinuxCommand()}
          </code>
          <button
            className="aef-btn aef-btn-active"
            onClick={() => handleCopy(buildLinuxCommand())}
            style={{ flexShrink: 0 }}
          >
            {copied ? <><Check size={12} /> Copied</> : <><Copy size={12} /> Copy</>}
          </button>
        </div>
      </div>

      {/* Navigation */}
      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={() => { setStep('select-os'); setTargetOS(null); }}
        >
          <ChevronLeft size={12} /> Back
        </button>
        <button
          className="aef-btn aef-btn-active"
          onClick={startVerification}
        >
          Verify Connection <ChevronRight size={12} />
        </button>
      </div>
    </div>
  );

  // ── Step 3: Verification ───────────────────────────────────────

  const renderVerification = (): JSX.Element => {
    const isActive = pairingStatus?.edge_status === 'active';

    return (
      <div style={{ textAlign: 'center', padding: 'var(--aef-space-4) 0' }}>
        {!isActive && (
          <div style={{ marginBottom: 'var(--aef-space-4)' }}>
            <Loader2
              size={36}
              style={{
                color: 'var(--aef-color-primary, #6366f1)',
                animation: 'spin 1s linear infinite',
              }}
            />
          </div>
        )}

        {isActive ? (
          <>
            <div style={{
              width: 48,
              height: 48,
              borderRadius: '50%',
              background: 'var(--aef-color-success, #22c55e)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              margin: '0 auto var(--aef-space-3)',
            }}>
              <Check size={24} color="#fff" />
            </div>
            <h3 style={{ fontSize: 15, fontWeight: 600, marginBottom: 'var(--aef-space-2)' }}>
              Edge Agent Connected!
            </h3>
            <p style={{ fontSize: 13, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-4)' }}>
              The edge agent is now active and paired with your cluster.
            </p>
          </>
        ) : (
          <>
            <h3 style={{ fontSize: 15, fontWeight: 600, marginBottom: 'var(--aef-space-2)' }}>
              Waiting for edge agent to connect…
            </h3>
            <p style={{ fontSize: 13, color: 'var(--aef-text-secondary)', marginBottom: 'var(--aef-space-3)' }}>
              Run the setup command on your target machine. This page will
              automatically update when the agent connects.
            </p>
            {pairingStatus && (
              <div style={{
                fontSize: 12,
                color: 'var(--aef-text-secondary)',
                background: 'var(--aef-border)',
                borderRadius: 'var(--aef-radius-md)',
                padding: 'var(--aef-space-2) var(--aef-space-3)',
                display: 'inline-block',
                marginBottom: 'var(--aef-space-3)',
              }}>
                Status: <strong>{pairingStatus.edge_status}</strong>
              </div>
            )}
          </>
        )}

        {/* Agent pairing details */}
        {pairingStatus && isActive && (
          <div style={{
            textAlign: 'left',
            background: 'var(--aef-border)',
            borderRadius: 'var(--aef-radius-md)',
            padding: 'var(--aef-space-3)',
            marginBottom: 'var(--aef-space-4)',
          }}>
            <div style={{ fontSize: 12, marginBottom: 'var(--aef-space-1)' }}>
              <strong>Hostname:</strong> {pairingStatus.hostname}
            </div>
            <div style={{ fontSize: 12, marginBottom: 'var(--aef-space-1)' }}>
              <strong>Platform:</strong> {pairingStatus.os}/{pairingStatus.arch}
            </div>
            {pairingStatus.cluster_agent_name && (
              <div style={{ fontSize: 12, marginBottom: 'var(--aef-space-1)' }}>
                <strong>Cluster Agent:</strong> {pairingStatus.cluster_agent_name}
              </div>
            )}
            {pairingStatus.paired_at && (
              <div style={{ fontSize: 12 }}>
                <strong>Paired At:</strong> {new Date(pairingStatus.paired_at).toLocaleString()}
              </div>
            )}
          </div>
        )}

        <div style={{ display: 'flex', justifyContent: 'center', gap: 'var(--aef-space-2)' }}>
          {!isActive && (
            <button
              className="aef-btn aef-btn-inactive"
              onClick={() => {
                if (pollingRef.current) {
                  clearInterval(pollingRef.current);
                  pollingRef.current = null;
                }
                setStep(targetOS === 'windows' ? 'windows-setup' : 'linux-setup');
              }}
            >
              <ChevronLeft size={12} /> Back to Setup
            </button>
          )}
          <button className="aef-btn aef-btn-active" onClick={onClose}>
            {isActive ? 'Done' : 'Close'}
          </button>
        </div>

        {/* Inline keyframe for spinner */}
        <style>{`
          @keyframes spin {
            from { transform: rotate(0deg); }
            to { transform: rotate(360deg); }
          }
        `}</style>
      </div>
    );
  };

  // ── Render current step ────────────────────────────────────────

  const renderStep = (): JSX.Element => {
    switch (step) {
      case 'select-os':
        return renderSelectOS();
      case 'windows-setup':
        return renderWindowsSetup();
      case 'linux-setup':
        return renderLinuxSetup();
      case 'verification':
        return renderVerification();
    }
  };

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
          <h3 style={{ margin: 0, fontSize: 14 }}>Edge Agent Setup</h3>
        </div>
        <div className="aef-container-card__body">
          {renderStepIndicator()}
          {renderStep()}
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
      addToast({ type: 'error', message: 'Failed to load edge agents.' });
    } finally {
      setIsLoading(false);
    }
  }, [client, addToast]);

  useEffect(() => {
    fetchAgents();
  }, [fetchAgents]);

  const handleDelete = useCallback(async (agentId: string) => {
    if (!window.confirm('Are you sure you want to unregister this edge agent? This will permanently erase all associated data and cannot be undone.')) {
      return;
    }
    try {
      await client.unregisterAgent(agentId);
      setAgents((prev) => prev.filter((a) => a.agent_id !== agentId));
      addToast({ type: 'success', message: 'Edge agent unregistered.' });
    } catch {
      addToast({ type: 'error', message: 'Failed to unregister edge agent.' });
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
            Edge Agents
          </h1>
          <p style={{ fontSize: 13, color: 'var(--aef-text-secondary)', margin: 0 }}>
            Manage your edge agents. Deploy agents to collect metrics and telemetry.
          </p>
        </div>
        <button
          className="aef-btn aef-btn-active"
          onClick={() => setShowCreateModal(true)}
        >
          <Plus size={14} /> Create Edge Agent
        </button>
      </div>

      {/* Content */}
      {isLoading ? (
        <div className="aef-container-card">
          <div className="aef-container-card__body" style={{ textAlign: 'center' }}>
            Loading edge agents…
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
          onUpdated={(updatedAgent) => {
            setAgents((prev) =>
              prev.map((a) => (a.agent_id === updatedAgent.agent_id ? updatedAgent : a))
            );
            setSelectedAgent(updatedAgent);
          }}
        />
      )}
    </div>
  );
}
