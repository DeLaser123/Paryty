/**
 * Auth store — manages authentication state for the Paryti SaaS platform.
 *
 * Access tokens are kept in memory only for security.
 * Refresh tokens are stored in httpOnly cookies (set by backend).
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
// Note: Refresh tokens are now stored in httpOnly cookies by the backend.
// No localStorage storage is needed for security.

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
    // With httpOnly cookies, we attempt silent refresh directly.
    // The refresh token cookie will be sent automatically.
    try {
      const success = await get().refreshAuth();
      if (!success) {
        set({ isLoading: false });
      }
    } catch {
      set({ isLoading: false });
    }
  },

  login: async (params: LoginParams) => {
    const client = getRestClient();
    const response = await client.post<LoginResponse>('/api/v1/auth/login', params);

    // Refresh token is now in httpOnly cookie (set by backend).
    // No need to store in localStorage.

    set({
      accessToken: response.accessToken,
      refreshToken: null, // Not stored in JS anymore
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

    // Refresh token is now in httpOnly cookie (set by backend).
    // No need to store in localStorage.

    set({
      accessToken: response.accessToken,
      refreshToken: null, // Not stored in JS anymore
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
        // Refresh token will be sent automatically via httpOnly cookie.
        await getRestClient().post('/api/v1/auth/logout', {});
      }
    } catch {
      // Best-effort logout — always clear local state
    }
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
    try {
      const client = getRestClient();
      // Refresh token is sent automatically via httpOnly cookie.
      const response = await client.post<RefreshResponse>('/api/v1/auth/refresh', {}, { skipAuth: true });

      // Refresh token is now in httpOnly cookie (set by backend).
      // No need to store in localStorage.

      set({
        accessToken: response.accessToken,
        refreshToken: null, // Not stored in JS anymore
        user: response.user,
        tenant: response.tenant,
        isAuthenticated: true,
        isLoading: false,
      });

      // Fetch plan info after silent refresh.
      usePlanStore.getState().fetchCurrentPlan();
      return true;
    } catch {
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
    // Refresh token is now in httpOnly cookie (set by backend).
    // No need to store in localStorage.
    set({
      accessToken: data.accessToken,
      refreshToken: null, // Not stored in JS anymore
      user: data.user,
      tenant: data.tenant,
      isAuthenticated: true,
      isLoading: false,
    });
  },

  clearAuth: () => {
    // Refresh token is in httpOnly cookie — backend will clear it on logout.
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
