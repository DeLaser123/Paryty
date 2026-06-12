/**
 * TwinCreatePage — form page for creating a new Digital Paryty twin.
 *
 * Uses the TwinConfigForm component for the form fields and the twins API
 * for the creation request. Navigates to the twin detail page on success.
 *
 * @module pages/Twins/TwinCreatePage
 */

import { useState, useCallback, memo } from 'react';
import { useNavigate } from 'react-router-dom';
import { ArrowLeft, Plus } from 'lucide-react';
import { useToastStore } from '../../stores/toastStore';
import { createTwin } from '../../api/twins';
import type { CreateTwinPayload } from '../../api/twins';
import { TwinConfigForm } from '../../components/Twin/TwinConfigForm';

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Page for creating a new Digital Paryty twin.
 *
 * Renders a back button, page header, and the TwinConfigForm.
 * On successful creation, navigates to the twin detail page.
 */
export const TwinCreatePage = memo(function TwinCreatePage() {
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);

  const [isSubmitting, setIsSubmitting] = useState(false);

  // ─── Handlers ───────────────────────────────────────────────────────

  const handleSubmit = useCallback(
    async (payload: CreateTwinPayload) => {
      setIsSubmitting(true);
      try {
        const twin = await createTwin(payload);
        addToast({
          type: 'success',
          message: `Digital Paryty "${twin.name}" created!`,
        });
        navigate(`/twins/${twin.id}`);
      } catch (err) {
        const message = err instanceof Error ? err.message : 'Failed to create Digital Paryty.';
        addToast({ type: 'error', message });
      } finally {
        setIsSubmitting(false);
      }
    },
    [addToast, navigate],
  );

  const handleBack = useCallback(() => {
    navigate('/twins');
  }, [navigate]);

  // ─── Render ─────────────────────────────────────────────────────────

  return (
    <div className="dp-page" data-testid="twin-create-page">
      <div className="dp-page__inner" style={{ maxWidth: 600 }}>
        {/* Page header */}
        <div className="dp-header">
          <div className="dp-header__title-block">
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleBack}
              style={{ marginBottom: 'var(--aef-space-2)' }}
              data-testid="tc-back-btn"
            >
              <ArrowLeft size={14} /> Back to twins
            </button>
            <h1 className="dp-header__title">New Digital Paryty</h1>
            <p className="dp-header__sub">
              Configure and create a new digital twin for your software system.
            </p>
          </div>
        </div>

        {/* Info banner */}
        <div
          className="aef-container-card"
          style={{
            padding: 'var(--aef-space-3) var(--aef-space-4)',
            marginBottom: 'var(--aef-space-4)',
            background: 'var(--aef-surface-low)',
          }}
        >
          <p
            style={{
              margin: 0,
              fontFamily: 'var(--aef-font-body)',
              fontSize: 11,
              color: 'var(--aef-text-secondary)',
              lineHeight: 1.6,
            }}
          >
            A Digital Paryty is a live-telemetry-driven digital twin of your software system.
            It aggregates telemetry from connected agents to provide real-time topology observation,
            forecasting, and anomaly detection.
          </p>
        </div>

        {/* Creation form */}
        <TwinConfigForm
          onSubmit={handleSubmit}
          isSubmitting={isSubmitting}
          submitLabel="Create Twin"
        />

        {/* Submit shortcut (also accessible via form submit) */}
        <div
          style={{
            display: 'flex',
            justifyContent: 'flex-end',
            marginTop: 'var(--aef-space-6)',
          }}
        >
          <button
            type="submit"
            className="aef-btn aef-btn-active"
            form="twin-config-form"
            disabled={isSubmitting}
            style={{ opacity: isSubmitting ? 0.4 : 1 }}
            data-testid="tc-create-submit"
          >
            {isSubmitting ? (
              'Creating…'
            ) : (
              <>
                <Plus size={14} /> Create Twin
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
});
