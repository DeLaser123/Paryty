/**
 * TwinDetailPage — per-Digital-Paryty detail dashboard.
 *
 * Composed from DS DetachableCard primitives (each section can detach into
 * a full-viewport modal), aef-stat-module for config fields, aef-counter
 * for metrics, aef-badge for status. Staggered entrance animations via
 * aef-panel-enter with nth-child delays.
 *
 * @module pages/Twins/TwinDetailPage
 */

import { useEffect, useState, useCallback, useRef, useMemo, memo } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { ArrowLeft, Settings, Trash2, Activity, Server, Gauge, Pencil, UserPlus } from 'lucide-react';
import { useTwinStore } from '../../stores/twinStore';
import { useToastStore } from '../../stores/toastStore';
import { TwinHeader } from '../../components/twins/TwinHeader';
import { AgentList } from '../../components/twins/AgentList';
import { UnassignedAgentList } from '../../components/twins/UnassignedAgentList';
import { TwinStatusBadge } from '../../components/Twin/TwinStatusBadge';
import { TwinDeleteDialog } from './TwinDeleteDialog';
import { AssignAgentsDialog } from './AssignAgentsDialog';
import { DetachableCard } from '../../components/common/DetachableCard';
import type { DetachableCardHandle } from '../../components/common/DetachableCard';
import { fetchTwinMetrics } from '../../api/twins';
import { mapTwinStatus } from '../../types/digitalParyty';

// ─── Stagger animation CSS (injected once) ──────────────────────────────────

const STAGGER_STYLE_ID = 'twin-detail-stagger';

