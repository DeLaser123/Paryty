/**
 * TwinDetailPage — per-Digital-Paryty detail dashboard.
 *
 * Shows twin metadata header, assigned agents with backlog management,
 * and unassigned agents available for assignment.
 *
 * @module pages/TwinDetailPage
 */

import { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Settings, ArrowLeft } from 'lucide-react';
import { useTwinStore } from '../stores/twinStore';
import { useToastStore } from '../stores/toastStore';
import { TwinHeader } from '../components/twins/TwinHeader';
import { AgentList } from '../components/twins/AgentList';
import { UnassignedAgentList } from '../components/twins/UnassignedAgentList';

export function TwinDetailPage() {
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

  useEffect(() => {
    if (!id) return;

    // Fetch twin metadata and agents on mount / id change
    fetchTwin(id);
    fetchAgents(id);

    return () => {
      clearSelection();
    };
  }, [id, fetchTwin, fetchAgents, clearSelection]);

  // ─── Handlers (with toast feedback) ──────────────────────────────────

  const handleAssign = async (agentId: string): Promise<boolean> => {
    if (!id) return false;
    const ok = await assignAgent(id, agentId);
    if (ok) {
      addToast({ type: 'success', message: `Agent ${agentId} assigned.` });
    } else {
      addToast({ type: 'error', message: `Failed to assign agent ${agentId}.` });
    }
    return ok;
  };

  const handleAcceptBacklog = async (agentId: string): Promise<boolean> => {
    if (!id) return false;
    const ok = await acceptBacklog(id, agentId);
    if (ok) {
      addToast({ type: 'success', message: `Backlog accepted for agent ${agentId}.` });
    } else {
      addToast({ type: 'error', message: `Failed to accept backlog for agent ${agentId}.` });
    }
    return ok;
  };

  const handleRejectBacklog = async (agentId: string): Promise<boolean> => {
    if (!id) return false;
    const ok = await rejectBacklog(id, agentId);
    if (ok) {
      addToast({ type: 'info', message: `Backlog rejected for agent ${agentId}.` });
    } else {
      addToast({ type: 'error', message: `Failed to reject backlog for agent ${agentId}.` });
    }
    return ok;
  };

  // ─── Loading state ───────────────────────────────────────────────────

  if (isLoading && !selectedTwin) {
    return (
      <div className="dp-page" data-testid="twin-detail-page">
        <div className="dp-page__inner">
          <div style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 'var(--aef-space-12)',
            fontFamily: 'var(--aef-font-body)',
            fontSize: 12,
            color: 'var(--aef-text-secondary)',
          }}>
            Loading twin…
          </div>
        </div>
      </div>
    );
  }

  // ─── Error state ─────────────────────────────────────────────────────

  if (error || !selectedTwin) {
    return (
      <div className="dp-page" data-testid="twin-detail-page">
        <div className="dp-page__inner">
          <div className="dp-header">
            <div className="dp-header__title-block">
              <button
                className="aef-btn aef-btn-inactive"
                onClick={() => navigate('/')}
                style={{ marginBottom: 'var(--aef-space-2)' }}
              >
                <ArrowLeft size={14} /> Back to catalogue
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

  // ─── Main layout ─────────────────────────────────────────────────────

  return (
    <div className="dp-page" data-testid="twin-detail-page">
      <div className="dp-page__inner">
        {/* Page header with back + settings */}
        <div className="dp-header">
          <div className="dp-header__title-block">
            <button
              className="aef-btn aef-btn-inactive"
              onClick={() => navigate('/')}
              style={{ marginBottom: 'var(--aef-space-2)' }}
            >
              <ArrowLeft size={14} /> Back to catalogue
            </button>
            <h1 className="dp-header__title">Digital Paryty Detail</h1>
          </div>
          <div className="dp-header__actions">
            <button
              className="aef-btn aef-btn-inactive"
              onClick={() => navigate(`/twins/${id}/settings`)}
              data-testid="twin-settings-btn"
            >
              <Settings size={14} /> Settings
            </button>
          </div>
        </div>

        {/* Twin header card */}
        <TwinHeader twin={selectedTwin} />

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
            <div style={{
              fontFamily: 'var(--aef-font-body)',
              fontSize: 11,
              color: 'var(--aef-text-secondary)',
            }}>
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
            <div style={{
              fontFamily: 'var(--aef-font-body)',
              fontSize: 11,
              color: 'var(--aef-text-secondary)',
            }}>
              Loading agents…
            </div>
          ) : (
            <UnassignedAgentList agents={unassignedAgents} onAssign={handleAssign} />
          )}
        </section>
      </div>
    </div>
  );
}
