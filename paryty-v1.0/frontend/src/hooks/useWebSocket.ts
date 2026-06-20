import { useEffect, useState, useRef } from 'react';
import { getWsClient } from '../api/websocket';
import type { WsState } from '../api/websocket';

/**
 * Hook that subscribes to WebSocket connection state.
 *
 * Does NOT auto-connect — the AuthProvider manages the connection lifecycle
 * to prevent connecting before the auth token is available (which would
 * cause 403 errors on page reload).
 */
export function useWebSocket() {
  const [state, setState] = useState<WsState>('disconnected');
  const wsRef = useRef(getWsClient());

  useEffect(() => {
    const ws = wsRef.current;
    const unsubscribe = ws.onStateChange(setState);
    // Sync current state in case it changed between render and effect
    setState(ws.getState());
    return () => {
      unsubscribe();
    };
  }, []);

  return {
    state,
    isConnected: state === 'connected',
    client: wsRef.current,
  };
}
