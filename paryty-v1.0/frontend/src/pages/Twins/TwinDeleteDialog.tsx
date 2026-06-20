/**
 * TwinDeleteDialog — confirmation modal for deleting a Digital Paryty twin.
 *
 * Uses DS modal composition: aef-modal-overlay + aef-modal with
 * __header, __body, and __footer sections. Animated via aef-modal-enter.
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
 * Uses DS modal composition for consistent styling, animation, and accessibility.
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
      className="aef-modal-overlay"
      onClick={handleBackdropClick}
      data-testid="twin-delete-dialog"
    >
      <div
        className="aef-modal"
        role="dialog"
        aria-modal="true"
        aria-label="Confirm twin deletion"
      >
        {/* Header */}
        <div className="aef-modal-header">
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
            <AlertTriangle size={14} style={{ color: 'var(--aef-status-error)' }} />
            <span className="aef-modal-title">Delete Digital Paryty</span>
          </div>
          <button
            type="button"
            className="aef-modal-close"
            onClick={onClose}
            disabled={isDeleting}
            aria-label="Close dialog"
            data-testid="td-close-btn"
          >
            <X size={14} />
          </button>
        </div>

        {/* Body */}
        <div className="aef-modal-body">
          <p className="aef-modal-para">
            Are you sure you want to delete{' '}
            <strong style={{ color: 'var(--aef-text-primary)' }}>&ldquo;{twinName}&rdquo;</strong>?
            This action cannot be undone. All associated agent assignments
            and configuration will be permanently removed.
          </p>

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
              background: 'var(--aef-status-error)',
              color: 'var(--aef-btn-active-text)',
              border: 'none',
              opacity: isDeleting ? 'var(--aef-disabled-opacity)' : 1,
            }}
            data-testid="td-confirm-btn"
          >
            {isDeleting ? (
              'Deleting…'
            ) : (
              <>
                <Trash2 size={12} /> Delete Twin
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
});

