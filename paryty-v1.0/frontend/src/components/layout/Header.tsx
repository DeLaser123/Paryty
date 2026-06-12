/**
 * Topbar — Contextual application toolbar.
 *
 * Structure: page-title + active-entity (left) → tenant badge + plan badge
 * + time-window dropdown + alert bell + timeline toggle (right).
 *
 * Navigation tabs have been moved to the Sidebar.
 * Profile and log-out have been moved to the Sidebar bottom section.
 *
 * @module components/layout/Topbar
 */

import { useState, useRef, useEffect, useCallback } from 'react';
import { useLocation } from 'react-router-dom';
import { ChevronDown, Megaphone, ChevronRight, Clock } from 'lucide-react';
import clsx from 'clsx';
import { useAlertsStore } from '../../stores/alertsStore';
import { useTopologyStore } from '../../stores/topologyStore';
import { useAuthStore } from '../../stores/authStore';
import { usePlanStore } from '../../stores/planStore';
import { useDashboardStore } from '../../stores/dashboardStore';
import { useDropdownEdge } from '../../hooks/useDropdownEdge';
import { Tooltip } from '../common/Tooltip';

/** Route → display label map. */
const PAGE_LABELS: Record<string, string> = {
  '/':         'Dashboard',
  '/topology': 'Topology',
  '/metrics':  'Metrics',
  '/alerts':   'Alerts',
  '/intel':    'Paryty-Intel',
  '/settings': 'Settings',
};

/** Time-window options shown in the dropdown. */
interface TimeOption {
  label: string;
  value: string;
}

const TIME_OPTIONS: TimeOption[] = [
  { label: 'Last 15 minutes', value: '15m' },
  { label: 'Last 30 minutes', value: '30m' },
  { label: 'Last 1 hour',     value: '1h'  },
  { label: 'Last 3 hours',    value: '3h'  },
  { label: 'Last 6 hours',    value: '6h'  },
  { label: 'Last 24 hours',   value: '24h' },
  { label: 'Last 7 days',     value: '7d'  },
];

const DEFAULT_TIME = TIME_OPTIONS[2]; // Last 1 hour

/**
 * Application topbar with contextual title, time-window picker,
 * and alert bell.
 */
export function Header() {
  const location = useLocation();
  const firingCount = useAlertsStore((s) => s.firingCount());
  const selectedNode = useTopologyStore((s) => s.selectedNode);

  // Auth / plan data
  const tenant = useAuthStore((s) => s.tenant);
  const planName = usePlanStore((s) => s.currentPlan?.planName);

  // Active twin data (dashboard only)
  const catalogue = useDashboardStore((s) => s.catalogue);
  const activeTwinId = useDashboardStore((s) => s.activeTwinId);
  const activeTwin = catalogue.find((t) => t.id === activeTwinId) ?? null;

  const [timeWindow, setTimeWindow] = useState<TimeOption>(DEFAULT_TIME);
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const { flipRight, flipUp } = useDropdownEdge(dropdownRef, dropdownOpen);

  const pageLabel = PAGE_LABELS[location.pathname] ?? 'Paryty';
  const isTopology = location.pathname === '/topology';
  const isDashboard = location.pathname === '/';

  // Close dropdown when clicking outside
  useEffect(() => {
    if (!dropdownOpen) return;
    const handleOut = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
      }
    };
    document.addEventListener('mousedown', handleOut);
    return () => document.removeEventListener('mousedown', handleOut);
  }, [dropdownOpen]);

  // Close dropdown on Escape
  useEffect(() => {
    if (!dropdownOpen) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setDropdownOpen(false);
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [dropdownOpen]);

  const selectTime = useCallback((opt: TimeOption) => {
    setTimeWindow(opt);
    setDropdownOpen(false);
  }, []);

  return (
    <header className="app-topbar" data-testid="topbar">

      {/* ── Left: page title + active entity ── */}
      <div className="app-topbar__left">
        <span className="app-topbar__page-title">{pageLabel}</span>
        {isTopology && selectedNode && (
          <>
            <ChevronRight size={12} className="app-topbar__breadcrumb-sep" />
            <span className="app-topbar__entity">{selectedNode.name}</span>
          </>
        )}
        {isDashboard && activeTwin && (
          <>
            <ChevronRight size={12} className="app-topbar__breadcrumb-sep" />
            <span className="app-topbar__entity">{activeTwin.name}</span>
          </>
        )}
      </div>

      {/* ── Right: actions ── */}
      <div className="app-topbar__right">

        {/* Tenant badge */}
        <span className="app-topbar__tenant-badge">
          {tenant?.name ?? 'gai-tech'}
        </span>

        {/* Plan badge pill */}
        <span
          className="app-topbar__plan-badge"
          data-testid="topbar-plan-badge"
        >
          {planName ?? '—'}
        </span>

        {/* Time-window dropdown */}
        <div className="aef-dropdown" ref={dropdownRef}>
          <button
            className="app-topbar__time-trigger aef-dropdown-trigger"
            onClick={() => setDropdownOpen((o) => !o)}
            aria-haspopup="listbox"
            aria-expanded={dropdownOpen}
            data-testid="topbar-time-trigger"
          >
            <Clock size={12} style={{ flexShrink: 0, color: 'var(--aef-text-secondary)' }} />
            <span style={{ flex: 1, textAlign: 'left' }}>{timeWindow.label}</span>
            <ChevronDown
              size={12}
              className={clsx('aef-dropdown-chevron', dropdownOpen && 'aef-dropdown-chevron--open')}
            />
          </button>
          {dropdownOpen && (
            <div className={clsx('aef-dropdown-menu', flipRight && 'aef-dropdown-menu--flip', flipUp && 'aef-dropdown-menu--flip-up')} role="listbox" data-testid="topbar-time-menu">
              {TIME_OPTIONS.map((opt) => (
                <div
                  key={opt.value}
                  role="option"
                  aria-selected={opt.value === timeWindow.value}
                  className={clsx(
                    'aef-dropdown-item',
                    opt.value === timeWindow.value && 'aef-dropdown-item--selected',
                  )}
                  onClick={() => selectTime(opt)}
                >
                  <Tooltip label={opt.label}>
                    <span className="aef-dropdown-item__label">{opt.label}</span>
                  </Tooltip>
                  {opt.value === timeWindow.value && (
                    <span className="aef-dropdown-check">✓</span>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        {/* System notifications */}
        <button
          className="app-topbar__icon-btn"
          aria-label={`${firingCount} notifications`}
          data-testid="topbar-notifications"
        >
          <Megaphone size={15} />
          {firingCount > 0 && (
            <span className="app-topbar__alert-badge">{firingCount}</span>
          )}
        </button>

      </div>
    </header>
  );
}
