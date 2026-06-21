/**
 * TwinCreatePage — DEPRECATED: redirects to the consolidated dashboard wizard.
 *
 * This was a standalone creation page that has been replaced by the
 * dashboard wizard modal. Redirects to /?new=true.
 *
 * @module pages/Twins/TwinCreatePage
 */

import { Navigate } from 'react-router-dom';

export function TwinCreatePage() {
  return <Navigate to="/?new=true" replace />;
}
