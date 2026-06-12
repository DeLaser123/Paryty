/**
 * AuthProvider — React context provider for authentication state.
 *
 * On mount, checks for a stored refresh token and attempts silent refresh.
 * Exposes auth state via useAuth() hook.
 *
 * @module components/auth/AuthProvider
 */

import { createContext, useContext, useEffect, type ReactNode } from 'react';
import { useAuthStore } from '../../stores/authStore';
import { getRestClient } from '../../api/rest';
import { getWsClient } from '../../api/websocket';
import { setSseTokenGetter } from '../../api/sse';

// ─── Context ──────────────────────────────────────────────────────────

interface AuthContextValue {
  isAuthenticated: boolean;
  isLoading: boolean;
}

const AuthContext = createContext<AuthContextValue>({
  isAuthenticated: false,
  isLoading: true,
});

// ─── Props ────────────────────────────────────────────────────────────

interface AuthProviderProps {
  children: ReactNode;
}

// ─── Component ────────────────────────────────────────────────────────

/**
 * Wraps the application with authentication context.
 *
 * On mount:
 * 1. Wires up the RestClient token getter and refresh callback.
 * 2. Wires up the WebSocket token getter.
 * 3. Attempts silent token refresh if a stored refresh token exists.
 */
export function AuthProvider({ children }: AuthProviderProps) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const isLoading = useAuthStore((s) => s.isLoading);
  const accessToken = useAuthStore((s) => s.accessToken);
  const initAuth = useAuthStore((s) => s.initAuth);

  // Wire auth into RestClient and WebSocket
  useEffect(() => {
    const client = getRestClient();
    client.setTokenGetter(() => useAuthStore.getState().accessToken);
    client.setAuthRefreshCallback(async () => {
      return await useAuthStore.getState().refreshAuth();
    });

    const ws = getWsClient();
    ws.setTokenGetter(() => useAuthStore.getState().accessToken);

    // SSE clients read the token through a module-level getter because
    // EventSource cannot set request headers.
    setSseTokenGetter(() => useAuthStore.getState().accessToken);

    return () => {
      client.setTokenGetter(null);
      client.setAuthRefreshCallback(null);
      ws.setTokenGetter(null);
      setSseTokenGetter(null);
    };
  }, []);

  // Attempt silent refresh on mount
  useEffect(() => {
    initAuth();
  }, [initAuth]);

  // Reconnect WebSocket when token changes after auth
  useEffect(() => {
    if (accessToken) {
      const ws = getWsClient();
      ws.disconnect();
      ws.connect();
    }
  }, [accessToken]);

  return (
    <AuthContext.Provider value={{ isAuthenticated, isLoading }}>
      {children}
    </AuthContext.Provider>
  );
}

// ─── Hook ─────────────────────────────────────────────────────────────

/**
 * Access the current authentication state.
 *
 * @returns Auth state (isAuthenticated, isLoading).
 */
export function useAuth(): AuthContextValue {
  return useContext(AuthContext);
}
