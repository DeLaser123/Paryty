/**
 * Vitest setup — runs before each test file.
 *
 * Imports @testing-library/jest-dom matchers and sets up
 * common browser API mocks for the node environment.
 */
import '@testing-library/jest-dom/vitest';

// ─── localStorage mock for node environment ───────────────────────────

if (typeof localStorage === 'undefined') {
  const store = new Map<string, string>();
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (key: string): string | null => store.get(key) ?? null,
      setItem: (key: string, value: string): void => {
        store.set(key, value);
      },
      removeItem: (key: string): void => {
        store.delete(key);
      },
      clear: (): void => {
        store.clear();
      },
      get length(): number {
        return store.size;
      },
      key: (index: number): string | null => {
        const keys = Array.from(store.keys());
        return keys[index] ?? null;
      },
    },
    writable: true,
    configurable: true,
  });
}
