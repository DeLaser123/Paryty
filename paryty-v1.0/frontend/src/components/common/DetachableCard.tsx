/**
 * DetachableCard — DS primitive that wraps an aef-container-card with
 * detach/reattach behavior. Clicking the Maximize2 icon in the header
 * detaches the card into a centered modal filling 80% of the viewport.
 * The original position collapses (siblings reorganize). Clicking
 * reattach or pressing Escape restores the card to its exact position.
 *
 * State is preserved across detach/reattach because React keeps the
 * children in the component tree — only the DOM attachment point changes
 * via createPortal.
 *
 * Accessibility:
 * - `role="dialog"` + `aria-modal="true"` on the modal container (not the overlay)
 * - `aria-labelledby` links the dialog to its rendered title
 * - Focus is moved into the modal on detach via a ref + requestAnimationFrame
 * - Tab key is trapped within the modal (focus trap cycles first ↔ last focusable)
 * - Escape key reattaches the card
 * - Body scroll is locked while any card is detached
 *
 * Parent integration:
 * - Forward a `ref` typed as `DetachableCardHandle` to call `reattach()` imperatively.
 *   Useful for coordinating scroll-to-target behavior when the target card is detached.
 *
 * @module components/common/DetachableCard
 */

import {
  useState,
  useCallback,
  useEffect,
  useRef,
  memo,
  forwardRef,
  useImperativeHandle,
  useId,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { Maximize2, Minimize2 } from 'lucide-react';

// ─── Public types ────────────────────────────────────────────────────────────

/** Imperative handle exposed by DetachableCard via `forwardRef`. */
export interface DetachableCardHandle {
  /** Programmatically reattach the card to its inline position. */
  reattach: () => void;
}

export interface DetachableCardProps {
  /** Card title displayed in the header and detached modal. */
  title: string;
  /** Icon element rendered before the title. */
  icon?: ReactNode;
  /** Optional meta pill text (e.g. "3 agents"). */
  metaLabel?: string;
  /** Optional action element rendered in the header (e.g. Assign button). */
  headerAction?: ReactNode;
  /** Card body content. */
  children: ReactNode;
  /** Optional footer content rendered below the body. */
  footer?: ReactNode;
  /** Optional className on the outer aef-container-card. */
  className?: string;
  /** data-testid on the outer card. */
  testId?: string;
}

// ─── Focus trap helper ───────────────────────────────────────────────────────

const FOCUSABLE_SELECTOR =
  'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

function trapFocus(container: HTMLElement, e: KeyboardEvent): void {
  const focusable = Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR));
  if (focusable.length === 0) return;

  const first = focusable[0];
  const last = focusable[focusable.length - 1];

  if (e.shiftKey) {
    if (document.activeElement === first) {
      e.preventDefault();
      last.focus();
    }
  } else {
    if (document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }
}

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Detachable container card — DS primitive for inline ↔ modal transitions.
 *
 * Renders a standard `aef-container-card` inline. When detached, the card
 * content is rendered via `createPortal` into a full modal overlay. State
 * is preserved because the React tree is unchanged — only the DOM position
 * of the portal output differs.
 */
export const DetachableCard = memo(
  forwardRef<DetachableCardHandle, DetachableCardProps>(function DetachableCard(
    { title, icon, metaLabel, headerAction, children, footer, className, testId },
    ref,
  ) {
    const [isDetached, setIsDetached] = useState(false);
    const modalRef = useRef<HTMLDivElement>(null);
    const titleId = useId();

    const detach = useCallback(() => setIsDetached(true), []);
    const reattach = useCallback(() => setIsDetached(false), []);

    // Expose reattach to parent via imperative handle
    useImperativeHandle(ref, () => ({ reattach }), [reattach]);

    // Focus management: move focus into modal when detached
    useEffect(() => {
      if (!isDetached) return;
      // Double-rAF ensures the portal DOM is fully committed before focusing
      const id = requestAnimationFrame(() => {
        modalRef.current?.focus();
      });
      return () => cancelAnimationFrame(id);
    }, [isDetached]);

    // Escape key to reattach + Tab focus trap
    useEffect(() => {
      if (!isDetached) return;

      const onKeyDown = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
          reattach();
          return;
        }
        if (e.key === 'Tab' && modalRef.current) {
          trapFocus(modalRef.current, e);
        }
      };

      document.addEventListener('keydown', onKeyDown);
      return () => document.removeEventListener('keydown', onKeyDown);
    }, [isDetached, reattach]);

    // Body scroll lock while detached
    useEffect(() => {
      if (!isDetached) return;
      const prev = document.body.style.overflow;
      document.body.style.overflow = 'hidden';
      return () => {
        document.body.style.overflow = prev;
      };
    }, [isDetached]);

    // ── Detached state: portal modal ──────────────────────────────────

    if (isDetached) {
      return createPortal(
        <div
          className="aef-modal-overlay"
          onClick={reattach}
        >
          <div
            ref={modalRef}
            className="aef-modal aef-modal--detached"
            onClick={(e) => e.stopPropagation()}
            role="dialog"
            aria-modal="true"
            aria-labelledby={titleId}
            tabIndex={-1}
            style={{ outline: 'none' }}
          >
            <div className="aef-modal-header">
              <div className="aef-detached-header__title-row">
                {icon && (
                  <span className="aef-detached-header__icon">{icon}</span>
                )}
                <span className="aef-modal-title" id={titleId}>
                  {title}
                </span>
                {metaLabel && (
                  <span className="aef-meta-pill">{metaLabel}</span>
                )}
                {headerAction}
              </div>
              <button
                type="button"
                className="aef-modal-close"
                onClick={reattach}
                aria-label="Reattach to page"
                data-testid="reattach-btn"
              >
                <Minimize2 size={14} />
              </button>
            </div>
            <div className="aef-modal-body">{children}</div>
            {footer && <div className="aef-modal-footer">{footer}</div>}
          </div>
        </div>,
        document.body,
      );
    }

    // ── Normal inline state ───────────────────────────────────────────

    return (
      <section
        className={`aef-container-card${className ? ` ${className}` : ''}`}
        data-testid={testId}
      >
        <div className="aef-container-card__header">
          {icon && <span className="aef-container-card__icon">{icon}</span>}
          <span className="aef-container-card__title">{title}</span>
          {metaLabel && <span className="aef-meta-pill">{metaLabel}</span>}
          {headerAction}
          <button
            type="button"
            className="aef-container-card__detach-btn"
            onClick={detach}
            aria-label={`Detach ${title}`}
            data-testid={`detach-${testId ?? title.toLowerCase().replace(/\s+/g, '-')}`}
          >
            <Maximize2 size={12} />
          </button>
        </div>
        <div className="aef-container-card__body">{children}</div>
        {footer && <div className="aef-container-card__footer">{footer}</div>}
      </section>
    );
  }),
);
