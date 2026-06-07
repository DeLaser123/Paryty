/**
 * ErrorBoundary — React error boundary for visualization components.
 *
 * Catches rendering errors and shows a fallback UI with retry button.
 * Uses design system tokens for styling.
 *
 * @module components/common/ErrorBoundary
 */

import { Component, type ErrorInfo, type ReactNode } from 'react';

/** Props for ErrorBoundary. */
interface ErrorBoundaryProps {
  /** Child components to wrap. */
  children: ReactNode;
  /** Optional fallback UI. */
  fallback?: ReactNode;
}

/** State for ErrorBoundary. */
interface ErrorBoundaryState {
  /** Whether an error has been caught. */
  hasError: boolean;
  /** The caught error, if any. */
  error: Error | null;
}

/**
 * React error boundary that catches rendering errors and displays
 * a fallback UI with an error message and retry button.
 *
 * Uses design system container card pattern for the fallback.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('[ErrorBoundary] Caught error:', error, info.componentStack);
  }

  private handleRetry = (): void => {
    this.setState({ hasError: false, error: null });
  };

  render(): ReactNode {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }

      return (
        <div className="aef-container-card" style={{ margin: 'var(--aef-space-8)', maxWidth: 480 }}>
          <div className="aef-container-card__header">
            <span className="aef-container-card__title">Something went wrong</span>
          </div>
          <div className="aef-container-card__body">
            <p style={{ color: 'var(--aef-text-secondary)', fontSize: 12 }}>
              {this.state.error?.message ?? 'An unexpected error occurred'}
            </p>
            <button
              className="aef-btn aef-btn-active"
              onClick={this.handleRetry}
              data-testid="error-boundary-retry"
            >
              Try again
            </button>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}
