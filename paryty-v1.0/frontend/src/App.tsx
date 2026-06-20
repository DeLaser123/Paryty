/**
 * AuthenticatedLayout — wraps protected routes with AppShell chrome.
 *
 * Only rendered when the user is authenticated (enforced by ProtectedRoute).
 * Provides the full application shell: Sidebar, Header, StatusBar.
 */
function AuthenticatedLayout() {
  return (
    <ProtectedRoute>
      <AppShell>
        <Outlet />
      </AppShell>
    </ProtectedRoute>
  );
}

/**
 * App — Root application component.
 *
 * Wraps the entire application in BrowserRouter, AuthProvider,
 * ErrorBoundary, and ToastContainer.
 *
 * Public routes (/login, /register) render WITHOUT the AppShell chrome —
 * no sidebar, no header, no statusbar — just the auth page.
 *
 * Protected routes render INSIDE the AppShell chrome.
 *
 * Routes:
 * - `/`          → DashboardPage (Digital Paryty catalogue)
 * - `/topology`  → TopologyCanvas (PixiJS — only mounts on this route)
 * - `/metrics`   → MetricsView
 * - `/timeline`  → TimelineView
 * - `/alerts`    → AlertView
 * - `/intel`     → IntelView (PlanGate: requires paryty_intel)
 * - `/login`     → LoginPage (PublicRoute, no chrome)
 * - `/register`  → RegisterPage (PublicRoute, no chrome)
 * - `/settings`  → SettingsPage (ProtectedRoute)
 * - `/twins/new`  → TwinCreatePage (ProtectedRoute)
 * - `/twins/:id`  → TwinDetailPage (ProtectedRoute)
 * - `/twins/:id/settings` → TwinSettingsPage (ProtectedRoute)
 */

import { BrowserRouter as Router, Routes, Route, Outlet, Navigate } from 'react-router-dom';
import { AppShell } from './components/layout/AppShell';
import { AuthProvider } from './components/auth/AuthProvider';
import { ProtectedRoute } from './components/auth/ProtectedRoute';
import { PublicRoute } from './components/auth/PublicRoute';
import { PlanGate } from './components/auth/PlanGate';
import { ToastContainer } from './components/common/Toast';
import { LazyRoute } from './components/common/LazyRoute';
import { lazyWithRetry } from './utils/lazyWithRetry';
import { DashboardPage } from './components/dashboard/DashboardPage';

// Lazy-loaded route components with retry for chunk load failures.
const TopologyCanvas = lazyWithRetry(() =>
  import('./components/topology/TopologyCanvas').then((m) => ({ default: m.TopologyCanvas })),
);
const MetricsView = lazyWithRetry(() => import('./components/MetricsView'));
const AlertView = lazyWithRetry(() => import('./components/AlertView'));
const IntelView = lazyWithRetry(() => import('./components/intel/IntelView'));

// Lazy-loaded auth + settings pages
const LoginPage = lazyWithRetry(() => import('./pages/LoginPage').then((m) => ({ default: m.LoginPage })));
const RegisterPage = lazyWithRetry(() => import('./pages/RegisterPage').then((m) => ({ default: m.RegisterPage })));
const SettingsPage = lazyWithRetry(() => import('./pages/SettingsPage').then((m) => ({ default: m.SettingsPage })));
const TwinDetailPage = lazyWithRetry(() => import('./pages/Twins/TwinDetailPage').then((m) => ({ default: m.TwinDetailPage })));
const TwinEditPage = lazyWithRetry(() => import('./pages/Twins/TwinEditPage').then((m) => ({ default: m.TwinEditPage })));
const TwinSettingsPage = lazyWithRetry(() => import('./pages/TwinSettingsPage').then((m) => ({ default: m.TwinSettingsPage })));
const AgentsPage = lazyWithRetry(() => import('./pages/AgentsPage'));



function App() {
  return (
    <Router>
      <AuthProvider>
        <Routes>
          {/* ── Public routes (no AppShell chrome) ── */}
          <Route path="/login" element={
            <PublicRoute><LazyRoute><LoginPage /></LazyRoute></PublicRoute>
          } />
          <Route path="/register" element={
            <PublicRoute><LazyRoute><RegisterPage /></LazyRoute></PublicRoute>
          } />

          {/* ── Authenticated routes (with AppShell chrome) ── */}
          <Route element={<AuthenticatedLayout />}>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/topology" element={<LazyRoute><TopologyCanvas /></LazyRoute>} />
            <Route path="/metrics" element={<LazyRoute><MetricsView /></LazyRoute>} />
            <Route path="/alerts" element={<LazyRoute><AlertView /></LazyRoute>} />
            <Route path="/settings" element={<ProtectedRoute requiredRoles={['admin']}><LazyRoute><SettingsPage /></LazyRoute></ProtectedRoute>} />
            <Route path="/agents" element={<LazyRoute><AgentsPage /></LazyRoute>} />
            <Route path="/twins/new" element={<Navigate to="/?new=true" replace />} />
            <Route path="/twins/:id/edit" element={<LazyRoute><TwinEditPage /></LazyRoute>} />
            <Route path="/twins/:id" element={<LazyRoute><TwinDetailPage /></LazyRoute>} />
            <Route path="/twins/:id/settings" element={<ProtectedRoute requiredRoles={['admin', 'operator']}><LazyRoute><TwinSettingsPage /></LazyRoute></ProtectedRoute>} />

            {/* ── Feature-gated routes ── */}
            <Route path="/intel" element={
              <PlanGate feature="paryty_intel">
                <LazyRoute><IntelView /></LazyRoute>
              </PlanGate>
            } />
          </Route>
        </Routes>
      </AuthProvider>
      {/* Toast notifications — rendered outside all layout for correct stacking */}
      <ToastContainer />
    </Router>
  );
}

export default App;
