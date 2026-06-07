/**
 * Regression tests for the transport-degradation subscription lifecycle.
 *
 * Guards against the multi-gigabyte memory leak in which PixiTopologyApp
 * subscribed to the module-level particle store but discarded the unsubscribe
 * handle, pinning every destroyed app instance in the store's subscriber set.
 */

import { describe, it, expect } from 'vitest';
import { subscribeTransportMode } from '../../engine/transportSubscription';
import { useParticleStore } from '../../stores/particleStore';
import type { TransportMode } from '../../types/event';

describe('subscribeTransportMode', () => {
  it('invokes the consumer with each new transport mode while subscribed', () => {
    const seen: TransportMode[] = [];
    const unsub = subscribeTransportMode((mode) => seen.push(mode));

    useParticleStore.getState().setTransportMode('rest');
    useParticleStore.getState().setTransportMode('sse');
    unsub();

    expect(seen).toEqual(['rest', 'sse']);
  });

  it('stops invoking the consumer after unsubscribe (no retained subscriber)', () => {
    const seen: TransportMode[] = [];
    const unsub = subscribeTransportMode((mode) => seen.push(mode));

    useParticleStore.getState().setTransportMode('rest');
    const countWhileSubscribed = seen.length;

    unsub();
    useParticleStore.getState().setTransportMode('ws');
    useParticleStore.getState().setTransportMode('offline');

    // The leak regression: after unsubscribe, no further callbacks may fire.
    expect(seen.length).toBe(countWhileSubscribed);
  });

  it('supports many independent subscriptions that each release cleanly', () => {
    const counts = [0, 0, 0];
    const unsubs = counts.map((_, i) =>
      subscribeTransportMode(() => {
        counts[i] += 1;
      }),
    );

    useParticleStore.getState().setTransportMode('rest');
    expect(counts).toEqual([1, 1, 1]);

    // Release them one at a time; each release must stop only its own listener.
    unsubs[0]();
    useParticleStore.getState().setTransportMode('sse');
    expect(counts).toEqual([1, 2, 2]);

    unsubs[1]();
    unsubs[2]();
    useParticleStore.getState().setTransportMode('ws');
    expect(counts).toEqual([1, 2, 2]);
  });
});
