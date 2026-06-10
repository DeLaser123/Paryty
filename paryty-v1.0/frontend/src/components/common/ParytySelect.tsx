/**
 * ParytySelect — Custom animated dropdown replacing native `<select>`.
 *
 * Uses the Paryty design system `aef-dropdown` pattern with:
 * - Animated open/close (aef-dropdown-in keyframe)
 * - Click-outside and Escape key to close
 * - Selected item check mark indicator
 * - Full keyboard/ARIA support
 *
 * @module components/common/ParytySelect
 */

import { useState, useRef, useEffect, useCallback } from 'react';
import { ChevronDown } from 'lucide-react';
import clsx from 'clsx';
import { useDropdownEdge } from '../../hooks/useDropdownEdge';
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

/**
 * Custom dropdown that matches the Paryty design system.
 *
 * Drop-in replacement for native `<select>` — same semantic
 * but animated and styled per the design reference.
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
  const ref = useRef<HTMLDivElement>(null);
  const { flipRight, flipUp } = useDropdownEdge(ref, open);

  const selectedLabel =
    options.find((o) => o.value === value)?.label ?? placeholder;

  // Close on click-outside
  useEffect(() => {
    if (!open) return;
    const handleOutside = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
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
    <div className="aef-dropdown" ref={ref}>
      <button
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
      {open && (
        <div
          className={clsx('aef-dropdown-menu', flipRight && 'aef-dropdown-menu--flip', flipUp && 'aef-dropdown-menu--flip-up')}
          role="listbox"
          data-testid={testId ? `${testId}-menu` : undefined}
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
        </div>
      )}
    </div>
  );
}
