/**
 * TwinEditPage — edit an existing Digital Paryty twin's configuration.
 *
 * Loads the twin data by ID on mount and renders the TwinConfigForm
 * with pre-filled values. Supports saving updates and navigating back.
 *
 * @module pages/Twins/TwinEditPage
 */

import { useState, useEffect, useCallback, memo } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { ArrowLeft, Save } from 'lucide-react';
import { useToastStore } from '../../stores/toastStore';
import { fetchTwinById, updateTwin } from '../../api/twins';
import type { TwinDetails } from '../../types/digitalParyty';
import type { CreateTwinPayload } from '../../api/twins';
import { TwinConfigForm } from '../../components/Twin/TwinConfigForm';

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Page for editing an existing Digital Paryty twin.
 *
 * Fetches the twin by ID on mount, renders a pre-filled TwinConfigForm,
 * and sends a PUT request on submit. Navigates back to the detail page
 * on success.
 */
export const TwinEditPage = memo(function TwinEditPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);

  const [twin, setTwin] = useState<TwinDetails | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // ─── Data Fetching ──────────────────────────────────────────────────

  useEffect(() => {
    if (!id) return;

    let cancelled = false;
    setIsLoading(true);

    fetchTwinById(id)
      .then((data) => {
        if (!cancelled) {
          setTwin(data);
          setIsLoading(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          const message = err instanceof Error ? err.message : 'Failed to load twin.';
          setError(message);
          setIsLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [id]);

  // ─── Handlers ───────────────────────────────────────────────────────

  const handleSubmit = useCallback(
    async (payload: CreateTwinPayload) => {
      if (!id) return;
      setIsSubmitting(true);
      try {
        await updateTwin(id, {
          name: payload.name,
          description: payload.description,
          config: payload.config,
        });
        addToast({ type: 'success', message: 'Twin configuration updated.' });
        navigate(`/twins/${id}`);
      } catch (err) {
        const message = err instanceof Error ? err.message : 'Failed to update twin.';
        addToast({ type: 'error', message });
      } finally {
        setIsSubmitting(false);
      }
    },
    [id, addToast, navigate],
  );

  const handleBack = useCallback(() => {
    if (id) {
      navigate(`/twins/${id}`);
    } else {
      navigate('/twins');
    }
  }, [id, navigate]);

  // ─── Loading state ──────────────────────────────────────────────────

  if (isLoading) {
    return (
      <div className="dp-page" data-testid="twin-edit-page">
        <div className="dp-page__inner" style={{ maxWidth: 'var(--aef-content-width-form)' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              padding: 'var(--aef-space-12)',
              fontFamily: 'var(--aef-font-body)',
              fontSize: 'var(--aef-font-size-sm)',
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

  if (error || !twin) {
    return (
      <div className="dp-page" data-testid="twin-edit-page">
        <div className="dp-page__inner" style={{ maxWidth: 'var(--aef-content-width-form)' }}>
          <div className="dp-header">
            <div className="dp-header__title-block">
              <button
                type="button"
                className="aef-btn aef-btn-inactive"
                onClick={() => navigate('/twins')}
                style={{ marginBottom: 'var(--aef-space-2)' }}
              >
                <ArrowLeft size={12} /> Back to twins
              </button>
              <h1 className="dp-header__title">Twin Not Found</h1>
              <p className="dp-header__sub">
                {error ?? 'The requested twin could not be loaded for editing.'}
              </p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // ─── Render ─────────────────────────────────────────────────────────

  return (
    <div className="dp-page" data-testid="twin-edit-page">
      <div className="dp-page__inner" style={{ maxWidth: 'var(--aef-content-width-form)' }}>
        {/* Page header */}
        <div className="dp-header">
          <div className="dp-header__title-block">
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleBack}
              style={{ marginBottom: 'var(--aef-space-2)' }}
              data-testid="te-back-btn"
            >
              <ArrowLeft size={12} /> Back to twin
            </button>
            <h1 className="dp-header__title">Edit Digital Paryty</h1>
            <p className="dp-header__sub">
              Update configuration for &ldquo;{twin.name}&rdquo;
            </p>
          </div>
        </div>

        {/* Edit form (pre-filled with current values) */}
        <TwinConfigForm
          initialValues={{
            name: twin.name,
            description: twin.description,
            config: twin.config,
          }}
          onSubmit={handleSubmit}
          isSubmitting={isSubmitting}
          submitLabel="Save Changes"
        />

        {/* Action bar */}
        <div
          style={{
            display: 'flex',
            justifyContent: 'flex-end',
            gap: 'var(--aef-space-3)',
            marginTop: 'var(--aef-space-6)',
          }}
        >
          <button
            type="button"
            className="aef-btn aef-btn-inactive"
            onClick={handleBack}
            disabled={isSubmitting}
            data-testid="te-cancel-btn"
          >
            Cancel
          </button>
          <button
            type="submit"
            className="aef-btn aef-btn-active"
            form="twin-config-form"
            disabled={isSubmitting}
            style={{ opacity: isSubmitting ? 'var(--aef-disabled-opacity)' : 1 }}
            data-testid="te-save-btn"
          >
            {isSubmitting ? (
              'Saving…'
            ) : (
              <>
                <Save size={12} /> Save Changes
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
});
