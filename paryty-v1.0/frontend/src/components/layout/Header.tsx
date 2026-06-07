/**
 * Header — Top application header matching the design system preview.
 *
 * Structure: tabs (left) → status indicator + bell + avatar (right).
 * Uses `ds-preview-header` classes from the design system CSS.
 *
 * @module components/layout/Header
 */

import { Link, useLocation } from 'react-router-dom';
import { Circle, Bell, Users } from 'lucide-react';
import clsx from 'clsx';
import { useWebSocket } from '../../hooks/useWebSocket';

/** Tab definitions for the header. */
const TABS = [
  { label: 'Dashboard', path: '/' },
  { label: 'Topology',  path: '/topology' },
  { label: 'Metrics',   path: '/metrics' },
  { label: 'Timeline',  path: '/timeline' },
  { label: 'Alerts',    path: '/alerts' },
  { label: 'Paryty-Intel', path: '/intel' },
];

/**
 * Top header bar matching the design system preview header.
 *
 * Height: 44px. Uses tabs for navigation, status indicator,
 * bell icon, and avatar.
 */
export function Header() {
  const location = useLocation();
  const { state, isConnected } = useWebSocket();

  return (
    <header className="ds-preview-header" data-testid="header">
      {/* Tabs — route-based navigation */}
      <div className="ds-preview-header__tabs">
        {TABS.map((tab) => {
          const isActive = location.pathname === tab.path || (tab.path === '/' && location.pathname === '/topology');
          return (
            <Link
              key={tab.path}
              to={tab.path}
              className={clsx('ds-tab', isActive && 'ds-tab-active')}
            >
              {tab.label}
            </Link>
          );
        })}
      </div>

      {/* Right-side actions — matching design system preview */}
      <div className="ds-preview-header__actions">
        {/* Connection status indicator */}
        <span className="ds-status-indicator">
          <Circle
            size={6}
            fill={isConnected ? 'var(--aef-status-live)' : state === 'connecting' || state === 'reconnecting' ? 'var(--aef-status-warning)' : 'var(--aef-text-secondary)'}
            color={isConnected ? 'var(--aef-status-live)' : state === 'connecting' || state === 'reconnecting' ? 'var(--aef-status-warning)' : 'var(--aef-text-secondary)'}
          />
          <span style={{
            color: isConnected ? 'var(--aef-status-live)' : 'var(--aef-text-secondary)',
            fontFamily: 'var(--aef-font-body)',
            fontSize: 11,
          }}>
            {isConnected ? 'Connected' : state === 'connecting' || state === 'reconnecting' ? 'Connecting…' : 'Disconnected'}
          </span>
        </span>

        {/* Bell */}
        <Link to="/alerts" className="ds-preview-header__icon-link">
          <Bell size={15} />
        </Link>

        {/* Avatar circle */}
        <div className="ds-avatar-circle">
          <Users size={13} />
        </div>
      </div>
    </header>
  );
}
