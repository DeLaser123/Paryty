/**
 * AppShell — Full-height application shell layout.
 *
 * Renders the Sidebar, Header, main content area, and StatusBar.
 * Uses flex layout for vertical stacking and horizontal sidebar + content.
 *
 * @module components/layout/AppShell
 */

import { type ReactNode, useLayoutEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { Header } from './Header';
import { StatusBar } from './StatusBar';
import { TimelineDrawerBar } from './TimelineDrawerBar';
import { ContextMenuProvider } from './ContextMenu';
import { useParticleStore } from '../../stores/particleStore';

/** Props for the AppShell component. */
interface AppShellProps {
  /** Child routes to render in the main content area. */
  children: ReactNode;
}

/**
 * Root layout shell for the Paryty observability dashboard.
 *
 * Structure:
 * ```
 * ┌──────────┬───────────────────────────────┐
 * │          │           Header              │
 * │ Sidebar  ├───────────────────────────────┤
 * │          │                               │
 * │          │         Main Content          │
 * │          │                               │
 * │          ├───────────────────────────────┤
 * │          │    TimelineDrawerBar (topo)   │
 * │          ├───────────────────────────────┤
 * │          │         StatusBar             │
 * └──────────┴───────────────────────────────┘
 * ```
 */
export function AppShell({ children }: AppShellProps) {
  const location = useLocation();
  const transportMode = useParticleStore((s) => s.transportMode);
  const isTopology = location.pathname === '/topology';

  // Toggle transport mode class on #root for CSS saturation degradation
  useLayoutEffect(() => {
    const root = document.getElementById('root');
    if (!root) return;
    root.classList.remove(
      'transport-mode--ws',
      'transport-mode--rest',
      'transport-mode--sse',
      'transport-mode--offline',
    );
    if (transportMode) {
      root.classList.add(`transport-mode--${transportMode}`);
    }
  }, [transportMode]);

  return (
    <div className="app-layout">
      <Sidebar activePath={location.pathname} />
      <div className="app-shell__main-wrapper">
        <Header />
        <main className="app-shell__content">
          <ContextMenuProvider>
            {children}
          </ContextMenuProvider>
        </main>
        {/* Timeline drawer bar — retractable, only on topology page */}
        {isTopology && <TimelineDrawerBar />}
        <StatusBar />
      </div>
    </div>
  );
}
