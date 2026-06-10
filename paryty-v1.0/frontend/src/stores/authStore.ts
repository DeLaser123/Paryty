/**
 * Auth store — manages authentication state for the Paryti SaaS platform.
 *
 * Access tokens are kept in memory only for security.
 * Refresh tokens persist in localStorage for session recovery.
 *
 * @module stores/authStore
 */

import { create } from 'zustand';
import { getRestClient } from '../api/rest';
import { usePlanStore } from './planStore';
import type {
  User,
  Tenant,
  LoginParams,
  RegisterParams,
  LoginResponse,
  RefreshResponse,
} from '../types/auth';

// ─── Storage Helpers ──────────────────────────────────────────────────

const REFRESH_STORAGE_KEY = 'paryty_refresh_token';

function getStoredRefreshToken(): string | null {
  try {
    return localStorage.getItem(REFRESH_STORAGE_KEY);
  } catch {
    return null;
  }
}

function setStoredRefreshToken(token: string): void {
  try {
    localStorage.setItem(REFRESH_STORAGE_KEY, token);
  } catch {
    // Silently fail if localStorage is unavailable
  }
}

function clearStoredRefreshToken(): void {
  try {
    localStorage.removeItem(REFRESH_STORAGE_KEY);
  } catch {
    // Silently fail
  }
}

// ─── Store Shape ─────────────────────────────────────────────────────

interface AuthState {
  /** Access token — kept in memory only, never persisted. */
  accessToken: string | null;
  /** Refresh token — persisted in localStorage for session recovery. */
  refreshToken: string | null;
  /** Currently authenticated user. */
  user: User | null;
  /** Current tenant. */
  tenant: Tenant | null;
  /** Whether the user is fully authenticated. */
  isAuthenticated: boolean;
  /** Whether auth is initializing (checking stored refresh token). */
  isLoading: boolean;

  // Actions
  login: (params: LoginParams) => Promise<void>;
  register: (params: RegisterParams) => Promise<void>;
  logout: () => Promise<void>;
  refreshAuth: () => Promise<boolean>;
  setAuth: (data: { accessToken: string; refreshToken: string; user: User; tenant: Tenant }) => void;
  clearAuth: () => void;
  /** Call on app mount to attempt silent token refresh. */
  initAuth: () => Promise<void>;
}

// ─── Store ───────────────────────────────────────────────────────────

export const useAuthStore = create<AuthState>()((set, get) => ({
  accessToken: null,
  refreshToken: null,
  user: null,
  tenant: null,
  isAuthenticated: false,
  isLoading: true,

  initAuth: async () => {
    const storedRefresh = getStoredRefreshToken();
    if (!storedRefresh) {
      set({ isLoading: false });
      return;
    }

    // Attempt silent refresh
    set({ refreshToken: storedRefresh });
    try {
      const success = await get().refreshAuth();
      if (!success) {
        clearStoredRefreshToken();
        set({ refreshToken: null, isLoading: false });
      }
    } catch {
      clearStoredRefreshToken();
      set({ refreshToken: null, isLoading: false });
    }
  },

  login: async (params: LoginParams) => {
    const client = getRestClient();
    const response = await client.post<LoginResponse>('/api/v1/auth/login', params);

    setStoredRefreshToken(response.refreshToken);

    set({
      accessToken: response.accessToken,
      refreshToken: response.refreshToken,
      user: response.user,
      tenant: response.tenant,
      isAuthenticated: true,
      isLoading: false,
    });

    // Populate plan store synchronously from the auth response (BFF pattern).
    // No extra API call — plan data is injected by the backend.
    if (response.plan) {
      usePlanStore.setState({ currentPlan: response.plan });
    }
  },

  register: async (params: RegisterParams) => {
    const client = getRestClient();
    const response = await client.post<LoginResponse>('/api/v1/auth/register', params);

    setStoredRefreshToken(response.refreshToken);

    set({
      accessToken: response.accessToken,
      refreshToken: response.refreshToken,
      user: response.user,
      tenant: response.tenant,
      isAuthenticated: true,
      isLoading: false,
    });

    // Populate plan store synchronously from the auth response (BFF pattern).
    if (response.plan) {
      usePlanStore.setState({ currentPlan: response.plan });
    }
  },

  logout: async () => {
    const { accessToken } = get();
    try {
      if (accessToken) {
        await getRestClient().post('/api/v1/auth/logout');
      }
    } catch {
      // Best-effort logout — always clear local state
    }
    clearStoredRefreshToken();
    set({
      accessToken: null,
      refreshToken: null,
      user: null,
      tenant: null,
      isAuthenticated: false,
      isLoading: false,
    });
  },

  refreshAuth: async () => {
    const { refreshToken } = get();
    if (!refreshToken) {
      return false;
    }

    try {
      const client = getRestClient();
      const response = await client.post<RefreshResponse>('/api/v1/auth/refresh', {
        refreshToken,
      });

      setStoredRefreshToken(response.refreshToken);

      set({
        accessToken: response.accessToken,
        refreshToken: response.refreshToken,
        user: response.user,
        tenant: response.tenant,
        isAuthenticated: true,
        isLoading: false,
      });

      // Fetch plan info after silent refresh.
      usePlanStore.getState().fetchCurrentPlan();
      return true;
    } catch {
      clearStoredRefreshToken();
      set({
        accessToken: null,
        refreshToken: null,
        user: null,
        tenant: null,
        isAuthenticated: false,
        isLoading: false,
      });
      return false;
    }
  },

  setAuth: (data) => {
    setStoredRefreshToken(data.refreshToken);
    set({
      accessToken: data.accessToken,
      refreshToken: data.refreshToken,
      user: data.user,
      tenant: data.tenant,
      isAuthenticated: true,
      isLoading: false,
    });
  },

  clearAuth: () => {
    clearStoredRefreshToken();
    // Clear plan state as well — nobody is authenticated.
    usePlanStore.setState({
      currentPlan: null,
      plans: [],
      error: null,
      isLoading: false,
    });
    set({
      accessToken: null,
      refreshToken: null,
      user: null,
      tenant: null,
      isAuthenticated: false,
      isLoading: false,
    });
  },
}));
