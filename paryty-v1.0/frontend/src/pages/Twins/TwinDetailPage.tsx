/**
 * TwinDetailPage — per-Digital-Paryty detail dashboard.
 *
 * Shows twin metadata header, configuration summary, assigned agents with
 * backlog management, unassigned agents, and a metrics overview.
 * Uses the twinStore for state management and the existing TwinHeader,
 * AgentList, and UnassignedAgentList components.
 *
 * @module pages/Twins/TwinDetailPage
 */

import { useEffect, useState, useCallback, memo } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { ArrowLeft, Settings, Trash2 } from 'lucide-react';
import { useTwinStore } from '../../stores/twinStore';
import { useToastStore } from '../../stores/toastStore';
import { TwinHeader } from '../../components/twins/TwinHeader';
import { AgentList } from '../../components/twins/AgentList';
import { UnassignedAgentList } from '../../components/twins/UnassignedAgentList';
import { TwinStatusBadge } from '../../components/Twin/TwinStatusBadge';
import { TwinDeleteDialog } from './TwinDeleteDialog';
import { fetchTwinMetrics } from '../../api/twins';

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Detail page for a single Digital Paryty twin.
 *
 * Fetches twin data, agents, and metrics on mount. Provides navigation
 * to edit and delete actions. Displays assigned/unassigned agent sections
 * and a metrics summary.
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

  // ─── Loading state ──────────────────────────────────────────────────

  if (isLoading && !selectedTwin) {
    return (
      <div className="dp-page" data-testid="twin-detail-page">
        <div className="dp-page__inner">
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              padding: 'var(--aef-space-12)',
              fontFamily: 'var(--aef-font-body)',
              fontSize: 12,
              color: 'var(--aef-text-secondary)',
            }}
          >
            Loading twin…
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
                <ArrowLeft size={14} /> Back to twins
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
    <div className="dp-page" data-testid="twin-detail-page">
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
              <ArrowLeft size={14} /> Back to twins
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
              Edit
            </button>
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleSettings}
              data-testid="twin-settings-btn"
            >
              <Settings size={14} /> Settings
            </button>
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={() => setShowDeleteDialog(true)}
              data-testid="twin-delete-btn"
              style={{ color: 'var(--aef-error)' }}
            >
              <Trash2 size={14} /> Delete
            </button>
          </div>
        </div>

        {/* Twin header card (reuses existing component) */}
        <TwinHeader twin={selectedTwin} />

        {/* Configuration summary */}
        <section
          className="aef-container-card"
          style={{ padding: 'var(--aef-space-4) var(--aef-space-5)' }}
          data-testid="twin-config-section"
        >
          <div
            style={{
              fontFamily: 'var(--aef-font-heading)',
              fontSize: 13,
              fontWeight: 600,
              color: 'var(--aef-text-primary)',
              marginBottom: 'var(--aef-space-3)',
            }}
          >
            Configuration
          </div>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))',
              gap: 'var(--aef-space-3)',
            }}
          >
            <div>
              <span
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 10,
                  color: 'var(--aef-text-secondary)',
                  display: 'block',
                  marginBottom: 2,
                }}
              >
                Status
              </span>
              <TwinStatusBadge status={selectedTwin.status as 'healthy' | 'degraded' | 'unhealthy' | 'unknown'} />
            </div>
            <div>
              <span
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 10,
                  color: 'var(--aef-text-secondary)',
                  display: 'block',
                  marginBottom: 2,
                }}
              >
                Agent IDs
              </span>
              <span
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 12,
                  color: 'var(--aef-text-primary)',
                }}
              >
                {selectedTwin.config.agentIds.length > 0
                  ? selectedTwin.config.agentIds.join(', ')
                  : 'Auto-assigned'}
              </span>
            </div>
            <div>
              <span
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 10,
                  color: 'var(--aef-text-secondary)',
                  display: 'block',
                  marginBottom: 2,
                }}
              >
                Tenant Label
              </span>
              <span
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 12,
                  color: 'var(--aef-text-primary)',
                }}
              >
                {selectedTwin.config.tenantLabel ?? '—'}
              </span>
            </div>
          </div>
        </section>

        {/* Metrics overview (if available) */}
        {metrics && Object.keys(metrics).length > 0 && (
          <section
            className="aef-container-card"
            style={{ padding: 'var(--aef-space-4) var(--aef-space-5)' }}
            data-testid="twin-metrics-section"
          >
            <div
              style={{
                fontFamily: 'var(--aef-font-heading)',
                fontSize: 13,
                fontWeight: 600,
                color: 'var(--aef-text-primary)',
                marginBottom: 'var(--aef-space-3)',
              }}
            >
              Metrics Overview
            </div>
            <div
              style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))',
                gap: 'var(--aef-space-3)',
              }}
            >
              {Object.entries(metrics).slice(0, 6).map(([key, value]) => (
                <div key={key}>
                  <span
                    style={{
                      fontFamily: 'var(--aef-font-body)',
                      fontSize: 10,
                      color: 'var(--aef-text-secondary)',
                      display: 'block',
                      marginBottom: 2,
                      textTransform: 'capitalize',
                    }}
                  >
                    {key.replace(/([A-Z])/g, ' $1').replace(/_/g, ' ')}
                  </span>
                  <span
                    style={{
                      fontFamily: 'var(--aef-font-mono, monospace)',
                      fontSize: 14,
                      fontWeight: 600,
                      color: 'var(--aef-text-primary)',
                    }}
                  >
                    {String(value)}
                  </span>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Assigned Agents section */}
        <section
          className="aef-container-card"
          style={{ padding: 'var(--aef-space-4) var(--aef-space-5)' }}
          data-testid="assigned-agents-section"
        >
          <div
            style={{
              fontFamily: 'var(--aef-font-heading)',
              fontSize: 13,
              fontWeight: 600,
              color: 'var(--aef-text-primary)',
              marginBottom: 'var(--aef-space-3)',
            }}
          >
            Assigned Agents
          </div>
          {isLoading ? (
            <div
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 11,
                color: 'var(--aef-text-secondary)',
              }}
            >
              Loading agents…
            </div>
          ) : (
            <AgentList
              agents={assignedAgents}
              onAcceptBacklog={handleAcceptBacklog}
              onRejectBacklog={handleRejectBacklog}
            />
          )}
        </section>

        {/* Unassigned Agents section */}
        <section
          className="aef-container-card"
          style={{ padding: 'var(--aef-space-4) var(--aef-space-5)' }}
          data-testid="unassigned-agents-section"
        >
          <div
            style={{
              fontFamily: 'var(--aef-font-heading)',
              fontSize: 13,
              fontWeight: 600,
              color: 'var(--aef-text-primary)',
              marginBottom: 'var(--aef-space-3)',
            }}
          >
            Unassigned Agents
          </div>
          {isLoading ? (
            <div
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 11,
                color: 'var(--aef-text-secondary)',
              }}
            >
              Loading agents…
            </div>
          ) : (
            <UnassignedAgentList agents={unassignedAgents} onAssign={handleAssign} />
          )}
        </section>
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
    </div>
  );
});
