import { lazy, type ComponentType, type LazyExoticComponent } from 'react';

/**
 * Wraps React.lazy with automatic retry for chunk load failures.
 *
 * Addresses the #1 cause of "dynamic content loading errors":
 * transient network failures during chunk download.
 *
 * Detection: checks for Vite's "Failed to fetch dynamically imported module"
 * or generic "Loading chunk" error messages.
 */
function isChunkLoadError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  return (
    message.includes('Failed to fetch dynamically imported module') ||
    message.includes('Loading chunk') ||
    message.includes('ChunkLoadError')
  );
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function lazyWithRetry(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  factory: () => Promise<{ default: ComponentType<any> }>,
  retries = 2,
  interval = 1500,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
): LazyExoticComponent<ComponentType<any>> {
  return lazy(async () => {
    let lastError: unknown;
    for (let attempt = 0; attempt <= retries; attempt++) {
      try {
        return await factory();
      } catch (error) {
        lastError = error;
        if (isChunkLoadError(error) && attempt < retries) {
          // Wait with exponential-ish backoff before retrying
          await delay(interval * (attempt + 1));
          continue;
        }
        throw error;
      }
    }
    throw lastError;
  });
}
