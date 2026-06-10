/**
 * Sidebar — Navigation sidebar matching the design system exactly.
 *
 * Uses `ds-sidebar` classes from the design system CSS.
 * Structure: brand block → nav items → bottom (settings/collapse).
 * Active item: white pill with black text/icon.
 *
 * Nav items are conditionally rendered based on the current plan's features.
 *
 * @module components/layout/Sidebar
 */

import { useState, useCallback, useRef, useEffect, useLayoutEffect, useMemo } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  LayoutDashboard,
  Globe,
  BarChart2,
  AlertTriangle,
  Brain,
  Settings,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  ChevronRight,
  UserCircle2,
} from 'lucide-react';
import clsx from 'clsx';
import { useAuthStore } from '../../stores/authStore';
import { usePlanStore } from '../../stores/planStore';
import { useDropdownEdge } from '../../hooks/useDropdownEdge';
import { Tooltip } from '../common/Tooltip';

/** Navigation item definition. */
interface NavItem {
  label: string;
  icon: React.ReactNode;
  path: string;
  /** Optional feature flag — item is hidden if plan lacks this feature. */
  feature?: string;
}

/** Props for the Sidebar component. */
interface SidebarProps {
  activePath: string;
}

/**
 * Application sidebar matching the design system sidebar pattern.
 *
 * Width transitions between `--aef-sidebar-width` (244px) and
 * `--aef-sidebar-collapsed` (56px). Features a morphing sliding-pill
 * active indicator that animates between nav items on route changes.
 */
export function Sidebar({ activePath }: SidebarProps) {
  const [collapsed, setCollapsed] = useState(false);
  const navRef = useRef<HTMLElement>(null);
  const [indicatorStyle, setIndicatorStyle] = useState<{ top: number; height: number; opacity: number }>({
    top: 0,
    height: 0,
    opacity: 0,
  });

  // Auth data
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const hasIntelFeature = usePlanStore((s) => s.hasFeature('paryty_intel'));
  const navigate = useNavigate();

  // Profile dropdown
  const [profileOpen, setProfileOpen] = useState(false);
  const profileRef = useRef<HTMLDivElement>(null);
  const { flipRight, flipDown } = useDropdownEdge(profileRef, profileOpen);

  const toggleCollapse = useCallback(() => {
    setCollapsed((prev) => !prev);
  }, []);

  // Close profile dropdown on outside click
  useEffect(() => {
    if (!profileOpen) return;
    const handleOut = (e: MouseEvent) => {
      if (profileRef.current && !profileRef.current.contains(e.target as Node)) {
        setProfileOpen(false);
      }
    };
    document.addEventListener('mousedown', handleOut);
    return () => document.removeEventListener('mousedown', handleOut);
  }, [profileOpen]);

  // Close profile dropdown on Escape
  useEffect(() => {
    if (!profileOpen) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setProfileOpen(false);
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [profileOpen]);

  const handleSettings = useCallback(() => {
    setProfileOpen(false);
    navigate('/settings');
  }, [navigate]);

  const handleSignOut = useCallback(async () => {
    setProfileOpen(false);
    await logout();
    navigate('/login');
  }, [logout, navigate]);

  // Build nav items — filter out Intel if plan lacks the feature
  const navItems = useMemo<NavItem[]>(() => {
    const items: NavItem[] = [
      { label: 'Dashboard', icon: <LayoutDashboard size={16} />, path: '/' },
      { label: 'Topology',  icon: <Globe size={16} />,           path: '/topology' },
      { label: 'Metrics',   icon: <BarChart2 size={16} />,       path: '/metrics' },
      { label: 'Alerts',    icon: <AlertTriangle size={16} />,   path: '/alerts' },
    ];
    if (hasIntelFeature) {
      items.push({ label: 'Paryty-Intel', icon: <Brain size={16} />, path: '/intel', feature: 'paryty_intel' });
    }
    return items;
  }, [hasIntelFeature]);

  // Measure active nav item and position the sliding indicator pill
  useLayoutEffect(() => {
    const nav = navRef.current;
    if (!nav) return;
    const activeEl = nav.querySelector('.ds-nav-item-active') as HTMLElement | null;
    if (!activeEl) {
      setIndicatorStyle({ top: 0, height: 0, opacity: 0 });
      return;
    }
    const navRect = nav.getBoundingClientRect();
    const activeRect = activeEl.getBoundingClientRect();
    setIndicatorStyle({
      top: activeRect.top - navRect.top + nav.scrollTop,
      height: activeRect.height,
      opacity: 1,
    });
  }, [activePath, collapsed, navItems]);

  return (
    <aside
      className={clsx('ds-sidebar', collapsed ? 'ds-sidebar-collapsed' : 'ds-sidebar-expanded', 'app-sidebar')}
      style={{ width: collapsed ? 'var(--aef-sidebar-collapsed)' : 'var(--aef-sidebar-width)' }}
    >
      {/* Brand block */}
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

      {/* Collapse toggle — between brand and nav, visually distinct */}
      <div className="ds-sidebar__collapse-row">
        <button
          className="ds-sidebar__collapse-btn"
          onClick={toggleCollapse}
          data-testid="sidebar-collapse-toggle"
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {collapsed ? <PanelLeftOpen size={14} /> : <PanelLeftClose size={14} />}
        </button>
      </div>

      {/* Navigation with sliding pill indicator */}
      <nav ref={navRef} className="ds-sidebar__nav aef-scroll" aria-label="Main navigation">
        {/* Sliding active indicator pill */}
        <div className="ds-nav-indicator" style={indicatorStyle} />
        {navItems.map((item) => {
          const isActive = item.path === '/'
            ? activePath === '/'
            : activePath === item.path || activePath.startsWith(item.path + '/');
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

      {/* Bottom section — profile with dropdown (Settings / Log out) */}
      <div className="ds-sidebar__bottom">
        <div className="aef-dropdown" ref={profileRef}>
          <button
            className={clsx('ds-sidebar__profile', collapsed && 'ds-sidebar__profile--icon-only')}
            onClick={() => setProfileOpen((o) => !o)}
            aria-haspopup="true"
            aria-expanded={profileOpen}
            aria-label="Profile menu"
            data-testid="sidebar-profile-trigger"
          >
            <div className="ds-sidebar__profile-avatar">
              <UserCircle2 size={16} />
            </div>
            {!collapsed && (
              <div className="ds-sidebar__profile-info">
                <span className="ds-sidebar__profile-name">{user?.name ?? 'Operator'}</span>
                <span className="ds-sidebar__profile-role">{user?.email ?? 'Tenant Admin'}</span>
              </div>
            )}
          </button>
          {profileOpen && (
            <div
              className={clsx(
                'aef-dropdown-menu',
                'sidebar-profile-dropdown',
                flipRight && 'aef-dropdown-menu--flip',
                flipDown && 'aef-dropdown-menu--flip-up',
              )}
              role="menu"
              data-testid="sidebar-profile-dropdown"
            >
              <div className="aef-dropdown-item" role="menuitem" onClick={handleSettings}>
                <Settings size={14} />
                <Tooltip label="Settings">
                  <span className="aef-dropdown-item__label">Settings</span>
                </Tooltip>
              </div>
              <div className="aef-dropdown-separator" role="separator" />
              <div className="aef-dropdown-item" role="menuitem" onClick={handleSignOut}>
                <LogOut size={14} />
                <Tooltip label="Log out">
                  <span className="aef-dropdown-item__label">Log out</span>
                </Tooltip>
              </div>
            </div>
          )}
        </div>
      </div>
    </aside>
  );
}
