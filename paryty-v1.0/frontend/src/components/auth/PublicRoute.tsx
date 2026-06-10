/**
 * PublicRoute — redirects authenticated users to /.
 *
 * Used for login/register pages that should only be shown
 * when the user is NOT authenticated.
 *
 * @module components/auth/PublicRoute
 */

import { type ReactNode } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from './AuthProvider';

interface PublicRouteProps {
  children: ReactNode;
}

/**
 * Route wrapper for public-only pages.
 *
 * - If authenticated: redirect to / (dashboard).
 * - Otherwise: render children.
 */
export function PublicRoute({ children }: PublicRouteProps) {
  const { isAuthenticated } = useAuth();

  if (isAuthenticated) {
    return <Navigate to="/" replace />;
  }

  return <>{children}</>;
}
