/**
 * App — Root application component.
 *
 * Wraps the entire application in BrowserRouter, StrictMode,
 * ErrorBoundary, and the AppShell layout.
 *
 * Routes:
 * - `/` → TopologyCanvas
 * - `/metrics` → MetricsView
 * - `/timeline` → TimelineView
 * - `/alerts` → AlertView
 * - `/intel` → IntelView
 */

import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';
import { Suspense, lazy } from 'react';
import { AppShell } from './components/layout/AppShell';
import { ErrorBoundary } from './components/common/ErrorBoundary';

// Lazy-loaded route components for code splitting
const TopologyCanvas = lazy(() =>
  import('./components/topology/TopologyCanvas').then((m) => ({ default: m.TopologyCanvas })),
);
const MetricsView = lazy(() => import('./components/MetricsView'));
const TimelineView = lazy(() => import('./components/TimelineView'));
const AlertView = lazy(() => import('./components/AlertView'));
const IntelView = lazy(() => import('./components/intel/IntelView'));

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
      <AppShell>
        <ErrorBoundary>
          <Suspense fallback={<LoadingFallback />}>
            <Routes>
              <Route path="/" element={<Navigate to="/topology" replace />} />
              <Route path="/topology" element={<TopologyCanvas />} />
              <Route path="/metrics" element={<MetricsView />} />
              <Route path="/timeline" element={<TimelineView />} />
              <Route path="/alerts" element={<AlertView />} />
              <Route path="/intel" element={<IntelView />} />
            </Routes>
          </Suspense>
        </ErrorBoundary>
      </AppShell>
    </Router>
  );
}

export default App;
