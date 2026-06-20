import { Suspense, type ReactNode } from 'react';
import { ErrorBoundary } from './ErrorBoundary';

interface LazyRouteProps {
  children: ReactNode;
}

/**
 * Per-route Suspense + ErrorBoundary wrapper.
 *
 * Isolates chunk load failures to the specific route — a failed
 * TopologyCanvas chunk does not break Dashboard or Settings.
 */
export function LazyRoute({ children }: LazyRouteProps) {
  return (
    <ErrorBoundary>
      <Suspense fallback={<LoadingSkeleton />}>
        {children}
      </Suspense>
    </ErrorBoundary>
  );
}

function LoadingSkeleton() {
  return (
    <div style={{ padding: 'var(--aef-space-6)', opacity: 0.5 }}>
      <div className="aef-container-card">
        <div className="aef-container-card__body">
          <div className="loading-fallback__progress">
            <div className="aef-progress-track">
              <div className="aef-progress-fill loading-fallback__progress-fill" />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
