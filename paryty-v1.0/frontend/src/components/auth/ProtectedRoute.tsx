/**
 * ProtectedRoute — redirects unauthenticated users to /login.
 *
 * Shows a loading spinner while auth state is initializing.
 *
 * @module components/auth/ProtectedRoute
 */

import { type ReactNode } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from './AuthProvider';

interface ProtectedRouteProps {
  children: ReactNode;
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

/**
 * Route wrapper that requires authentication.
 *
 * - If auth is loading: show spinner.
 * - If not authenticated: redirect to /login.
 * - If authenticated: render children.
 */
export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const { isAuthenticated, isLoading } = useAuth();

  if (isLoading) {
    return <AuthLoadingFallback />;
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
}