function ensureStaggerStyle() {
  if (typeof document === 'undefined') return;
  if (document.getElementById(STAGGER_STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STAGGER_STYLE_ID;
  style.textContent = `
    .twin-detail-sections > .aef-container-card {
      animation: aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) both;
    }
    .twin-detail-sections > .aef-container-card:nth-child(1) { animation-delay: 0ms; }
    .twin-detail-sections > .aef-container-card:nth-child(2) { animation-delay: 80ms; }
    .twin-detail-sections > .aef-container-card:nth-child(3) { animation-delay: 160ms; }
    .twin-detail-sections > .aef-container-card:nth-child(4) { animation-delay: 240ms; }
    .twin-detail-sections > .aef-container-card:nth-child(5) { animation-delay: 320ms; }
  `;
  document.head.appendChild(style);
}

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Detail page for a single Digital Paryty twin.
 *
 * Fetches twin data, agents, and metrics on mount. Each content section
 * is a DetachableCard that can pop into an 80% viewport modal for
 * expanded detail view. Provides navigation to edit and delete actions.
 */
export const TwinDetailPage = memo(function TwinDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);

  const selectedTwin = useTwinStore((s) => s.selectedTwin);
  const assignedAgents = useTwinStore((s) => s.assignedAgents);
  const unassignedAgents = useTwinStore((s) => s.unassignedAgents);
  const isLoading = useTwinStore((s) => s.isLoading);
  const error = useTwinStore((s) => s.error);
  const fetchTwin = useTwinStore((s) => s.fetchTwin);
  const fetchAgents = useTwinStore((s) => s.fetchAgents);
  const assignAgent = useTwinStore((s) => s.assignAgent);
  const acceptBacklog = useTwinStore((s) => s.acceptBacklog);
  const rejectBacklog = useTwinStore((s) => s.rejectBacklog);
  const clearSelection = useTwinStore((s) => s.clearSelection);

  const [metrics, setMetrics] = useState<Record<string, unknown> | null>(null);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [showAssignDialog, setShowAssignDialog] = useState(false);

  // Ref for scrolling to unassigned agents section
  const unassignedRef = useRef<HTMLDivElement>(null);
  // Imperative handle to reattach the unassigned card before scrolling
  const unassignedCardRef = useRef<DetachableCardHandle>(null);

  // Inject stagger animation styles
  useEffect(() => { ensureStaggerStyle(); }, []);

  // ─── Data Fetching ──────────────────────────────────────────────────

  useEffect(() => {
    if (!id) return;

    fetchTwin(id);
    fetchAgents(id);

    // Fetch metrics (best-effort)
    fetchTwinMetrics(id)
      .then(setMetrics)
      .catch(() => {
        // Metrics endpoint may not exist yet — silently ignore
      });

    return () => {
      clearSelection();
    };
  }, [id, fetchTwin, fetchAgents, clearSelection]);

  // ─── Handlers (with toast feedback) ─────────────────────────────────

  const handleAssign = useCallback(
    async (agentId: string): Promise<boolean> => {
      if (!id) return false;
      const ok = await assignAgent(id, agentId);
      if (ok) {
        addToast({ type: 'success', message: `Agent ${agentId} assigned.` });
      } else {
        addToast({ type: 'error', message: `Failed to assign agent ${agentId}.` });
      }
      return ok;
    },
    [id, assignAgent, addToast],
  );

  const handleAcceptBacklog = useCallback(
    async (agentId: string): Promise<boolean> => {
      if (!id) return false;
      const ok = await acceptBacklog(id, agentId);
      if (ok) {
        addToast({ type: 'success', message: `Backlog accepted for agent ${agentId}.` });
      } else {
        addToast({ type: 'error', message: `Failed to accept backlog for agent ${agentId}.` });
      }
      return ok;
    },
    [id, acceptBacklog, addToast],
  );

  const handleRejectBacklog = useCallback(
    async (agentId: string): Promise<boolean> => {
      if (!id) return false;
      const ok = await rejectBacklog(id, agentId);
      if (ok) {
        addToast({ type: 'info', message: `Backlog rejected for agent ${agentId}.` });
      } else {
        addToast({ type: 'error', message: `Failed to reject backlog for agent ${agentId}.` });
      }
      return ok;
    },
    [id, rejectBacklog, addToast],
  );

  const handleEdit = useCallback(() => {
    if (id) navigate(`/twins/${id}/edit`);
  }, [id, navigate]);

  const handleSettings = useCallback(() => {
    if (id) navigate(`/twins/${id}/settings`);
  }, [id, navigate]);

  const handleBack = useCallback(() => {
    navigate('/twins');
  }, [navigate]);

  const handleDeleteSuccess = useCallback(() => {
    setShowDeleteDialog(false);
    navigate('/twins');
  }, [navigate]);

  // ─── Memoized header actions (stable refs for DetachableCard memo) ──
  // Hooks MUST be before any early returns (Rules of Hooks).

  const configHeaderAction = useMemo(
    () => selectedTwin ? <TwinStatusBadge status={mapTwinStatus(selectedTwin.status)} /> : null,
    [selectedTwin?.status],
  );

  const assignAgentBtn = useMemo(
    () => (
      <button
        type="button"
        className="aef-btn aef-btn-active dp-assign-btn"
        onClick={() => setShowAssignDialog(true)}
        data-testid="twin-assign-btn"
      >
        <UserPlus size={10} /> Assign Agent
      </button>
    ),
    [],
  );

  // ─── Loading state ──────────────────────────────────────────────────

  if (isLoading && !selectedTwin) {
    return (
      <div className="dp-page" data-testid="twin-detail-page">
        <div className="dp-page__inner">
          <div
            className="aef-viz-well"
            style={{ height: 'auto', padding: 'var(--aef-space-10)' }}
          >
            <Activity size={20} className="aef-viz-well__icon" />
            <span className="aef-viz-well__label">Loading twin…</span>
          </div>
        </div>
      </div>
    );
  }

  // ─── Error state ────────────────────────────────────────────────────

  if (error || !selectedTwin) {
    return (
      <div className="dp-page" data-testid="twin-detail-page">
        <div className="dp-page__inner">
          <div className="dp-header">
            <div className="dp-header__title-block">
              <button
                type="button"
                className="aef-btn aef-btn-inactive"
                onClick={handleBack}
                style={{ marginBottom: 'var(--aef-space-2)' }}
              >
                <ArrowLeft size={12} /> Back to twins
              </button>
              <h1 className="dp-header__title">Twin Not Found</h1>
              <p className="dp-header__sub">
                {error ?? 'The requested Digital Paryty could not be loaded.'}
              </p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // ─── Main layout ────────────────────────────────────────────────────

  return (
    <div className="dp-page dp-page--scrollable" data-testid="twin-detail-page">
      <div className="dp-page__inner">
        {/* Page header with navigation and actions */}
        <div className="dp-header">
          <div className="dp-header__title-block">
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleBack}
              style={{ marginBottom: 'var(--aef-space-2)' }}
            >
              <ArrowLeft size={12} /> Back to twins
            </button>
            <h1 className="dp-header__title">Digital Paryty Detail</h1>
          </div>
          <div className="dp-header__actions">
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleEdit}
              data-testid="twin-edit-btn"
            >
              <Pencil size={12} /> Edit
            </button>
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleSettings}
              data-testid="twin-settings-btn"
            >
              <Settings size={12} /> Settings
            </button>
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={() => setShowDeleteDialog(true)}
              data-testid="twin-delete-btn"
              style={{ color: 'var(--aef-status-error)' }}
            >
              <Trash2 size={12} /> Delete
            </button>
          </div>
        </div>

        {/* Twin header card (uses DS aef-container-card composition) */}
        <TwinHeader twin={selectedTwin} />

        {/* Content sections — each wrapped in DetachableCard for detach/reattach */}
        <div className="twin-detail-sections" style={{ display: 'flex', flexDirection: 'column', gap: 'var(--aef-space-4)' }}>

          {/* Configuration summary */}
          <DetachableCard
            title="Configuration"
            icon={<Settings size={14} />}
            headerAction={configHeaderAction}
            testId="twin-config-section"
          >
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">Agent Labels</span>
              <span className="aef-stat-module__value">
                {selectedTwin.config?.agentLabels
                  ? Object.entries(selectedTwin.config.agentLabels)
                      .map(([k, v]) => `${k}=${v}`)
                      .join(', ')
                  : 'None configured'}
              </span>
            </div>
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">Enabled Collectors</span>
              <span className="aef-stat-module__value">
                {selectedTwin.config?.enabledCollectors && selectedTwin.config.enabledCollectors.length > 0
                  ? selectedTwin.config.enabledCollectors.join(', ')
                  : 'All default'}
              </span>
            </div>
            {selectedTwin.config?.collectionIntervalSeconds != null && (
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">Collection Interval</span>
                <span className="aef-stat-module__value">{selectedTwin.config.collectionIntervalSeconds}s</span>
              </div>
            )}
            {selectedTwin.config?.samplingRate != null && (
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">Sampling Rate</span>
                <span className="aef-stat-module__value">{selectedTwin.config.samplingRate}</span>
              </div>
            )}
          </DetachableCard>

          {/* Metrics overview (if available) */}
          {metrics && Object.keys(metrics).length > 0 && (
            <DetachableCard
              title="Metrics Overview"
              icon={<Gauge size={14} />}
              metaLabel={`${Object.keys(metrics).length} metrics`}
              testId="twin-metrics-section"
            >
              <div className="dp-metrics-grid">
                {Object.entries(metrics).slice(0, 6).map(([key, value]) => (
                  <div key={key} className="aef-counter aef-counter-neutral">
                    <div className="aef-counter__body">
                      <span className="aef-counter__label">
                        {key.replace(/([A-Z])/g, ' $1').replace(/_/g, ' ')}
                      </span>
                      <span className="aef-counter__value">{String(value)}</span>
                    </div>
                  </div>
                ))}
              </div>
            </DetachableCard>
          )}

          {/* Assigned Agents section */}
          <DetachableCard
            title="Assigned Agents"
            icon={<Server size={14} />}
            metaLabel={`${assignedAgents.length} agent${assignedAgents.length !== 1 ? 's' : ''}`}
            headerAction={assignAgentBtn}
            testId="assigned-agents-section"
          >
            {isLoading ? (
              <span className="dp-loading-label">Loading agents…</span>
            ) : (
              <AgentList
                agents={assignedAgents}
                onAcceptBacklog={handleAcceptBacklog}
                onRejectBacklog={handleRejectBacklog}
              />
            )}
          </DetachableCard>

          {/* Unassigned Agents section */}
          <div ref={unassignedRef}>
            <DetachableCard
              ref={unassignedCardRef}
              title="Unassigned Agents"
              icon={<Server size={14} />}
              metaLabel={`${unassignedAgents.length} available`}
              testId="unassigned-agents-section"
            >
              {isLoading ? (
                <span className="dp-loading-label">Loading agents…</span>
              ) : (
                <UnassignedAgentList agents={unassignedAgents} onAssign={handleAssign} />
              )}
            </DetachableCard>
          </div>
        </div>
      </div>

      {/* Delete confirmation dialog */}
      {showDeleteDialog && id && (
        <TwinDeleteDialog
          twinId={id}
          twinName={selectedTwin.name}
          onClose={() => setShowDeleteDialog(false)}
          onDeleted={handleDeleteSuccess}
        />
      )}

      {/* Assign agents dialog */}
      {showAssignDialog && id && (
        <AssignAgentsDialog
          twinId={id}
          agents={unassignedAgents}
          onAssign={handleAssign}
          onClose={() => setShowAssignDialog(false)}
          onAssigned={() => {
            setShowAssignDialog(false);
            fetchAgents(id);
          }}
        />
      )}
    </div>
  );
});
