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

import { BrowserRouter as Router, Routes, Route, Outlet } from 'react-router-dom';
import { Suspense, lazy } from 'react';
import { AppShell } from './components/layout/AppShell';
import { ErrorBoundary } from './components/common/ErrorBoundary';
import { AuthProvider } from './components/auth/AuthProvider';
import { ProtectedRoute } from './components/auth/ProtectedRoute';
import { PublicRoute } from './components/auth/PublicRoute';
import { PlanGate } from './components/auth/PlanGate';
import { ToastContainer } from './components/common/Toast';
import { DashboardPage } from './components/dashboard/DashboardPage';

// Lazy-loaded route components for code splitting
const TopologyCanvas = lazy(() =>
  import('./components/topology/TopologyCanvas').then((m) => ({ default: m.TopologyCanvas })),
);
const MetricsView = lazy(() => import('./components/MetricsView'));
const AlertView = lazy(() => import('./components/AlertView'));
const IntelView = lazy(() => import('./components/intel/IntelView'));

// Lazy-loaded auth + settings pages
const LoginPage = lazy(() => import('./pages/LoginPage').then((m) => ({ default: m.LoginPage })));
const RegisterPage = lazy(() => import('./pages/RegisterPage').then((m) => ({ default: m.RegisterPage })));
const SettingsPage = lazy(() => import('./pages/SettingsPage').then((m) => ({ default: m.SettingsPage })));
const TwinCreatePage = lazy(() => import('./pages/TwinCreatePage').then((m) => ({ default: m.TwinCreatePage })));
const TwinDetailPage = lazy(() => import('./pages/TwinDetailPage').then((m) => ({ default: m.TwinDetailPage })));
const TwinSettingsPage = lazy(() => import('./pages/TwinSettingsPage').then((m) => ({ default: m.TwinSettingsPage })));

/** Loading fallback for Suspense boundaries. */
function LoadingFallback() {
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

function App() {
  return (
    <Router>
      <AuthProvider>
        <ErrorBoundary>
          <Suspense fallback={<LoadingFallback />}>
            <Routes>
              {/* ── Public routes (no AppShell chrome) ── */}
              <Route path="/login" element={
                <PublicRoute><LoginPage /></PublicRoute>
              } />
              <Route path="/register" element={
                <PublicRoute><RegisterPage /></PublicRoute>
              } />

              {/* ── Authenticated routes (with AppShell chrome) ── */}
              <Route element={<AuthenticatedLayout />}>
                <Route path="/" element={<DashboardPage />} />
                <Route path="/topology" element={<TopologyCanvas />} />
                <Route path="/metrics" element={<MetricsView />} />
                <Route path="/alerts" element={<AlertView />} />
                <Route path="/settings" element={<SettingsPage />} />
                <Route path="/twins/new" element={<TwinCreatePage />} />
                <Route path="/twins/:id" element={<TwinDetailPage />} />
                <Route path="/twins/:id/settings" element={<TwinSettingsPage />} />

                {/* ── Feature-gated routes ── */}
                <Route path="/intel" element={
                  <PlanGate feature="paryty_intel">
                    <IntelView />
                  </PlanGate>
                } />
              </Route>
            </Routes>
          </Suspense>
        </ErrorBoundary>
      </AuthProvider>
      {/* Toast notifications — rendered outside all layout for correct stacking */}
      <ToastContainer />
    </Router>
  );
}

export default App;
