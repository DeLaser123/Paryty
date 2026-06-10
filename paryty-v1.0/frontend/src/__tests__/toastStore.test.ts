/**
 * Tests for toastStore — toast notification queue.
 *
 * Verifies addToast, removeToast, unique IDs, and auto-dismiss
 * behavior using fake timers.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useToastStore } from '../stores/toastStore';

// ─── Helpers ──────────────────────────────────────────────────────────

function resetStore() {
  useToastStore.setState({ toasts: [] });
}

// ─── Tests ────────────────────────────────────────────────────────────

describe('toastStore', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStore();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // ── Initial State ──────────────────────────────────────────────

  describe('initial state', () => {
    it('has an empty toasts array', () => {
      const state = useToastStore.getState();
      expect(state.toasts).toEqual([]);
      expect(state.toasts).toHaveLength(0);
    });
  });

  // ── addToast ───────────────────────────────────────────────────

  describe('addToast', () => {
    it('adds a toast to the toasts array', () => {
      useToastStore.getState().addToast({
        type: 'success',
        message: 'Operation completed',
      });

      const state = useToastStore.getState();
      expect(state.toasts).toHaveLength(1);
      expect(state.toasts[0].type).toBe('success');
      expect(state.toasts[0].message).toBe('Operation completed');
      expect(state.toasts[0].duration).toBe(5000); // default
    });

    it('assigns a unique id to each toast', () => {
      const store = useToastStore.getState();

      store.addToast({ type: 'info', message: 'First' });
      store.addToast({ type: 'info', message: 'Second' });
      store.addToast({ type: 'info', message: 'Third' });

      const toasts = useToastStore.getState().toasts;
      expect(toasts).toHaveLength(3);

      const ids = toasts.map((t) => t.id);
      const uniqueIds = new Set(ids);
      expect(uniqueIds.size).toBe(3); // all unique
    });

    it('each toast id starts with "toast_"', () => {
      useToastStore.getState().addToast({
        type: 'warning',
        message: 'Warning toast',
      });

      const toast = useToastStore.getState().toasts[0];
      expect(toast.id).toMatch(/^toast_/);
    });

    it('uses custom duration when provided', () => {
      useToastStore.getState().addToast({
        type: 'error',
        message: 'Quick error',
        duration: 2000,
      });

      expect(useToastStore.getState().toasts[0].duration).toBe(2000);
    });

    it('adds multiple toasts in order (newest last)', () => {
      useToastStore.getState().addToast({ type: 'info', message: 'A' });
      useToastStore.getState().addToast({ type: 'info', message: 'B' });
      useToastStore.getState().addToast({ type: 'info', message: 'C' });

      const toasts = useToastStore.getState().toasts;
      expect(toasts[0].message).toBe('A');
      expect(toasts[1].message).toBe('B');
      expect(toasts[2].message).toBe('C');
    });
  });

  // ── removeToast ────────────────────────────────────────────────

  describe('removeToast', () => {
    it('removes a specific toast by id', () => {
      useToastStore.getState().addToast({ type: 'success', message: 'Keep' });
      useToastStore.getState().addToast({ type: 'error', message: 'Remove me' });

      const toasts = useToastStore.getState().toasts;
      expect(toasts).toHaveLength(2);

      const removeId = toasts[1].id; // the "Remove me" toast
      useToastStore.getState().removeToast(removeId);

      const remaining = useToastStore.getState().toasts;
      expect(remaining).toHaveLength(1);
      expect(remaining[0].message).toBe('Keep');
    });

    it('does nothing when removing a non-existent id', () => {
      useToastStore.getState().addToast({ type: 'info', message: 'Only one' });
      expect(useToastStore.getState().toasts).toHaveLength(1);

      useToastStore.getState().removeToast('toast_nonexistent');

      expect(useToastStore.getState().toasts).toHaveLength(1);
    });

    it('can remove all toasts one by one', () => {
      const store = useToastStore.getState();
      store.addToast({ type: 'info', message: 'One' });
      store.addToast({ type: 'info', message: 'Two' });
      store.addToast({ type: 'info', message: 'Three' });

      const ids = useToastStore.getState().toasts.map((t) => t.id);
      for (const id of ids) {
        useToastStore.getState().removeToast(id);
      }

      expect(useToastStore.getState().toasts).toHaveLength(0);
    });
  });

  // ── Auto-Dismiss ───────────────────────────────────────────────

  describe('auto-dismiss', () => {
    it('auto-removes toast after default 5000ms', () => {
      useToastStore.getState().addToast({
        type: 'success',
        message: 'Will auto-dismiss',
      });

      expect(useToastStore.getState().toasts).toHaveLength(1);

      // Advance time to just before auto-dismiss (4900ms)
      vi.advanceTimersByTime(4900);
      expect(useToastStore.getState().toasts).toHaveLength(1);

      // Advance past the 5000ms threshold
      vi.advanceTimersByTime(200);
      expect(useToastStore.getState().toasts).toHaveLength(0);
    });

    it('auto-removes toast after custom duration', () => {
      useToastStore.getState().addToast({
        type: 'warning',
        message: 'Short-lived',
        duration: 1000,
      });

      expect(useToastStore.getState().toasts).toHaveLength(1);

      vi.advanceTimersByTime(999);
      expect(useToastStore.getState().toasts).toHaveLength(1);

      vi.advanceTimersByTime(2);
      expect(useToastStore.getState().toasts).toHaveLength(0);
    });

    it('does not auto-dismiss a toast that was already manually removed', () => {
      useToastStore.getState().addToast({
        type: 'info',
        message: 'Manual remove first',
        duration: 5000,
      });

      const toastId = useToastStore.getState().toasts[0].id;

      // Manually remove
      useToastStore.getState().removeToast(toastId);
      expect(useToastStore.getState().toasts).toHaveLength(0);

      // Advance past auto-dismiss time — should not cause errors
      vi.advanceTimersByTime(6000);
      expect(useToastStore.getState().toasts).toHaveLength(0);
    });

    it('handles multiple toasts with staggered auto-dismiss', () => {
      useToastStore.getState().addToast({
        type: 'info', message: 'First', duration: 1000,
      });
      useToastStore.getState().addToast({
        type: 'info', message: 'Second', duration: 3000,
      });
      useToastStore.getState().addToast({
        type: 'info', message: 'Third', duration: 5000,
      });

      expect(useToastStore.getState().toasts).toHaveLength(3);

      // After 1500ms, only first should be gone
      vi.advanceTimersByTime(1500);
      const afterFirst = useToastStore.getState().toasts;
      expect(afterFirst).toHaveLength(2);
      expect(afterFirst[0].message).toBe('Second');
      expect(afterFirst[1].message).toBe('Third');

      // After 3500ms, second should also be gone
      vi.advanceTimersByTime(2000);
      const afterSecond = useToastStore.getState().toasts;
      expect(afterSecond).toHaveLength(1);
      expect(afterSecond[0].message).toBe('Third');

      // After 5500ms, all gone
      vi.advanceTimersByTime(2000);
      expect(useToastStore.getState().toasts).toHaveLength(0);
    });
  });
});
