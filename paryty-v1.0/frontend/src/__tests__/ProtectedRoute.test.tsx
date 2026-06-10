/**
 * @vitest-environment jsdom
 *
 * Tests for ProtectedRoute — route guard component.
 *
 * Verifies redirect behavior, loading state, and child rendering
 * based on authentication context.
 */
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

// ─── Mock useAuth ─────────────────────────────────────────────────────

const mockUseAuth = vi.fn();

vi.mock('../components/auth/AuthProvider', () => ({
  useAuth: () => mockUseAuth(),
}));

// Must mock react-router-dom's Navigate for assertion (rendered as redirect)
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    Navigate: ({ to }: { to: string }) => <div data-testid="navigate-redirect">Redirect to {to}</div>,
  };
});

// ─── Component Under Test ─────────────────────────────────────────────

import { ProtectedRoute } from '../components/auth/ProtectedRoute';

// ─── Helpers ──────────────────────────────────────────────────────────

/** Render ProtectedRoute inside a MemoryRouter */
function renderProtectedRoute(children: React.ReactNode = <div data-testid="child-content">Protected Content</div>) {
  return render(
    <MemoryRouter>
      <ProtectedRoute>{children}</ProtectedRoute>
    </MemoryRouter>,
  );
}

// ─── Tests ────────────────────────────────────────────────────────────

describe('ProtectedRoute', () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('redirects to /login when not authenticated', () => {
    mockUseAuth.mockReturnValue({ isAuthenticated: false, isLoading: false });

    renderProtectedRoute();

    expect(screen.getByTestId('navigate-redirect')).toBeDefined();
    expect(screen.getByTestId('navigate-redirect').textContent).toContain('/login');
  });

  it('shows loading spinner when auth is loading', () => {
    mockUseAuth.mockReturnValue({ isAuthenticated: false, isLoading: true });

    renderProtectedRoute();

    expect(screen.getByText('Loading…')).toBeDefined();
  });

  it('renders children when authenticated', () => {
    mockUseAuth.mockReturnValue({ isAuthenticated: true, isLoading: false });

    renderProtectedRoute();

    expect(screen.getByTestId('child-content')).toBeDefined();
    expect(screen.getByText('Protected Content')).toBeDefined();
  });

  it('does NOT render children when not authenticated', () => {
    mockUseAuth.mockReturnValue({ isAuthenticated: false, isLoading: false });

    renderProtectedRoute();

    expect(screen.queryByTestId('child-content')).toBeNull();
  });

  it('does NOT redirect when authenticated', () => {
    mockUseAuth.mockReturnValue({ isAuthenticated: true, isLoading: false });

    renderProtectedRoute();

    expect(screen.queryByTestId('navigate-redirect')).toBeNull();
  });

  it('prioritizes loading spinner over redirect when loading and not authenticated', () => {
    mockUseAuth.mockReturnValue({ isAuthenticated: false, isLoading: true });

    renderProtectedRoute();

    // Should show spinner, not redirect
    expect(screen.getByText('Loading…')).toBeDefined();
    expect(screen.queryByTestId('navigate-redirect')).toBeNull();
  });
});
