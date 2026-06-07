/**
 * Sidebar — Navigation sidebar matching the design system exactly.
 *
 * Uses `ds-sidebar` classes from the design system CSS.
 * Structure: brand block → nav items → bottom (settings/collapse).
 * Active item: white pill with black text/icon.
 *
 * @module components/layout/Sidebar
 */

import { useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import {
  LayoutDashboard,
  Globe,
  Activity,
  BarChart2,
  Bell,
  Brain,
  Settings,
  PanelLeftClose,
  PanelLeftOpen,
  ChevronRight,
} from 'lucide-react';
import clsx from 'clsx';

/** Navigation item definition. */
interface NavItem {
  label: string;
  icon: React.ReactNode;
  path: string;
}

/** Primary navigation items — matching design system nav. */
const NAV_ITEMS: NavItem[] = [
  { label: 'Dashboard', icon: <LayoutDashboard size={16} />, path: '/' },
  { label: 'Topology',  icon: <Globe size={16} />,           path: '/topology' },
  { label: 'Metrics',   icon: <BarChart2 size={16} />,       path: '/metrics' },
  { label: 'Timeline',  icon: <Activity size={16} />,         path: '/timeline' },
  { label: 'Alerts',    icon: <Bell size={16} />,             path: '/alerts' },
  { label: 'Paryty-Intel', icon: <Brain size={16} />,         path: '/intel' },
];

/** Props for the Sidebar component. */
interface SidebarProps {
  activePath: string;
}

/**
 * Application sidebar matching the design system sidebar pattern.
 *
 * Width transitions between `--aef-sidebar-width` (244px) and
 * `--aef-sidebar-collapsed` (56px).
 */
export function Sidebar({ activePath }: SidebarProps) {
  const [collapsed, setCollapsed] = useState(false);

  const toggleCollapse = useCallback(() => {
    setCollapsed((prev) => !prev);
  }, []);

  return (
    <aside
      className={clsx('ds-sidebar', collapsed ? 'ds-sidebar-collapsed' : 'ds-sidebar-expanded', 'app-sidebar')}
      style={{ width: collapsed ? 'var(--aef-sidebar-collapsed)' : 'var(--aef-sidebar-width)' }}
    >
      {/* Brand block — exact design system structure */}
      <div className="ds-sidebar__brand">
        {collapsed ? (
          <div className="ds-sidebar__brand-icon-only">
            <span className="ds-sidebar__brand-mark">PT</span>
          </div>
        ) : (
          <>
            <span className="ds-sidebar__brand-name">Paryty</span>
            <span className="ds-sidebar__brand-sub">Observability<br />V1.0</span>
          </>
        )}
      </div>

      {/* Navigation — exact design system structure */}
      <nav className="ds-sidebar__nav" aria-label="Main navigation">
        {NAV_ITEMS.map((item) => {
          const isActive = activePath === item.path || (item.path === '/' && activePath === '/topology');
          return (
            <Link
              key={item.path}
              to={item.path}
              className={clsx(
                'ds-nav-item',
                collapsed && 'ds-nav-item-icon-only',
                isActive && 'ds-nav-item-active',
              )}
              data-testid={`nav-${item.label.toLowerCase()}`}
              aria-current={isActive ? 'page' : undefined}
            >
              <span className="ds-nav-item__icon">{item.icon}</span>
              {!collapsed && <span className="ds-nav-item__label">{item.label}</span>}
              {isActive && !collapsed && <ChevronRight size={12} className="ds-nav-item__chevron" />}
            </Link>
          );
        })}
      </nav>

      {/* Bottom section — settings + collapse toggle */}
      <div className="ds-sidebar__bottom">
        <Link
          to="/settings"
          className={clsx('ds-nav-item', collapsed && 'ds-nav-item-icon-only')}
        >
          <span className="ds-nav-item__icon"><Settings size={16} /></span>
          {!collapsed && <span className="ds-nav-item__label">Settings</span>}
        </Link>
        <button
          className={clsx('ds-nav-item', collapsed && 'ds-nav-item-icon-only')}
          onClick={toggleCollapse}
          data-testid="sidebar-collapse-toggle"
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          <span className="ds-nav-item__icon">
            {collapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}
          </span>
          {!collapsed && <span className="ds-nav-item__label">Collapse</span>}
        </button>
      </div>
    </aside>
  );
}
