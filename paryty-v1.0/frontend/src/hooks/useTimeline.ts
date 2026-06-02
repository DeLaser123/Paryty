import { useEffect, useCallback } from 'react';
import { useTimelineStore } from '../stores/timelineStore';
import { SSEClient } from '../api/sse';
import type { TimelineSnapshot } from '../types/timeline';

export function useTimeline() {
  const store = useTimelineStore();

  const startStreaming = useCallback(() => {
    const url = `/api/v1/timeline/stream?start=${encodeURIComponent(store.config.startTime)}&end=${encodeURIComponent(store.config.endTime)}&speed=${store.config.speed}`;
    const sse = new SSEClient({ url });

    sse.on('snapshot', (event) => {
      try {
        const snapshot = JSON.parse(event.data) as TimelineSnapshot;
        store.appendSnapshot(snapshot);
      } catch {
        // Ignore malformed events
      }
    });

    sse.on('end', () => {
      store.setStreaming(false);
    });

    sse.connect();
    store.setStreaming(true);

    return sse;
  }, [store]);

  useEffect(() => {
    let sse: SSEClient | undefined;
    if (store.position.state === 'playing' && !store.isStreaming) {
      sse = startStreaming();
    }
    return () => sse?.close();
  }, [store.position.state, store.isStreaming, startStreaming]);

  return store;
}
