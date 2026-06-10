/**
 * Tests for authStore — authentication state management.
 *
 * Verifies login, register, logout, refresh, setAuth, clearAuth,
 * and initAuth flows. Mocks the RestClient and localStorage.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAuthStore } from '../stores/authStore';

// ─── Mock Factories ───────────────────────────────────────────────────

const mockUser = {
  id: 'u1',
  email: 'test@example.com',
  name: 'Test User',
  role: 'admin' as const,
  tenantId: 't1',
  permissions: { read: true, write: true },
  createdAt: '2025-01-01T00:00:00Z',
};

const mockTenant = {
  id: 't1',
  name: 'Test Corp',
  planName: 'Pro',
  createdAt: '2025-01-01T00:00:00Z',
};

const mockLoginResponse = {
  accessToken: 'at_abc123',
  refreshToken: 'rt_xyz789',
  expiresAt: '2025-01-02T00:00:00Z',
  user: mockUser,
  tenant: mockTenant,
};

// ─── Mock RestClient ──────────────────────────────────────────────────

vi.mock('../api/rest', () => {
  const mockPost = vi.fn();
  const mockClient = {
    post: mockPost,
    get: vi.fn(),
    setTokenGetter: vi.fn(),
    setAuthRefreshCallback: vi.fn(),
  };
  return {
    getRestClient: vi.fn(() => mockClient),
    RestClient: vi.fn(() => mockClient),
    ApiClientError: class extends Error {
      status: number;
      code?: string;
      constructor(message: string, status: number, code?: string) {
        super(message);
        this.status = status;
        this.code = code;
        this.name = 'ApiClientError';
      }
    },
  };
});

// ─── Helpers ──────────────────────────────────────────────────────────

/** Reset the store to its initial state before each test. */
function resetStore() {
  useAuthStore.setState({
    accessToken: null,
    refreshToken: null,
    user: null,
    tenant: null,
    isAuthenticated: false,
    isLoading: true,
  });
}

// ─── Tests ────────────────────────────────────────────────────────────

