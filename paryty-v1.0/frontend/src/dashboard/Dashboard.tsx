/**
 * Module: Dashboard
 *
 * Responsibility: Root dashboard component for the design system entry point.
 *   Wraps the main App with BrowserRouter for routing support.
 * Out of scope: design system showcase (handled by DesignSystemPage).
 */

import App from '../App';

/**
 * Dashboard — entry point for the main application.
 * App already includes BrowserRouter, so no additional wrapping needed.
 */
export function Dashboard() {
  return <App />;
}
