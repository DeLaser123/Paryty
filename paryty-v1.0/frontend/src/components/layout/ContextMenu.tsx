/**
 * ContextMenu — Page-aware right-click context menu system.
 *
 * Listens for contextmenu events on the document, suppresses the
 * browser default, and renders an aef-context-menu at the cursor.
 * Menu items are determined by the current route.
 *
 * Topology items: particles, metric animations, node labels, timeline drawer,
 *   fullscreen.
 * Default items: refresh data, copy URL.
 *
 * @module components/layout/ContextMenu
 */

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { useLocation } from 'react-router-dom';
import {
  Sparkles,
  Activity,
  Tag,
  History,
  Maximize2,
  RefreshCw,
  Link2,
} from 'lucide-react';
import { useParticleStore } from '../../stores/particleStore';
import { useTopologyStore } from '../../stores/topologyStore';
import { Tooltip } from '../common/Tooltip';

// ─── Types ─────────────────────────────────────────────────────

interface MenuPosition {
  x: number;
  y: number;
}

interface ContextMenuCtx {
  /** Programmatically open the context menu at a position with custom items. */
  openAt: (pos: MenuPosition) => void;
  close: () => void;
}

const Ctx = createContext<ContextMenuCtx>({ openAt: () => {}, close: () => {} });
export const useContextMenu = () => useContext(Ctx);

// ─── Provider ──────────────────────────────────────────────────

interface Props {
  children: ReactNode;
}

/**
 * Wraps the app and provides a page-aware right-click context menu.
 */
export function ContextMenuProvider({ children }: Props) {
  const location = useLocation();
  const menuRef = useRef<HTMLDivElement>(null);

  const [pos, setPos] = useState<MenuPosition | null>(null);

  // Store selectors
  const particlesEnabled = useParticleStore((s) => s.particlesEnabled);
  const toggleParticles = useParticleStore((s) => s.toggleParticles);
  const metricsAnimationsEnabled = useTopologyStore((s) => s.metricsAnimationsEnabled);
  const toggleMetricsAnimations = useTopologyStore((s) => s.toggleMetricsAnimations);
  const nodeLabelsVisible = useTopologyStore((s) => s.nodeLabelsVisible);
  const toggleNodeLabels = useTopologyStore((s) => s.toggleNodeLabels);
  const timelineDrawerOpen = useTopologyStore((s) => s.timelineDrawerOpen);
  const toggleTimelineDrawer = useTopologyStore((s) => s.toggleTimelineDrawer);

  const isTopology = location.pathname === '/topology';

  const close = useCallback(() => setPos(null), []);

  // Intercept right-click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      // Don't intercept inside inputs / textareas
      const target = e.target as HTMLElement;
      if (target.closest('input, textarea, select, [contenteditable]')) return;

      e.preventDefault();

      // Viewport-edge detection: flip if near right/bottom edges
      const vw = window.innerWidth;
      const vh = window.innerHeight;
      const menuW = 200; // approximate
      const menuH = isTopology ? 220 : 100;
      setPos({
        x: e.clientX + menuW > vw ? e.clientX - menuW : e.clientX,
        y: e.clientY + menuH > vh ? e.clientY - menuH : e.clientY,
      });
    };
    document.addEventListener('contextmenu', handler);
    return () => document.removeEventListener('contextmenu', handler);
  }, [isTopology]);

  // Close on outside click
  useEffect(() => {
    if (!pos) return;
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        close();
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [pos, close]);

  // Close on Escape
  useEffect(() => {
    if (!pos) return;
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') close(); };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [pos, close]);

  // Close on route change
  useEffect(() => { close(); }, [location.pathname, close]);

  const act = useCallback((fn: () => void) => { fn(); close(); }, [close]);

  return (
    <Ctx.Provider value={{ openAt: setPos, close }}>
      {children}

      {pos && (
        <div
          ref={menuRef}
          className="aef-context-menu"
          style={{ position: 'fixed', left: pos.x, top: pos.y }}
          data-testid="context-menu"
        >
          {isTopology ? (
            <>
              {/* Topology-specific items */}
              <button
                className="aef-context-item"
                onClick={() => act(toggleParticles)}
                data-testid="ctx-particles"
              >
                <Sparkles size={13} />
                <Tooltip label="Particles">
                  <span className="aef-context-item__label">Particles</span>
                </Tooltip>
                <span className="aef-context-kbd">{particlesEnabled ? 'ON' : 'OFF'}</span>
              </button>

              <button
                className="aef-context-item"
                onClick={() => act(toggleMetricsAnimations)}
                data-testid="ctx-metrics-anim"
              >
                <Activity size={13} />
                <Tooltip label="Metric animations">
                  <span className="aef-context-item__label">Metric animations</span>
                </Tooltip>
                <span className="aef-context-kbd">{metricsAnimationsEnabled ? 'ON' : 'OFF'}</span>
              </button>

              <button
                className="aef-context-item"
                onClick={() => act(toggleNodeLabels)}
                data-testid="ctx-node-labels"
              >
                <Tag size={13} />
                <Tooltip label="Node labels">
                  <span className="aef-context-item__label">Node labels</span>
                </Tooltip>
                <span className="aef-context-kbd">{nodeLabelsVisible ? 'ON' : 'OFF'}</span>
              </button>

              <div className="aef-context-divider" />

              <button
                className="aef-context-item"
                onClick={() => act(toggleTimelineDrawer)}
                data-testid="ctx-timeline"
              >
                <History size={13} />
                <Tooltip label="Timeline replay">
                  <span className="aef-context-item__label">Timeline replay</span>
                </Tooltip>
                <span className="aef-context-kbd">{timelineDrawerOpen ? 'Hide' : 'Show'}</span>
              </button>

              <div className="aef-context-divider" />

              <button
                className="aef-context-item"
                onClick={() => act(() => {
                  if (document.fullscreenElement) {
                    document.exitFullscreen();
                  } else {
                    document.documentElement.requestFullscreen();
                  }
                })}
                data-testid="ctx-fullscreen"
              >
                <Maximize2 size={13} />
                <Tooltip label="Fullscreen">
                  <span className="aef-context-item__label">Fullscreen</span>
                </Tooltip>
                <span className="aef-context-kbd">F11</span>
              </button>
            </>
          ) : (
            <>
              {/* Generic items for all other pages */}
              <button
                className="aef-context-item"
                onClick={() => act(() => window.location.reload())}
                data-testid="ctx-refresh"
              >
                <RefreshCw size={13} />
                <Tooltip label="Refresh data">
                  <span className="aef-context-item__label">Refresh data</span>
                </Tooltip>
                <span className="aef-context-kbd">⌘R</span>
              </button>

              <button
                className="aef-context-item"
                onClick={() => act(() => navigator.clipboard.writeText(window.location.href))}
                data-testid="ctx-copy-url"
              >
                <Link2 size={13} />
                <Tooltip label="Copy page URL">
                  <span className="aef-context-item__label">Copy page URL</span>
                </Tooltip>
              </button>
            </>
          )}
        </div>
      )}
    </Ctx.Provider>
  );
}
