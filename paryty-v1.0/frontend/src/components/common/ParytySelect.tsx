/**
 * ParytySelect — Custom animated dropdown replacing native `<select>`.
 *
 * Uses the Paryty design system `aef-dropdown` pattern with:
 * - Dropdown menu portaled to document.body for global visibility
 *   (escapes overflow: hidden, stacking contexts, and container clipping)
 * - Animated open/close (aef-dropdown-in keyframe)
 * - Click-outside and Escape key to close
 * - Selected item check mark indicator
 * - Viewport edge-flip: opens above if near bottom, aligns right if near right edge
 * - Full keyboard/ARIA support
 *
 * @module components/common/ParytySelect
 */

import { useState, useRef, useEffect, useLayoutEffect, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { ChevronDown } from 'lucide-react';
import clsx from 'clsx';
import { Tooltip } from './Tooltip';

/** A single option in the dropdown. */
export interface ParytySelectOption {
  label: string;
  value: string;
}

/** Props for ParytySelect. */
export interface ParytySelectProps {
  /** Available options. */
  options: ParytySelectOption[];
  /** Currently selected value. */
  value: string;
  /** Called with the new value string when an option is selected. */
  onChange: (value: string) => void;
  /** Placeholder text when no value matches. */
  placeholder?: string;
  /** Extra CSS class for the trigger button (e.g. sizing overrides). */
  className?: string;
  /** data-testid prefix. */
  testId?: string;
}

const GAP = 4; // px between trigger and menu
const FLIP_THRESHOLD = 40; // px from viewport edge to trigger flip

/**
 * Custom dropdown that matches the Paryty design system.
 *
 * Drop-in replacement for native `<select>` — same semantic
 * but animated, styled per the design reference, and portaled
 * to document.body for global visibility.
 */
export function ParytySelect({
  options,
  value,
  onChange,
  placeholder = 'Select…',
  className,
  testId,
}: ParytySelectProps) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ top: number; left: number; width: number }>({ top: 0, left: 0, width: 0 });
  const wrapperRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const selectedLabel =
    options.find((o) => o.value === value)?.label ?? placeholder;

  // Position the menu when it opens — measure trigger bounds
  useLayoutEffect(() => {
    if (!open || !triggerRef.current) return;
    const rect = triggerRef.current.getBoundingClientRect();
    setPos({
      top: rect.bottom + GAP,
      left: rect.left,
      width: Math.max(rect.width, 150),
    });
  }, [open]);

  // Edge-flip after menu renders: reposition if it overflows viewport
  useLayoutEffect(() => {
    if (!open || !menuRef.current) return;
    const menuRect = menuRef.current.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    setPos((prev) => {
      let { top, left } = prev;
      // Flip above if menu extends past viewport bottom
      if (menuRect.bottom > vh - FLIP_THRESHOLD && triggerRef.current) {
        const triggerTop = triggerRef.current.getBoundingClientRect().top;
        top = triggerTop - menuRect.height - GAP;
      }
      // Shift left if menu extends past viewport right
      if (menuRect.right > vw - FLIP_THRESHOLD && triggerRef.current) {
        const triggerRight = triggerRef.current.getBoundingClientRect().right;
        left = triggerRight - menuRect.width;
      }
      return { ...prev, top, left };
    });
  }, [open, pos.top, pos.left]);

  // Close on click-outside (wrapper + menu)
  useEffect(() => {
    if (!open) return;
    const handleOutside = (e: MouseEvent) => {
      const target = e.target as Node;
      const inWrapper = wrapperRef.current?.contains(target);
      const inMenu = menuRef.current?.contains(target);
      if (!inWrapper && !inMenu) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', handleOutside);
    return () => document.removeEventListener('mousedown', handleOutside);
  }, [open]);

  // Close on Escape
  useEffect(() => {
    if (!open) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [open]);

  const selectOption = useCallback(
    (opt: ParytySelectOption) => {
      onChange(opt.value);
      setOpen(false);
    },
    [onChange],
  );

  return (
    <div className="aef-dropdown" ref={wrapperRef}>
      <button
        ref={triggerRef}
        type="button"
        className={clsx('aef-dropdown-trigger', className)}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
        data-testid={testId ? `${testId}-trigger` : undefined}
      >
        <span style={{ flex: 1, textAlign: 'left', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {selectedLabel}
        </span>
        <ChevronDown
          size={12}
          className={clsx('aef-dropdown-chevron', open && 'aef-dropdown-chevron--open')}
        />
      </button>
      {open &&
        createPortal(
          <div
            ref={menuRef}
            className="aef-dropdown-menu"
            role="listbox"
            data-testid={testId ? `${testId}-menu` : undefined}
            style={{
              position: 'fixed',
              top: pos.top,
              left: pos.left,
              minWidth: pos.width,
            }}
          >
            {options.map((opt) => (
              <div
                key={opt.value}
                role="option"
                aria-selected={opt.value === value}
                className={clsx(
                  'aef-dropdown-item',
                  opt.value === value && 'aef-dropdown-item--selected',
                )}
                onClick={() => selectOption(opt)}
              >
                <Tooltip label={opt.label}>
                  <span className="aef-dropdown-item__label">{opt.label}</span>
                </Tooltip>
                {opt.value === value && (
                  <span className="aef-dropdown-check">✓</span>
                )}
              </div>
            ))}
          </div>,
          document.body,
        )}
    </div>
  );
}