describe('authStore', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    resetStore();
  });

  // ── Initial State ──────────────────────────────────────────────

  describe('initial state', () => {
    it('has isAuthenticated=false, isLoading=true, user=null, tenant=null', () => {
      const state = useAuthStore.getState();
      expect(state.isAuthenticated).toBe(false);
      expect(state.isLoading).toBe(true);
      expect(state.user).toBeNull();
      expect(state.tenant).toBeNull();
    });

    it('has null accessToken and refreshToken', () => {
      const state = useAuthStore.getState();
      expect(state.accessToken).toBeNull();
      expect(state.refreshToken).toBeNull();
    });
  });

  // ── Login ──────────────────────────────────────────────────────

  describe('login', () => {
    it('sets isAuthenticated=true and populates user on success', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockResolvedValueOnce(mockLoginResponse);

      await useAuthStore.getState().login({
        email: 'test@example.com',
        password: 'secret123',
      });

      const state = useAuthStore.getState();
      expect(state.isAuthenticated).toBe(true);
      expect(state.user).toEqual(mockUser);
      expect(state.tenant).toEqual(mockTenant);
      expect(state.accessToken).toBe('at_abc123');
      expect(state.refreshToken).toBe('rt_xyz789');
      expect(state.isLoading).toBe(false);
    });

    it('persists refresh token to localStorage', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockResolvedValueOnce(mockLoginResponse);

      await useAuthStore.getState().login({
        email: 'test@example.com',
        password: 'secret123',
      });

      expect(localStorage.getItem('paryty_refresh_token')).toBe('rt_xyz789');
    });

    it('calls POST /api/v1/auth/login with correct params', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockResolvedValueOnce(mockLoginResponse);

      await useAuthStore.getState().login({
        email: 'test@example.com',
        password: 'secret123',
      });

      expect(mockPost).toHaveBeenCalledWith('/api/v1/auth/login', {
        email: 'test@example.com',
        password: 'secret123',
      });
    });

    it('propagates errors on failed login', async () => {
      const { getRestClient, ApiClientError } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockRejectedValueOnce(new ApiClientError('Unauthorized', 401));

      await expect(
        useAuthStore.getState().login({
          email: 'test@example.com',
          password: 'wrong',
        }),
      ).rejects.toThrow('Unauthorized');
    });
  });

  // ── Register ───────────────────────────────────────────────────

  describe('register', () => {
    it('sets isAuthenticated=true and populates user on success', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockResolvedValueOnce(mockLoginResponse);

      await useAuthStore.getState().register({
        email: 'new@example.com',
        password: 'secret123',
        name: 'New User',
        tenantName: 'New Corp',
        planName: 'Basic',
      });

      const state = useAuthStore.getState();
      expect(state.isAuthenticated).toBe(true);
      expect(state.user).toEqual(mockUser);
      expect(state.tenant).toEqual(mockTenant);
    });

    it('calls POST /api/v1/auth/register with correct params', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockResolvedValueOnce(mockLoginResponse);

      const registerParams = {
        email: 'new@example.com',
        password: 'secret123',
        name: 'New User',
        tenantName: 'New Corp',
        planName: 'Basic',
      };

      await useAuthStore.getState().register(registerParams);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/auth/register', registerParams);
    });
  });

  // ── Logout ─────────────────────────────────────────────────────

  describe('logout', () => {
    it('clears auth state', async () => {
      // Prime authenticated state
      useAuthStore.setState({
        accessToken: 'at_abc',
        refreshToken: 'rt_xyz',
        user: mockUser,
        tenant: mockTenant,
        isAuthenticated: true,
        isLoading: false,
      });

      await useAuthStore.getState().logout();

      const state = useAuthStore.getState();
      expect(state.isAuthenticated).toBe(false);
      expect(state.user).toBeNull();
      expect(state.tenant).toBeNull();
      expect(state.accessToken).toBeNull();
      expect(state.refreshToken).toBeNull();
    });

    it('removes refresh token from localStorage', async () => {
      localStorage.setItem('paryty_refresh_token', 'rt_xyz');
      useAuthStore.setState({
        accessToken: 'at_abc',
        refreshToken: 'rt_xyz',
        user: mockUser,
        tenant: mockTenant,
        isAuthenticated: true,
        isLoading: false,
      });

      await useAuthStore.getState().logout();

      expect(localStorage.getItem('paryty_refresh_token')).toBeNull();
    });

    it('calls POST /api/v1/auth/logout when accessToken exists', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockResolvedValueOnce(undefined);
      useAuthStore.setState({
        accessToken: 'at_abc',
        refreshToken: 'rt_xyz',
        user: mockUser,
        tenant: mockTenant,
        isAuthenticated: true,
        isLoading: false,
      });

      await useAuthStore.getState().logout();

      expect(mockPost).toHaveBeenCalledWith('/api/v1/auth/logout');
    });

    it('clears state even when logout API call fails', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockRejectedValueOnce(new Error('Network error'));
      useAuthStore.setState({
        accessToken: 'at_abc',
        refreshToken: 'rt_xyz',
        user: mockUser,
        tenant: mockTenant,
        isAuthenticated: true,
        isLoading: false,
      });

      await useAuthStore.getState().logout();

      const state = useAuthStore.getState();
      expect(state.isAuthenticated).toBe(false);
      expect(state.accessToken).toBeNull();
    });
  });

  // ── Refresh ────────────────────────────────────────────────────

  describe('refreshAuth', () => {
    const mockRefreshResponse = {
      accessToken: 'at_new456',
      refreshToken: 'rt_new789',
      expiresAt: '2025-01-03T00:00:00Z',
    };

    it('returns false when no refreshToken is available', async () => {
      const result = await useAuthStore.getState().refreshAuth();
      expect(result).toBe(false);
    });

    it('returns true and updates tokens on successful refresh', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockResolvedValueOnce(mockRefreshResponse);

      useAuthStore.setState({ refreshToken: 'rt_xyz' });

      const result = await useAuthStore.getState().refreshAuth();

      expect(result).toBe(true);
      const state = useAuthStore.getState();
      expect(state.accessToken).toBe('at_new456');
      expect(state.refreshToken).toBe('rt_new789');
      expect(state.isAuthenticated).toBe(true);
    });

    it('returns false and clears state on refresh failure', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockRejectedValueOnce(new Error('Invalid token'));

      useAuthStore.setState({
        refreshToken: 'rt_expired',
        user: mockUser,
        tenant: mockTenant,
      });

      const result = await useAuthStore.getState().refreshAuth();

      expect(result).toBe(false);
      const state = useAuthStore.getState();
      expect(state.accessToken).toBeNull();
      expect(state.refreshToken).toBeNull();
      expect(state.isAuthenticated).toBe(false);
      expect(state.user).toBeNull();
    });

    it('clears stored refresh token on failure', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockRejectedValueOnce(new Error('Invalid token'));

      localStorage.setItem('paryty_refresh_token', 'rt_expired');
      useAuthStore.setState({ refreshToken: 'rt_expired' });

      await useAuthStore.getState().refreshAuth();

      expect(localStorage.getItem('paryty_refresh_token')).toBeNull();
    });
  });

  // ── setAuth ────────────────────────────────────────────────────

  describe('setAuth', () => {
    it('sets all auth fields and marks authenticated', () => {
      useAuthStore.getState().setAuth({
        accessToken: 'at_direct',
        refreshToken: 'rt_direct',
        user: mockUser,
        tenant: mockTenant,
      });

      const state = useAuthStore.getState();
      expect(state.accessToken).toBe('at_direct');
      expect(state.refreshToken).toBe('rt_direct');
      expect(state.user).toEqual(mockUser);
      expect(state.tenant).toEqual(mockTenant);
      expect(state.isAuthenticated).toBe(true);
      expect(state.isLoading).toBe(false);
    });

    it('persists refresh token to localStorage', () => {
      useAuthStore.getState().setAuth({
        accessToken: 'at_direct',
        refreshToken: 'rt_direct',
        user: mockUser,
        tenant: mockTenant,
      });

      expect(localStorage.getItem('paryty_refresh_token')).toBe('rt_direct');
    });
  });

  // ── clearAuth ──────────────────────────────────────────────────

  describe('clearAuth', () => {
    it('resets all auth fields to null/false', () => {
      // Prime with authenticated state
      useAuthStore.setState({
        accessToken: 'at_abc',
        refreshToken: 'rt_xyz',
        user: mockUser,
        tenant: mockTenant,
        isAuthenticated: true,
        isLoading: false,
      });

      useAuthStore.getState().clearAuth();

      const state = useAuthStore.getState();
      expect(state.accessToken).toBeNull();
      expect(state.refreshToken).toBeNull();
      expect(state.user).toBeNull();
      expect(state.tenant).toBeNull();
      expect(state.isAuthenticated).toBe(false);
    });

    it('removes refresh token from localStorage', () => {
      localStorage.setItem('paryty_refresh_token', 'rt_xyz');
      useAuthStore.getState().clearAuth();
      expect(localStorage.getItem('paryty_refresh_token')).toBeNull();
    });
  });

  // ── initAuth ───────────────────────────────────────────────────

  describe('initAuth', () => {
    it('sets isLoading=false when no stored refresh token exists', async () => {
      await useAuthStore.getState().initAuth();
      const state = useAuthStore.getState();
      expect(state.isLoading).toBe(false);
      expect(state.isAuthenticated).toBe(false);
    });

    it('attempts refresh and sets isLoading=false on success', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockResolvedValueOnce({
        accessToken: 'at_silent',
        refreshToken: 'rt_silent',
        expiresAt: '2025-01-03T00:00:00Z',
      });

      localStorage.setItem('paryty_refresh_token', 'rt_stored');

      await useAuthStore.getState().initAuth();

      const state = useAuthStore.getState();
      expect(state.isLoading).toBe(false);
      expect(state.isAuthenticated).toBe(true);
      expect(state.accessToken).toBe('at_silent');
    });

    it('clears stored token and sets isLoading=false on refresh failure', async () => {
      const { getRestClient } = await import('../api/rest');
      const mockPost = getRestClient().post as ReturnType<typeof vi.fn>;
      mockPost.mockRejectedValueOnce(new Error('Expired'));

      localStorage.setItem('paryty_refresh_token', 'rt_expired');

      await useAuthStore.getState().initAuth();

      const state = useAuthStore.getState();
      expect(state.isLoading).toBe(false);
      expect(state.isAuthenticated).toBe(false);
      expect(localStorage.getItem('paryty_refresh_token')).toBeNull();
    });
  });
});
