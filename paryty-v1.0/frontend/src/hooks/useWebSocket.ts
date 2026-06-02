import { useEffect, useState, useRef } from 'react';
import { getWsClient } from '../api/websocket';
import type { WsState } from '../api/websocket';

export function useWebSocket() {
  const [state, setState] = useState<WsState>('disconnected');
  const wsRef = useRef(getWsClient());

  useEffect(() => {
    const ws = wsRef.current;
    const unsubscribe = ws.onStateChange(setState);
    ws.connect();
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
