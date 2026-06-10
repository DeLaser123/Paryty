/**
 * Toast — toast notification container and individual toast items.
 *
 * Renders a fixed-position toast stack (top-right).
 * Each toast auto-dismisses after its configured duration.
 *
 * @module components/common/Toast
 */

import { useState, useRef, useEffect, useCallback } from 'react';
import { CheckCircle, AlertTriangle, XCircle, Info, X } from 'lucide-react';
import clsx from 'clsx';
import { useToastStore, type Toast as ToastType } from '../../stores/toastStore';

// ─── Icon resolver ────────────────────────────────────────────────────

const ICON_SIZE = 14;

function toastIcon(type: ToastType['type']) {
  switch (type) {
    case 'success':
      return <CheckCircle size={ICON_SIZE} />;
    case 'warning':
      return <AlertTriangle size={ICON_SIZE} />;
    case 'error':
      return <XCircle size={ICON_SIZE} />;
    case 'info':
      return <Info size={ICON_SIZE} />;
  }
}

// ─── Single Toast ─────────────────────────────────────────────────────

interface ToastItemProps {
  toast: ToastType;
  onDismiss: (id: string) => void;
}

function ToastItem({ toast, onDismiss }: ToastItemProps) {
  const [expanded, setExpanded] = useState(false);
  const msgRef = useRef<HTMLSpanElement>(null);
  const [overflows, setOverflows] = useState(false);

  // Detect whether the message text overflows its container
  useEffect(() => {
    const el = msgRef.current;
    if (el) {
      setOverflows(el.scrollWidth > el.clientWidth);
    }
  }, [toast.message]);

  const handleClick = useCallback(() => {
    if (overflows) setExpanded((prev) => !prev);
  }, [overflows]);

  return (
    <div
      className={clsx(
        'toast-item',
        `toast-item--${toast.type}`,
        expanded && 'toast-item--expanded',
        overflows && 'toast-item--expandable',
      )}
      role="alert"
      data-testid={`toast-${toast.type}`}
      onClick={handleClick}
    >
      <span className="toast-item__icon">{toastIcon(toast.type)}</span>
      <span
        ref={msgRef}
        className={clsx('toast-item__message', expanded && 'toast-item__message--expanded')}
      >
        {toast.message}
      </span>
      <button
        className="toast-item__close"
        onClick={(e) => { e.stopPropagation(); onDismiss(toast.id); }}
        aria-label="Dismiss notification"
        data-testid="toast-dismiss"
      >
        <X size={12} />
      </button>
    </div>
  );
}

// ─── Toast Container ──────────────────────────────────────────────────

/**
 * Fixed-position toast notification container.
 *
 * Reads from the toast store and renders each active toast.
 * Positioned at top-right of the viewport.
 */
export function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts);
  const removeToast = useToastStore((s) => s.removeToast);

  if (toasts.length === 0) return null;

  return (
    <div className="toast-container" data-testid="toast-container" aria-live="polite">
      {toasts.map((toast) => (
        <ToastItem key={toast.id} toast={toast} onDismiss={removeToast} />
      ))}
    </div>
  );
}
