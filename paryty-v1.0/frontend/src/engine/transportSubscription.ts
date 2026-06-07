/**
 * Transport-degradation subscription lifecycle.
 *
 * Subscribes a consumer to particle-store transport-mode changes and returns
 * the unsubscribe handle. Extracted from {@link PixiTopologyApp} so the
 * subscription's teardown can be regression-tested without a WebGL context.
 *
 * REGRESSION GUARD: a prior version called `useParticleStore.subscribe(...)`
 * inline and discarded the returned unsubscribe handle. Because the Zustand
 * store is a module-level singleton that lives for the lifetime of the page,
 * the discarded subscription pinned every `PixiTopologyApp` instance (its
 * renderer, texture atlas, and pre-allocated sprite pool) in the store's
 * subscriber set forever — a multi-gigabyte leak that compounded across React
 * StrictMode double-mounts and `/topology` route navigations. Always retain
 * the handle and release it in teardown.
 *
 * @module engine/transportSubscription
 */

import { useParticleStore } from '../stores/particleStore';
import type { TransportMode } from '../types/event';

/**
 * Subscribes `onMode` to particle-store transport-mode changes.
 *
 * @param onMode - Invoked with the current transport mode on every store change.
 * @returns An unsubscribe handle. Call it during teardown to detach the
 *   listener and allow the owning object to be garbage-collected.
 */
export function subscribeTransportMode(
  onMode: (mode: TransportMode) => void,
): () => void {
  return useParticleStore.subscribe((state) => onMode(state.transportMode));
}
