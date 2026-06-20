/**
 * ProtectedRoute — redirects unauthenticated users to /login.
 *
 * Shows a loading spinner while auth state is initializing.
 * Supports optional role-based access control.
 *
 * @module components/auth/ProtectedRoute
 */

import { type ReactNode } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from './AuthProvider';
import { useAuthStore } from '../../stores/authStore';

interface ProtectedRouteProps {
  children: ReactNode;
  /** Optional: required role(s) for access. If not specified, any authenticated user can access. */
  requiredRoles?: string[];
}

/** Loading fallback while auth initializes. */
function AuthLoadingFallback() {
  return (
    <div style={{
      flex: 1,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      fontFamily: 'var(--aef-font-body)',
      fontSize: 12,
      color: 'var(--aef-text-secondary)',
    }}>
      Loading…
    </div>
  );
}

/** Access denied fallback for insufficient permissions. */
function AccessDeniedFallback() {
  return (
    <div style={{
      flex: 1,
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      fontFamily: 'var(--aef-font-body)',
      fontSize: 14,
      color: 'var(--aef-text-secondary)',
      gap: '8px',
    }}>
      <div style={{ fontSize: 48, opacity: 0.5 }}>🔒</div>
      <div>Access Denied</div>
      <div style={{ fontSize: 12 }}>You don't have permission to access this page.</div>
    </div>
  );
}

/**
 * Route wrapper that requires authentication.
 *
 * - If auth is loading: show spinner.
 * - If not authenticated: redirect to /login.
 * - If authenticated but insufficient role: show access denied.
 * - If authenticated: render children.
 */
export function ProtectedRoute({ children, requiredRoles }: ProtectedRouteProps) {
  const { isAuthenticated, isLoading } = useAuth();
  const user = useAuthStore((s) => s.user);

  if (isLoading) {
    return <AuthLoadingFallback />;
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  // Role-based access control
  if (requiredRoles && requiredRoles.length > 0) {
    const userRole = user?.role;
    if (!userRole || !requiredRoles.includes(userRole)) {
      return <AccessDeniedFallback />;
    }
  }

  return <>{children}</>;
}
