/**
 * Toast store — lightweight toast notification queue.
 *
 * Toasts auto-dismiss after a configurable duration (default 5000ms).
 * Each toast has a unique ID for tracking and removal.
 *
 * @module stores/toastStore
 */

import { create } from 'zustand';

// ─── Types ───────────────────────────────────────────────────────────

export interface Toast {
  id: string;
  type: 'success' | 'error' | 'warning' | 'info';
  message: string;
  /** Auto-dismiss duration in milliseconds. Default: 5000. */
  duration?: number;
}

/** Omit-style input type — id is auto-generated. */
export type ToastInput = Omit<Toast, 'id'>;

interface ToastState {
  /** Active toasts (newest last). */
  toasts: Toast[];
  /** Add a toast to the queue. */
  addToast: (toast: ToastInput) => void;
  /** Remove a toast by ID. */
  removeToast: (id: string) => void;
}

// ─── Helpers ─────────────────────────────────────────────────────────

let toastCounter = 0;

function nextId(): string {
  toastCounter++;
  return `toast_${Date.now()}_${toastCounter}`;
}

// ─── Store ───────────────────────────────────────────────────────────

export const useToastStore = create<ToastState>()((set, get) => ({
  toasts: [],

  addToast: (input: ToastInput) => {
    const toast: Toast = {
      ...input,
      id: nextId(),
      duration: input.duration ?? 5000,
    };

    set((s) => ({ toasts: [...s.toasts, toast] }));

    // Auto-dismiss
    const duration = toast.duration ?? 5000;
    if (duration > 0) {
      setTimeout(() => {
        const { toasts } = get();
        if (toasts.some((t) => t.id === toast.id)) {
          set((s) => ({ toasts: s.toasts.filter((t) => t.id !== toast.id) }));
        }
      }, duration);
    }
  },

  removeToast: (id: string) => {
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
  },
}));
