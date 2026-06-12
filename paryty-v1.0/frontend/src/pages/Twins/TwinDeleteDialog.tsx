/**
 * TwinDeleteDialog — confirmation modal for deleting a Digital Paryty twin.
 *
 * Renders a centered overlay dialog with a warning message, twin name,
 * and cancel/confirm buttons. Calls the twins API to delete on confirm.
 *
 * @module pages/Twins/TwinDeleteDialog
 */

import { useState, useCallback, useEffect, memo } from 'react';
import { AlertTriangle, X, Trash2 } from 'lucide-react';
import { useToastStore } from '../../stores/toastStore';
import { deleteTwin } from '../../api/twins';

// ─── Types ──────────────────────────────────────────────────────────────────

interface TwinDeleteDialogProps {
  /** The ID of the twin to delete. */
  twinId: string;
  /** The display name of the twin (for the confirmation message). */
  twinName: string;
  /** Called when the dialog should close without action. */
  onClose: () => void;
  /** Called after successful deletion. */
  onDeleted: () => void;
}

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Modal dialog for confirming twin deletion.
 *
 * Displays a warning with the twin name, requires explicit confirm action,
 * and handles the delete API call with loading/error states. Closes on
 * Escape key or clicking the backdrop.
 */
export const TwinDeleteDialog = memo(function TwinDeleteDialog({
  twinId,
  twinName,
  onClose,
  onDeleted,
}: TwinDeleteDialogProps) {
  const addToast = useToastStore((s) => s.addToast);

  const [isDeleting, setIsDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // ─── Keyboard handling ──────────────────────────────────────────────

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !isDeleting) {
        onClose();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isDeleting, onClose]);

  // ─── Handlers ───────────────────────────────────────────────────────

  const handleConfirm = useCallback(async () => {
    setIsDeleting(true);
    setError(null);

    try {
      await deleteTwin(twinId);
      addToast({
        type: 'success',
        message: `Digital Paryty "${twinName}" deleted.`,
      });
      onDeleted();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to delete twin.';
      setError(message);
      addToast({ type: 'error', message });
    } finally {
      setIsDeleting(false);
    }
  }, [twinId, twinName, addToast, onDeleted]);

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (e.target === e.currentTarget && !isDeleting) {
        onClose();
      }
    },
    [isDeleting, onClose],
  );

  // ─── Render ─────────────────────────────────────────────────────────

  return (
    <div
      className="twin-delete-dialog-backdrop"
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 1000,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'rgba(0, 0, 0, 0.5)',
        backdropFilter: 'blur(2px)',
      }}
      onClick={handleBackdropClick}
      role="dialog"
      aria-modal="true"
      aria-label="Confirm twin deletion"
      data-testid="twin-delete-dialog"
    >
      <div
        className="aef-container-card"
        style={{
          width: '100%',
          maxWidth: 420,
          margin: 'var(--aef-space-4)',
          padding: 0,
          overflow: 'hidden',
        }}
      >
        {/* Header */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: 'var(--aef-space-4) var(--aef-space-5)',
            borderBottom: 'var(--aef-border-width) solid var(--aef-border)',
          }}
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--aef-space-2)',
            }}
          >
            <AlertTriangle
              size={16}
              style={{ color: 'var(--aef-error)' }}
            />
            <h2
              style={{
                margin: 0,
                fontFamily: 'var(--aef-font-heading)',
                fontSize: 15,
                fontWeight: 600,
                color: 'var(--aef-text-primary)',
              }}
            >
              Delete Digital Paryty
            </h2>
          </div>
          <button
            type="button"
            className="aef-btn aef-btn-inactive"
            onClick={onClose}
            disabled={isDeleting}
            aria-label="Close dialog"
            style={{ padding: 'var(--aef-space-1) var(--aef-space-2)' }}
            data-testid="td-close-btn"
          >
            <X size={14} />
          </button>
        </div>

        {/* Body */}
        <div style={{ padding: 'var(--aef-space-4) var(--aef-space-5)' }}>
          <p
            style={{
              margin: 0,
              fontFamily: 'var(--aef-font-body)',
              fontSize: 12,
              color: 'var(--aef-text-primary)',
              lineHeight: 1.6,
              marginBottom: 'var(--aef-space-3)',
            }}
          >
            Are you sure you want to delete{' '}
            <strong>&ldquo;{twinName}&rdquo;</strong>?
            This action cannot be undone. All associated agent assignments
            and configuration will be permanently removed.
          </p>

          {/* Error message */}
          {error && (
            <div
              style={{
                padding: 'var(--aef-space-2) var(--aef-space-3)',
                background: 'var(--aef-surface-low)',
                border: 'var(--aef-border-width) solid var(--aef-error)',
                borderRadius: 'var(--aef-radius-control)',
                marginBottom: 'var(--aef-space-3)',
              }}
            >
              <span
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 11,
                  color: 'var(--aef-error)',
                }}
              >
                {error}
              </span>
            </div>
          )}
        </div>

        {/* Footer */}
        <div
          style={{
            display: 'flex',
            justifyContent: 'flex-end',
            gap: 'var(--aef-space-3)',
            padding: 'var(--aef-space-3) var(--aef-space-5)',
            borderTop: 'var(--aef-border-width) solid var(--aef-border)',
            background: 'var(--aef-surface-low)',
          }}
        >
          <button
            type="button"
            className="aef-btn aef-btn-inactive"
            onClick={onClose}
            disabled={isDeleting}
            data-testid="td-cancel-btn"
          >
            Cancel
          </button>
          <button
            type="button"
            className="aef-btn"
            onClick={handleConfirm}
            disabled={isDeleting}
            style={{
              background: 'var(--aef-error)',
              color: '#fff',
              border: 'none',
              opacity: isDeleting ? 0.6 : 1,
            }}
            data-testid="td-confirm-btn"
          >
            {isDeleting ? (
              'Deleting…'
            ) : (
              <>
                <Trash2 size={14} /> Delete Twin
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
});

