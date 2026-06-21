/**
 * TwinCreatePage — DEPRECATED: redirects to the consolidated dashboard wizard.
 *
 * The creation wizard has been consolidated into DashboardPage.tsx.
 * This page redirects to /?new=true to open the wizard modal.
 *
 * @module pages/TwinCreatePage
 */

import { Navigate } from 'react-router-dom';

export function TwinCreatePage() {
  return <Navigate to="/?new=true" replace />;
}
