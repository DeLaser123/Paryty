/**
 * Module: DesignSystemPage
 *
 * Responsibility: Interactive showcase of the Paryty V1.0 interaction
 *   system: radius math, proprietary scroll, icon library, dropdowns, popups,
 *   animated navigation, and motion tokens. Demo-only; no backend calls.
 * Out of scope: simulation logic, state mutations, runtime data.
 * Consumes: tokens.css custom properties, lucide-react icons.
 */

import { useState, useRef, useEffect, useCallback, Fragment } from 'react';
import type { ElementType } from 'react';

import './tokens.css';
import './design-system-page.css';

import {
  LayoutDashboard, Globe, Cpu, GitBranch, Rocket, Activity,
  BarChart2, FileText, ShieldCheck, Settings, Bell, ChevronRight,
  CheckCircle2, AlertTriangle, Circle, ArrowUpRight, Users,
  Map, Table2, Sliders,
  /* Interaction-system icons */
  ChevronDown, X, Search,
  Zap, Lock, Key, Eye, Server,
  PenTool, Layers, Box, RefreshCw, Timer,
  TrendingUp, LineChart, Download, Copy, Trash2, Edit3, BookOpen,
  Info, Loader2, LayoutGrid, Columns, PanelLeft, Network,
  Plus, Play, Pause, SkipForward, Maximize2, Upload,
  XCircle,
} from 'lucide-react';

// - Nav item shape -
interface NavItem {
  label: string;
  icon: React.ReactNode;
  active?: boolean;
}

const PRIMARY_NAV: NavItem[] = [
  { label: 'Dashboard',       icon: <LayoutDashboard size={16} /> },
  { label: 'Universe',        icon: <Globe           size={16} />, active: true },
  { label: 'Entities',        icon: <Cpu             size={16} /> },
  { label: 'Flow Builder',    icon: <GitBranch       size={16} /> },
  { label: 'Deploy',          icon: <Rocket          size={16} /> },
  { label: 'Runtime',         icon: <Activity        size={16} /> },
  { label: 'Viz',             icon: <BarChart2       size={16} /> },
  { label: 'Reports',         icon: <FileText        size={16} /> },
  { label: 'Trust',           icon: <ShieldCheck     size={16} /> },
];

// - Section wrapper -
function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="ds-section">
      <h2 className="ds-section-title">{title}</h2>
      {children}
    </section>
  );
}

// - Token swatch -
function TokenSwatch({ label, value, border }: { label: string; value: string; border?: boolean }) {
  return (
    <div className="ds-token-swatch">
      <div
        className="ds-token-swatch__box"
        style={{
          background: value,
          border: border ? '1px solid var(--aef-border-strong)' : undefined,
        }}
      />
      <span className="ds-token-label">{label}</span>
      <span className="ds-token-value">{value}</span>
    </div>
  );
}

// - Button -
function AefButton({ active, children }: { active?: boolean; children: React.ReactNode }) {
  return (
    <button className={active ? 'aef-btn aef-btn-active' : 'aef-btn aef-btn-inactive'}>
      {children}
    </button>
  );
}

// - Counter card -
type CounterVariant = 'neutral' | 'active' | 'variant-a' | 'variant-b';

function CounterCard({
  label, value, icon, variant = 'neutral',
}: {
  label: string;
  value: string | number;
  icon: React.ReactNode;
  variant?: CounterVariant;
}) {
  return (
    <div className={`aef-counter aef-counter-${variant}`}>
      <div className="aef-counter__icon">{icon}</div>
      <div className="aef-counter__body">
        <span className="aef-counter__label">{label}</span>
        <span className="aef-counter__value">{value}</span>
      </div>
    </div>
  );
}

// - Table Card -
interface TableRow {
  name: string;
  type: string;
  status: 'valid' | 'warning' | 'pending';
  score: string;
}

/**
 * useActiveCard - reusable hook for card active-border state.
 * White border when the user focuses or clicks inside; returns to normal on
 * interaction outside. Uses document-level mousedown/focusin/focusout.
 * @perf - human-interaction rate only; no telemetry path.
 */
function useActiveCard() {
  const ref = useRef<HTMLDivElement>(null);
  const [isActive, setIsActive] = useState(false);

  useEffect(() => {
    function onDocMouseDown(e: MouseEvent) {
      if (ref.current) setIsActive(ref.current.contains(e.target as Node));
    }
    function onDocFocusIn(e: FocusEvent) {
      if (ref.current?.contains(e.target as Node)) setIsActive(true);
    }
    function onDocFocusOut(e: FocusEvent) {
      if (!ref.current?.contains(e.relatedTarget as Node)) setIsActive(false);
    }
    document.addEventListener('mousedown', onDocMouseDown);
    document.addEventListener('focusin', onDocFocusIn);
    document.addEventListener('focusout', onDocFocusOut);
    return () => {
      document.removeEventListener('mousedown', onDocMouseDown);
      document.removeEventListener('focusin', onDocFocusIn);
      document.removeEventListener('focusout', onDocFocusOut);
    };
  }, []);

  return { ref, isActive };
}

/**
 * DemoCard - applies active-border grammar to demo card surfaces.
 * @perf - human-interaction rate only; no telemetry path.
 */
function DemoCard({ children, style }: { children: React.ReactNode; style?: React.CSSProperties }) {
  const { ref, isActive } = useActiveCard();
  return (
    <div ref={ref} className={`ds-demo-card${isActive ? ' aef-card-active' : ''}`} style={style}>
      {children}
    </div>
  );
}

const TABLE_ROWS: TableRow[] = [
  { name: 'Sector-01',   type: 'Transport',   status: 'valid',   score: '98.2' },
  { name: 'Signal-Grid-A', type: 'Signal',     status: 'warning', score: '74.5' },
  { name: 'Flow-B-03',   type: 'Mobility',       status: 'pending', score: '-' },
];

function StatusBadge({ status }: { status: TableRow['status'] }) {
  const map = {
    valid:   { label: 'Valid',   cls: 'badge-valid'   },
    warning: { label: 'Warning', cls: 'badge-warning' },
    pending: { label: 'Pending', cls: 'badge-pending' },
  };
  const { label, cls } = map[status];
  return <span className={`aef-badge ${cls}`}>{label}</span>;
}

function TableCard() {
  const { ref, isActive } = useActiveCard();
  return (
    <div ref={ref} className={`aef-table-card${isActive ? ' aef-card-active' : ''}`}>
      <div className="aef-table-card__header">
        <Table2 size={14} />
        <span>Input Validation</span>
      </div>
      <table className="aef-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Status</th>
            <th>Score</th>
          </tr>
        </thead>
        <tbody>
          {TABLE_ROWS.map(row => (
            <tr key={row.name}>
              <td>{row.name}</td>
              <td>{row.type}</td>
              <td><StatusBadge status={row.status} /></td>
              <td>{row.score}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// - Container card -
function ContainerCard({
  title, icon, metaLabel, children,
}: {
  title: string;
  icon: React.ReactNode;
  metaLabel?: string;
  children: React.ReactNode;
}) {
  const { ref, isActive } = useActiveCard();
  return (
    <div ref={ref} className={`aef-container-card${isActive ? ' aef-card-active' : ''}`}>
      <div className="aef-container-card__header">
        <span className="aef-container-card__icon">{icon}</span>
        <span className="aef-container-card__title">{title}</span>
        {metaLabel && <span className="aef-meta-pill">{metaLabel}</span>}
      </div>
      <div className="aef-container-card__body">{children}</div>
    </div>
  );
}

// - Viz well (inner map panel placeholder) -
function VizWell({ label }: { label: string }) {
  return (
    <div className="aef-viz-well">
      <Map size={20} className="aef-viz-well__icon" />
      <span className="aef-viz-well__label">{label}</span>
    </div>
  );
}

// - Compact stat module -
function StatModule({ label, value }: { label: string; value: string }) {
  return (
    <div className="aef-stat-module">
      <span className="aef-stat-module__label">{label}</span>
      <span className="aef-stat-module__value">{value}</span>
    </div>
  );
}

// - Sidebar (expanded) -
function ExpandedSidebar() {
  return (
    <aside className="ds-sidebar ds-sidebar-expanded">
      <div className="ds-sidebar__brand">
        <span className="ds-sidebar__brand-name">Paryty</span>
        <span className="ds-sidebar__brand-sub">Design System<br />V1.0</span>
      </div>
      <nav className="ds-sidebar__nav">
        {PRIMARY_NAV.map(item => (
          <div key={item.label} className={item.active ? 'ds-nav-item ds-nav-item-active' : 'ds-nav-item'}>
            <span className="ds-nav-item__icon">{item.icon}</span>
            <span className="ds-nav-item__label">{item.label}</span>
            {item.active && <ChevronRight size={12} className="ds-nav-item__chevron" />}
          </div>
        ))}
      </nav>
      <div className="ds-sidebar__bottom">
        <div className="ds-nav-item">
          <span className="ds-nav-item__icon"><Settings size={16} /></span>
          <span className="ds-nav-item__label">Settings</span>
        </div>
      </div>
    </aside>
  );
}

// - Sidebar (collapsed) -
function CollapsedSidebar() {
  return (
    <aside className="ds-sidebar ds-sidebar-collapsed">
      <div className="ds-sidebar__brand ds-sidebar__brand-icon-only">
        <span className="ds-sidebar__brand-mark">PT</span>
      </div>
      <nav className="ds-sidebar__nav">
        {PRIMARY_NAV.map(item => (
          <div
            key={item.label}
            className={item.active ? 'ds-nav-item ds-nav-item-icon-only ds-nav-item-active' : 'ds-nav-item ds-nav-item-icon-only'}
            title={item.label}
          >
            <span className="ds-nav-item__icon">{item.icon}</span>
          </div>
        ))}
      </nav>
      <div className="ds-sidebar__bottom">
        <div className="ds-nav-item ds-nav-item-icon-only" title="Settings">
          <span className="ds-nav-item__icon"><Settings size={16} /></span>
        </div>
      </div>
    </aside>
  );
}

// - Dashboard preview (full layout mock) -
function DashboardPreview() {
  return (
    <div className="ds-dashboard-preview">
      {/* Sidebar */}
      <aside className="ds-preview-sidebar">
        <div className="ds-sidebar__brand">
          <span className="ds-sidebar__brand-name">Paryty</span>
          <span className="ds-sidebar__brand-sub">Design System<br />V1.0</span>
        </div>
        <nav className="ds-sidebar__nav">
          {PRIMARY_NAV.map(item => (
            <div key={item.label} className={item.active ? 'ds-nav-item ds-nav-item-active' : 'ds-nav-item'}>
              <span className="ds-nav-item__icon">{item.icon}</span>
              <span className="ds-nav-item__label">{item.label}</span>
            </div>
          ))}
        </nav>
        <div className="ds-sidebar__bottom">
          <div className="ds-nav-item">
            <span className="ds-nav-item__icon"><Settings size={16} /></span>
            <span className="ds-nav-item__label">Settings</span>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <div className="ds-preview-main">
        {/* Top header */}
        <header className="ds-preview-header">
          <div className="ds-preview-header__tabs">
            {['Scenario Comparison', 'View Matrix', 'Runtime Active'].map((t, i) => (
              <span key={t} className={i === 2 ? 'ds-tab ds-tab-active' : 'ds-tab'}>{t}</span>
            ))}
          </div>
          <div className="ds-preview-header__actions">
            <span className="ds-status-indicator">
              <Circle size={6} fill="var(--aef-status-live)" color="var(--aef-status-live)" />
              <span style={{ color: 'var(--aef-status-live)', fontFamily: 'var(--aef-font-body)', fontSize: 11 }}>Runtime Active</span>
            </span>
            <Bell size={15} />
            <div className="ds-avatar-split">
              <span className="ds-avatar-half ds-avatar-half-a" />
              <span className="ds-avatar-half ds-avatar-half-b" />
            </div>
            <div className="ds-avatar-circle">
              <Users size={13} />
            </div>
          </div>
        </header>

        {/* Page heading */}
        <div className="ds-preview-heading">
          <h1 className="ds-preview-heading__title">Universe Builder</h1>
          <p className="ds-preview-heading__sub">Define the physical topology and compile before deployment.</p>
        </div>

        {/* Counter row */}
        <div className="ds-preview-counter-row">
          {[
            { label: 'Entities',   value: '142',  v: 'neutral'   as CounterVariant },
            { label: 'Compiled',   value: '139',  v: 'active'    as CounterVariant },
            { label: 'Flows',      value: '28',   v: 'neutral'   as CounterVariant },
            { label: 'Violations', value: '3',    v: 'variant-b' as CounterVariant },
            { label: 'Scenarios',  value: '7',    v: 'neutral'   as CounterVariant },
            { label: 'Reports',    value: '12',   v: 'variant-a' as CounterVariant },
          ].map(c => (
            <CounterCard key={c.label} label={c.label} value={c.value} icon={<ArrowUpRight size={12} />} variant={c.v} />
          ))}
        </div>

        {/* Two-column main area */}
        <div className="ds-preview-body">
          {/* Large viz container */}
          <ContainerCard title="Universe Topology" icon={<Globe size={14} />} metaLabel="Compiled">
            <VizWell label="Physical topology viewport" />
            <div className="ds-preview-stat-row">
              <StatModule label="Nodes" value="142" />
              <StatModule label="Edges" value="317" />
              <StatModule label="Zones" value="9" />
              <StatModule label="Compile" value="v12" />
            </div>
          </ContainerCard>

          {/* Right column */}
          <div className="ds-preview-right-col">
            <ContainerCard title="Entity Catalog" icon={<Cpu size={14} />}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {['Road Segment', 'Signal Controller', 'Emergency Vehicle', 'Road Sensor'].map(n => (
                  <div key={n} className="aef-catalog-row">
                    <Circle size={6} fill="var(--aef-border)" color="var(--aef-border)" />
                    <span>{n}</span>
                  </div>
                ))}
              </div>
            </ContainerCard>
            <ContainerCard title="Input Validation" icon={<ShieldCheck size={14} />}>
              <TableCard />
            </ContainerCard>
          </div>
        </div>
      </div>
    </div>
  );
}

// - Progress bar demo -
function ProgressDemo() {
  return (
    <div className="ds-progress-demo">
      {[20, 55, 80, 100].map(v => (
        <div key={v} className="ds-progress-row">
          <span className="ds-progress-label">{v}%</span>
          <div className="aef-progress-track">
            <div className="aef-progress-fill" style={{ width: `${v}%` }} />
          </div>
        </div>
      ))}
    </div>
  );
}

// - Indicator dots -
function IndicatorDots() {
  return (
    <div className="ds-dot-row">
      {[1, 2, 3, 4].map(i => (
        <span key={i} className={i === 2 ? 'aef-dot aef-dot-active' : 'aef-dot'} />
      ))}
    </div>
  );
}

// - Root page -

/* ============================================================
   SECTION 11 - Radius Mathematics
   ============================================================ */

const RADIUS_TOKENS = [
  { name: '--aef-radius-control', value: '10px',    label: 'control', desc: 'buttons, chips' },
  { name: '--aef-radius-field',   value: '12px',    label: 'field',   desc: 'inputs, selects' },
  { name: '--aef-radius-card',    value: '14px',    label: 'card',    desc: 'cards, popups' },
  { name: '--aef-radius-panel',   value: '20px',    label: 'panel',   desc: 'panels, modals' },
  { name: '--aef-radius-sheet',   value: '24px',    label: 'sheet',   desc: 'sheets, drawers' },
  { name: '--aef-radius-full',    value: '9999px',  label: 'full',    desc: 'circles, pills' },
];

function RadiusSection() {
  return (
    <Section title="Radius Mathematics">
      <div className="ds-radius-scale-row">
        {RADIUS_TOKENS.map(t => (
          <div key={t.name} className="ds-radius-sample">
            <div className="ds-radius-sample__box" style={{ borderRadius: t.value }} />
            <span className="ds-token-label">{t.name}</span>
            <span className="ds-token-value">{t.value}</span>
            <span className="ds-radius-desc">{t.desc}</span>
          </div>
        ))}
      </div>
      <div className="ds-radius-nested-demo">
        <div className="ds-radius-panel-wrap">
          <span className="ds-radius-tag">panel: 20px</span>
          <div className="ds-radius-card-wrap">
            <span className="ds-radius-tag">card: 14px (parent 20px - inset 4px =&gt; clamp to 14px)</span>
            <div className="ds-radius-control-wrap">
              <span className="ds-radius-tag">control: 10px (parent 14px - inset 4px = 10px)</span>
            </div>
          </div>
        </div>
      </div>
      <div className="ds-annotation" style={{ marginTop: 12 }}>
        <p>Default: clamp(10px, min(w,h) * 0.22, 28px). Large surfaces: clamp(18px, min(w,h) * 0.08, 36px).</p>
        <p>Nested rule: child radius = max(parent radius - spacing inset, radius-control).</p>
      </div>
    </Section>
  );
}

/* ============================================================
   SECTION 12 - Proprietary Scroll
   ============================================================ */

/**
 * Native keydown handler for vertical owned scroll regions.
 * Attached via addEventListener (not React onKeyDown) so Puppeteer CDP
 * keyboard events reach the handler reliably in all headless configurations.
 * Prevents default only for the keys this handler manages.
 * Does not touch document/body scroll.
 */
function verticalScrollKeyHandler(e: KeyboardEvent): void {
  const el = e.currentTarget as HTMLElement;
  const step = 40;
  const page = Math.round(el.clientHeight * 0.8);
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault();
      el.scrollTop += step;
      break;
    case 'ArrowUp':
      e.preventDefault();
      el.scrollTop -= step;
      break;
    case 'PageDown':
      e.preventDefault();
      el.scrollTop += page;
      break;
    case 'PageUp':
      e.preventDefault();
      el.scrollTop -= page;
      break;
    case 'Home':
      e.preventDefault();
      el.scrollTop = 0;
      break;
    case 'End':
      e.preventDefault();
      el.scrollTop = el.scrollHeight - el.clientHeight;
      break;
    default:
      break;
  }
}

/**
 * Native keydown handler for horizontal owned scroll regions.
 * PageDown/ArrowRight scroll right; PageUp/ArrowLeft scroll left; Home/End clamp.
 * Attached via addEventListener for the same reliability reason as above.
 */
function horizontalScrollKeyHandler(e: KeyboardEvent): void {
  const el = e.currentTarget as HTMLElement;
  const step = 80;
  const page = Math.round(el.clientWidth * 0.8);
  switch (e.key) {
    case 'ArrowRight':
    case 'PageDown':
      e.preventDefault();
      el.scrollLeft += (e.key === 'ArrowRight' ? step : page);
      break;
    case 'ArrowLeft':
    case 'PageUp':
      e.preventDefault();
      el.scrollLeft -= (e.key === 'ArrowLeft' ? step : page);
      break;
    case 'Home':
      e.preventDefault();
      el.scrollLeft = 0;
      break;
    case 'End':
      e.preventDefault();
      el.scrollLeft = el.scrollWidth - el.clientWidth;
      break;
    default:
      break;
  }
}

/**
 * Native wheel handler for horizontal scroll regions.
 * Converts vertical wheel delta to horizontal scroll only when the container
 * can consume it. Prevents default only on consumable deltas.
 * No global wheel hijack - attached only to the specific container ref.
 */
function horizontalWheelHandler(e: WheelEvent): void {
  const el = e.currentTarget as HTMLElement;
  // Pass through if horizontal scroll already dominates
  if (Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return;
  const atStart = el.scrollLeft <= 0;
  const atEnd   = el.scrollLeft >= el.scrollWidth - el.clientWidth - 1;
  // Pass through if container cannot consume this direction
  if ((e.deltaY < 0 && atStart) || (e.deltaY > 0 && atEnd)) return;
  e.preventDefault();
  el.scrollLeft += e.deltaY;
}

/**
 * Attaches a non-passive wheel listener to a scroll region ref.
 * Returns cleanup for use in useEffect.
 */
function attachWheelListener(
  ref: React.RefObject<HTMLDivElement | null>,
  handler: (e: WheelEvent) => void,
): () => void {
  const el = ref.current;
  if (!el) return () => { /* no-op */ };
  el.addEventListener('wheel', handler, { passive: false });
  return () => el.removeEventListener('wheel', handler);
}

/**
 * Attaches a native (non-passive) keydown listener to a scroll region ref.
 * Returns the cleanup function for use in useEffect.
 */
function attachScrollKeyListener(
  ref: React.RefObject<HTMLDivElement | null>,
  handler: (e: KeyboardEvent) => void,
): () => void {
  const el = ref.current;
  if (!el) return () => { /* no-op */ };
  el.addEventListener('keydown', handler, { passive: false });
  return () => el.removeEventListener('keydown', handler);
}

function ScrollSection() {
  const longItems = Array.from({ length: 22 }, (_, i) => `Entity-${String(i + 1).padStart(3, '0')}`);
  const hItems = Array.from({ length: 16 }, (_, i) => `Zone-${i + 1}`);

  // Refs for all five owned scroll demo regions
  const longPanelRef = useRef<HTMLDivElement>(null);
  const outerPanelRef = useRef<HTMLDivElement>(null);
  const innerPanelRef = useRef<HTMLDivElement>(null);
  const tableBodyRef = useRef<HTMLDivElement>(null);
  const hStripRef = useRef<HTMLDivElement>(null);

  // Attach native non-passive keydown listeners; bypasses React synthetic event
  // delegation so Puppeteer CDP keyboard events always reach the handler.
  useEffect(() => {
    const cleanups = [
      attachScrollKeyListener(longPanelRef, verticalScrollKeyHandler),
      attachScrollKeyListener(outerPanelRef, verticalScrollKeyHandler),
      attachScrollKeyListener(innerPanelRef, verticalScrollKeyHandler),
      attachScrollKeyListener(tableBodyRef, verticalScrollKeyHandler),
      attachScrollKeyListener(hStripRef, horizontalScrollKeyHandler),
      attachWheelListener(hStripRef, horizontalWheelHandler),
    ];
    return () => cleanups.forEach(fn => fn());
  }, []);

  return (
    <Section title="Proprietary Scroll">
      <div className="ds-scroll-demos-grid">
        {/* Long panel */}
        <div className="ds-scroll-demo-card">
          <span className="ds-scroll-demo-label">Long panel</span>
          <div className="aef-scroll-wrap">
            <div
              ref={longPanelRef}
              className="aef-scroll-region"
              style={{ maxHeight: 180 }}
              tabIndex={0}
              role="region"
              aria-label="Long panel scroll region"
            >
              {longItems.map(item => (
                <div key={item} className="ds-scroll-item">{item}</div>
              ))}
            </div>
          </div>
        </div>

        {/* Nested panels */}
        <div className="ds-scroll-demo-card">
          <span className="ds-scroll-demo-label">Nested panels</span>
          <div className="aef-scroll-wrap">
            <div
              ref={outerPanelRef}
              className="aef-scroll-region"
              style={{ maxHeight: 180 }}
              tabIndex={0}
              role="region"
              aria-label="Outer nested scroll panel"
            >
              <span className="ds-scroll-demo-label" style={{ fontSize: 10, padding: '4px 8px', display: 'block' }}>Outer</span>
              {longItems.slice(0, 6).map(item => (
                <div key={item} className="ds-scroll-item">{item}</div>
              ))}
              <div className="aef-scroll-wrap" style={{ margin: '6px 8px', borderRadius: 'var(--aef-radius-control)' }}>
                <div
                  ref={innerPanelRef}
                  className="aef-scroll-region"
                  style={{ maxHeight: 90, background: 'var(--aef-surface-mid)' }}
                  tabIndex={0}
                  role="region"
                  aria-label="Inner scroll panel"
                >
                  <span className="ds-scroll-demo-label" style={{ fontSize: 10, padding: '4px 8px', display: 'block' }}>Inner</span>
                  {longItems.slice(6, 14).map(item => (
                    <div key={`i-${item}`} className="ds-scroll-item">{item}</div>
                  ))}
                </div>
              </div>
              {longItems.slice(14).map(item => (
                <div key={`o2-${item}`} className="ds-scroll-item">{item}</div>
              ))}
            </div>
          </div>
        </div>

        {/* Table body scroll */}
        <div className="ds-scroll-demo-card">
          <span className="ds-scroll-demo-label">Table body scroll</span>
          <div className="aef-table-card" style={{ overflow: 'hidden' }}>
            <table className="aef-table" style={{ tableLayout: 'fixed', width: '100%' }}>
              <thead>
                <tr>
                  <th>Node</th>
                  <th>Status</th>
                </tr>
              </thead>
            </table>
            <div
              ref={tableBodyRef}
              className="aef-scroll-region"
              style={{ maxHeight: 140 }}
              tabIndex={0}
              role="region"
              aria-label="Table body scroll region"
            >
              <table className="aef-table" style={{ tableLayout: 'fixed', width: '100%' }}>
                <tbody>
                  {longItems.map(item => (
                    <tr key={item}>
                      <td>{item}</td>
                      <td style={{ color: 'var(--aef-status-live)', fontSize: 10 }}>ok</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        {/* Horizontal strip */}
        <div className="ds-scroll-demo-card">
          <span className="ds-scroll-demo-label">Horizontal strip</span>
          <div className="aef-scroll-wrap aef-scroll-wrap--h">
            <div
              ref={hStripRef}
              className="aef-scroll-region aef-scroll-region--h"
              tabIndex={0}
              role="region"
              aria-label="Horizontal zone strip"
            >
              {hItems.map(z => (
                <div key={z} className="ds-hscroll-chip">{z}</div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </Section>
  );
}

/* ============================================================
   SECTION 13 - Icon Library
   ============================================================ */

const ICON_SIZES_MAP = { xs: 12, sm: 14, md: 16, lg: 20, xl: 24 } as const;
type IconSize = keyof typeof ICON_SIZES_MAP;
type IconTone = 'default' | 'muted' | 'live' | 'warning' | 'accent-a' | 'accent-b';
type IconState = 'idle' | 'hover' | 'pressed' | 'active' | 'loading' | 'success' | 'warning' | 'disabled';

interface ParytyIconProps {
  icon: ElementType;
  size?: IconSize;
  tone?: IconTone;
  state?: IconState;
  label: string;
  className?: string;
}

/** @perf - renders at human-interaction rate only; no telemetry path */
function ParytyIcon({ icon: Icon, size = 'md', tone = 'default', state = 'idle', label, className = '' }: ParytyIconProps) {
  const px = ICON_SIZES_MAP[size];
  return (
    <span
      className={`aef-icon aef-icon-interactive aef-icon--sz-${size} aef-icon--tone-${tone} aef-icon--state-${state} ${className}`}
      title={label}
      aria-label={label}
      role="img"
    >
      <Icon size={px} aria-hidden />
    </span>
  );
}

const ICON_CATALOG: Array<{ category: string; icons: Array<{ icon: ElementType; label: string }> }> = [
  {
    category: 'Navigation',
    icons: [
      { icon: LayoutDashboard, label: 'Dashboard' },
      { icon: Globe,           label: 'Universe' },
      { icon: GitBranch,       label: 'Flow Builder' },
      { icon: Rocket,          label: 'Deploy' },
      { icon: Map,             label: 'Map' },
    ],
  },
  {
    category: 'Builders',
    icons: [
      { icon: PenTool, label: 'Draw' },
      { icon: Layers,  label: 'Layers' },
      { icon: Box,     label: 'Entity' },
      { icon: Network, label: 'Network' },
      { icon: Columns, label: 'Columns' },
    ],
  },
  {
    category: 'Runtime',
    icons: [
      { icon: Zap,       label: 'Trigger' },
      { icon: Timer,     label: 'Timer' },
      { icon: RefreshCw, label: 'Refresh' },
      { icon: Server,    label: 'Server' },
      { icon: Activity,  label: 'Activity' },
    ],
  },
  {
    category: 'Telemetry',
    icons: [
      { icon: BarChart2,  label: 'Bar Chart' },
      { icon: TrendingUp, label: 'Trend Up' },
      { icon: LineChart,  label: 'Line Chart' },
      { icon: Sliders,    label: 'Sliders' },
      { icon: Bell,       label: 'Alert' },
    ],
  },
  {
    category: 'Reports',
    icons: [
      { icon: FileText, label: 'File Text' },
      { icon: BookOpen, label: 'Book Open' },
      { icon: Table2,   label: 'Table' },
      { icon: Download, label: 'Download' },
      { icon: Copy,     label: 'Copy' },
    ],
  },
  {
    category: 'Trust',
    icons: [
      { icon: ShieldCheck,    label: 'Shield Check' },
      { icon: Lock,           label: 'Lock' },
      { icon: Key,            label: 'Key' },
      { icon: Eye,            label: 'Eye' },
      { icon: AlertTriangle,  label: 'Alert' },
    ],
  },
  {
    category: 'Actions',
    icons: [
      { icon: Plus,   label: 'Add' },
      { icon: Trash2, label: 'Delete' },
      { icon: Edit3,  label: 'Edit' },
      { icon: Upload, label: 'Upload' },
      { icon: Search, label: 'Search' },
    ],
  },
  {
    category: 'Media',
    icons: [
      { icon: Play,        label: 'Play' },
      { icon: Pause,       label: 'Pause' },
      { icon: SkipForward, label: 'Skip Forward' },
      { icon: Maximize2,   label: 'Maximize' },
      { icon: X,           label: 'Close' },
    ],
  },
  {
    category: 'Layout',
    icons: [
      { icon: LayoutGrid,   label: 'Grid' },
      { icon: PanelLeft,    label: 'Panel Left' },
      { icon: ChevronDown,  label: 'Chevron Down' },
      { icon: ChevronRight, label: 'Chevron Right' },
      { icon: ArrowUpRight, label: 'Arrow Up Right' },
    ],
  },
  {
    category: 'Status',
    icons: [
      { icon: CheckCircle2, label: 'Check Circle' },
      { icon: Info,         label: 'Info' },
      { icon: XCircle,      label: 'X Circle' },
      { icon: Loader2,      label: 'Loading' },
      { icon: Circle,       label: 'Circle' },
    ],
  },
];

const ICON_STATE_DEMOS: Array<{ state: IconState; desc: string }> = [
  { state: 'idle',     desc: 'Idle' },
  { state: 'hover',    desc: 'Hover' },
  { state: 'pressed',  desc: 'Pressed' },
  { state: 'active',   desc: 'Active' },
  { state: 'loading',  desc: 'Loading' },
  { state: 'success',  desc: 'Success' },
  { state: 'warning',  desc: 'Warning' },
  { state: 'disabled', desc: 'Disabled' },
];

function IconLibrarySection() {
  return (
    <Section title="Icon Library (50 icons across 10 categories)">
      <div className="ds-icon-catalog">
        {ICON_CATALOG.map(cat => (
          <div key={cat.category} className="ds-icon-category">
            <span className="ds-icon-category-label">{cat.category}</span>
            <div className="ds-icon-row">
              {cat.icons.map(({ icon: Icon, label }) => (
                <div key={label} className="ds-icon-cell" title={label}>
                  <ParytyIcon icon={Icon} label={label} size="md" />
                  <span className="ds-icon-cell-label">{label}</span>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
      <div className="ds-icon-states-demo">
        <span className="ds-token-label" style={{ marginBottom: 8, display: 'block' }}>
          Animated states - Activity icon (CSS-driven: rotate, scale-settle, pulse)
        </span>
        <div className="ds-icon-states-row">
          {ICON_STATE_DEMOS.map(({ state, desc }) => (
            <div key={state} className="ds-icon-state-cell">
              <ParytyIcon icon={Activity} label={`Activity - ${desc}`} size="lg" state={state} />
              <span className="ds-icon-state-label">{desc}</span>
            </div>
          ))}
        </div>
      </div>
    </Section>
  );
}

/* ============================================================
   SECTION 14 - Dropdowns, Popups & Menus
   ============================================================ */

const DROPDOWN_OPTIONS = [
  { value: 'compile',     label: 'Compile Universe', kind: 'normal' },
  { value: 'deploy',      label: 'Deploy Scenario',  kind: 'normal' },
  { value: 'export',      label: 'Export Report',    kind: 'normal' },
  { value: 'processing',  label: 'Processing...',    kind: 'loading' },
  { value: 'archive',     label: 'Archive',          kind: 'destructive' },
  { value: 'disabled-op', label: 'Disabled action',  kind: 'disabled' },
] as const;

function DropdownDemo() {
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState('Select action');
  // Roving focus: index into the focusable subset of DROPDOWN_OPTIONS
  const [focusedIdx, setFocusedIdx] = useState(0);
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef   = useRef<HTMLButtonElement>(null);
  const itemRefs     = useRef<(HTMLDivElement | null)[]>([]);

  // Indices that can receive focus (not disabled, not loading)
  const navigableIdxs = DROPDOWN_OPTIONS.reduce<number[]>((acc, opt, i) => {
    if (opt.kind !== 'disabled' && opt.kind !== 'loading') acc.push(i);
    return acc;
  }, []);

  const closeMenu = useCallback(() => {
    setOpen(false);
    requestAnimationFrame(() => triggerRef.current?.focus());
  }, []);

  // On open: focus first navigable item
  useEffect(() => {
    if (!open) return;
    const firstIdx = navigableIdxs[0] ?? 0;
    setFocusedIdx(firstIdx);
    requestAnimationFrame(() => itemRefs.current[firstIdx]?.focus());
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  // Outside click
  useEffect(() => {
    if (!open) return;
    function onOut(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) closeMenu();
    }
    document.addEventListener('mousedown', onOut);
    return () => document.removeEventListener('mousedown', onOut);
  }, [open, closeMenu]);

  const handleMenuKeyDown = useCallback((e: React.KeyboardEvent) => {
    const pos = navigableIdxs.indexOf(focusedIdx);
    if (e.key === 'Escape') {
      e.preventDefault();
      closeMenu();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      const nextPos  = Math.min(pos + 1, navigableIdxs.length - 1);
      const nextIdx  = navigableIdxs[nextPos];
      setFocusedIdx(nextIdx);
      itemRefs.current[nextIdx]?.focus();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      const prevPos  = Math.max(pos - 1, 0);
      const prevIdx  = navigableIdxs[prevPos];
      setFocusedIdx(prevIdx);
      itemRefs.current[prevIdx]?.focus();
    } else if (e.key === 'Home') {
      e.preventDefault();
      const firstIdx = navigableIdxs[0];
      setFocusedIdx(firstIdx);
      itemRefs.current[firstIdx]?.focus();
    } else if (e.key === 'End') {
      e.preventDefault();
      const lastIdx  = navigableIdxs[navigableIdxs.length - 1];
      setFocusedIdx(lastIdx);
      itemRefs.current[lastIdx]?.focus();
    }
  }, [focusedIdx, navigableIdxs, closeMenu]);

  const activateItem = useCallback((optLabel: string) => {
    setSelected(optLabel);
    closeMenu();
  }, [closeMenu]);

  return (
    <DemoCard>
      <span className="ds-demo-card-label">Dropdown Select</span>
      <div ref={containerRef} style={{ position: 'relative' }}>
        <button
          ref={triggerRef}
          className="aef-dropdown-trigger"
          onClick={() => setOpen(o => !o)}
          aria-haspopup="listbox"
          aria-expanded={open}
          onKeyDown={e => {
            if ((e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') && !open) {
              e.preventDefault();
              setOpen(true);
            }
          }}
        >
          <span>{selected}</span>
          <ChevronDown size={14} className={open ? 'aef-dropdown-chevron aef-dropdown-chevron--open' : 'aef-dropdown-chevron'} aria-hidden />
        </button>
        {open && (
          <div
            className="aef-dropdown-menu"
            role="listbox"
            aria-label="Actions"
            onKeyDown={handleMenuKeyDown}
          >
            {DROPDOWN_OPTIONS.map((opt, i) => {
              if (i === 3) {
                return (
                  <Fragment key="after-3">
                    <div
                      role="option"
                      aria-disabled="true"
                      tabIndex={-1}
                      className="aef-dropdown-item aef-dropdown-item--loading"
                    >
                      <Loader2 size={13} className="aef-spin" aria-hidden />
                      {opt.label}
                    </div>
                    <div className="aef-dropdown-divider" />
                  </Fragment>
                );
              }
              const isDisabled    = opt.kind === 'disabled';
              const isDestructive = opt.kind === 'destructive';
              const isFocused     = focusedIdx === i;
              return (
                <div
                  key={opt.value}
                  ref={el => { itemRefs.current[i] = el; }}
                  role="option"
                  aria-selected={selected === opt.label}
                  aria-disabled={isDisabled || undefined}
                  tabIndex={isFocused && !isDisabled ? 0 : -1}
                  className={[
                    'aef-dropdown-item',
                    isDisabled    ? 'aef-dropdown-item--disabled'    : '',
                    isDestructive ? 'aef-dropdown-item--destructive' : '',
                    selected === opt.label ? 'aef-dropdown-item--selected' : '',
                  ].filter(Boolean).join(' ')}
                  onClick={() => { if (!isDisabled) activateItem(opt.label); }}
                  onKeyDown={e => {
                    if ((e.key === 'Enter' || e.key === ' ') && !isDisabled) {
                      e.preventDefault();
                      activateItem(opt.label);
                    }
                  }}
                >
                  {opt.label}
                  {selected === opt.label && <CheckCircle2 size={12} className="aef-dropdown-check" aria-hidden />}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </DemoCard>
  );
}

function PopoverDemo() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function onOut(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) { if (e.key === 'Escape') setOpen(false); }
    document.addEventListener('mousedown', onOut);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onOut);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  return (
    <DemoCard>
      <span className="ds-demo-card-label">Popover Panel</span>
      <div ref={ref} style={{ position: 'relative', display: 'inline-block' }}>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={() => setOpen(o => !o)}
          aria-expanded={open}
          aria-haspopup="true"
        >
          <Info size={13} aria-hidden /> Details
        </button>
        {open && (
          <div className="aef-popover" role="dialog" aria-label="Universe topology details">
            <div className="aef-popover-header">
              <span>Universe Topology</span>
              <button className="aef-popover-close" onClick={() => setOpen(false)} aria-label="Close popover">
                <X size={13} aria-hidden />
              </button>
            </div>
            <div className="aef-popover-body">
              <StatModule label="Nodes"    value="142" />
              <StatModule label="Edges"    value="317" />
              <StatModule label="Compiled" value="v12" />
            </div>
          </div>
        )}
      </div>
    </DemoCard>
  );
}

const MODAL_CONTENT = Array.from({ length: 12 }, (_, i) =>
  `Section ${i + 1}: Configure this parameter before committing the scenario to the deployment pipeline.`
);

function ModalDemo() {
  // Config: set to true to close the dialog when clicking the backdrop.
  // Lightweight overlays (Popover, Dropdown) close on outside click by default.
  const MODAL_DISMISS_ON_BACKDROP = false;

  const [open, setOpen] = useState(false);
  const modalRef  = useRef<HTMLDivElement>(null);
  const closeBtnRef = useRef<HTMLButtonElement>(null);

  const close = useCallback(() => setOpen(false), []);

  useEffect(() => {
    if (open && closeBtnRef.current) closeBtnRef.current.focus();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') { close(); return; }
      if (e.key === 'Tab' && modalRef.current) {
        const focusable = Array.from(
          modalRef.current.querySelectorAll<HTMLElement>(
            'button:not([disabled]), [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
          )
        );
        const first = focusable[0];
        const last  = focusable[focusable.length - 1];
        if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
        else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
      }
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, close]);

  return (
    <DemoCard>
      <span className="ds-demo-card-label">Modal / Dialog</span>
      <button className="aef-btn aef-btn-inactive" onClick={() => setOpen(true)}>
        Open dialog
      </button>
      {open && (
        <div
          className="aef-modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="modal-title"
          onClick={MODAL_DISMISS_ON_BACKDROP ? (e => { if (e.target === e.currentTarget) close(); }) : undefined}
        >
          <div ref={modalRef} className="aef-modal">
            <div className="aef-modal-header">
              <span id="modal-title" className="aef-modal-title">Scenario Settings</span>
              <button ref={closeBtnRef} className="aef-modal-close" onClick={close} aria-label="Close dialog">
                <X size={16} aria-hidden />
              </button>
            </div>
            <div className="aef-modal-body">
              {MODAL_CONTENT.map((t, i) => (
                <p key={i} className="aef-modal-para">{t}</p>
              ))}
            </div>
            <div className="aef-modal-footer">
              <button className="aef-btn aef-btn-inactive" onClick={close}>Cancel</button>
              <button className="aef-btn aef-btn-active"   onClick={close}>Save changes</button>
            </div>
          </div>
        </div>
      )}
    </DemoCard>
  );
}

interface ToastItem { id: number; message: string; type: 'success' | 'warning' | 'error'; }

function ToastDemo() {
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const nextId = useRef(0);

  const addToast = useCallback((message: string, type: ToastItem['type']) => {
    const id = ++nextId.current;
    setToasts(t => [...t, { id, message, type }]);
    setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), 3200);
  }, []);

  return (
    <DemoCard>
      <span className="ds-demo-card-label">Toast Notifications</span>
      <div className="ds-row">
        <button className="aef-btn aef-btn-inactive" onClick={() => addToast('Compile complete.', 'success')}>
          Success
        </button>
        <button className="aef-btn aef-btn-inactive" onClick={() => addToast('3 violations detected.', 'warning')}>
          Warning
        </button>
        <button className="aef-btn aef-btn-inactive" onClick={() => addToast('Deploy failed.', 'error')}>
          Error
        </button>
      </div>
      <div className="aef-toast-container" aria-live="polite" aria-atomic="false">
        {toasts.map(t => (
          <div key={t.id} className={`aef-toast aef-toast--${t.type}`} role="status">
            {t.type === 'success' && <CheckCircle2 size={14} aria-hidden />}
            {t.type === 'warning' && <AlertTriangle size={14} aria-hidden />}
            {t.type === 'error'   && <X size={14} aria-hidden />}
            <span>{t.message}</span>
          </div>
        ))}
      </div>
    </DemoCard>
  );
}

const CMD_ITEMS: Array<{ label: string; icon: ElementType; kbd: string }> = [
  { label: 'Compile Universe',   icon: Cpu,        kbd: 'Ctrl+B' },
  { label: 'Deploy Scenario',    icon: Rocket,     kbd: 'Ctrl+D' },
  { label: 'Open Runtime View',  icon: Activity,   kbd: 'Ctrl+R' },
  { label: 'Export Report',      icon: FileText,   kbd: 'Ctrl+E' },
  { label: 'Open Flow Builder',  icon: GitBranch,  kbd: 'Ctrl+F' },
  { label: 'Trust Envelope',     icon: ShieldCheck, kbd: 'Ctrl+T' },
  { label: 'Settings',           icon: Settings,   kbd: 'Ctrl+,' },
  { label: 'Search Entities',    icon: Search,     kbd: 'Ctrl+/' },
];

function CommandPaletteDemo() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  // -1 means input is focused, 0+ is item index in filtered list
  const [focusedResultIdx, setFocusedResultIdx] = useState(-1);
  const inputRef   = useRef<HTMLInputElement>(null);
  const resultRefs = useRef<(HTMLDivElement | null)[]>([]);
  const close = useCallback(() => { setOpen(false); setQuery(''); setFocusedResultIdx(-1); }, []);

  const filtered = CMD_ITEMS.filter(c => c.label.toLowerCase().includes(query.toLowerCase()));

  // On open or query change: reset focus to input
  useEffect(() => {
    if (open) { inputRef.current?.focus(); setFocusedResultIdx(-1); }
  }, [open, query]);

  const handleInputKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape') { e.preventDefault(); close(); return; }
    if (e.key === 'ArrowDown' && filtered.length > 0) {
      e.preventDefault();
      setFocusedResultIdx(0);
      requestAnimationFrame(() => resultRefs.current[0]?.focus());
    } else if (e.key === 'End' && filtered.length > 0) {
      e.preventDefault();
      const lastIdx = filtered.length - 1;
      setFocusedResultIdx(lastIdx);
      requestAnimationFrame(() => resultRefs.current[lastIdx]?.focus());
    }
  };

  const handleResultKeyDown = (e: React.KeyboardEvent, idx: number) => {
    if (e.key === 'Escape')     { e.preventDefault(); close(); return; }
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); close(); return; }
    if (e.key === 'ArrowDown')  {
      e.preventDefault();
      if (idx < filtered.length - 1) {
        setFocusedResultIdx(idx + 1);
        resultRefs.current[idx + 1]?.focus();
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (idx === 0) {
        setFocusedResultIdx(-1);
        inputRef.current?.focus();
      } else {
        setFocusedResultIdx(idx - 1);
        resultRefs.current[idx - 1]?.focus();
      }
    } else if (e.key === 'Home') {
      e.preventDefault();
      setFocusedResultIdx(-1);
      inputRef.current?.focus();
    } else if (e.key === 'End') {
      e.preventDefault();
      const lastIdx = filtered.length - 1;
      setFocusedResultIdx(lastIdx);
      resultRefs.current[lastIdx]?.focus();
    }
  };

  return (
    <DemoCard>
      <span className="ds-demo-card-label">Command Palette</span>
      <button className="aef-btn aef-btn-inactive" onClick={() => setOpen(true)}>
        Open palette <kbd className="aef-cmd-kbd">Ctrl+K</kbd>
      </button>
      {open && (
        <div
          className="aef-cmd-overlay"
          role="dialog"
          aria-label="Command palette"
          onClick={e => { if (e.target === e.currentTarget) close(); }}
        >
          <div className="aef-cmd-palette">
            <div className="aef-cmd-search">
              <Search size={14} aria-hidden />
              <input
                ref={inputRef}
                className="aef-cmd-input"
                value={query}
                onChange={e => setQuery(e.target.value)}
                placeholder="Search commands..."
                aria-label="Search commands"
                aria-controls="aef-cmd-results"
                aria-autocomplete="list"
                onKeyDown={handleInputKeyDown}
              />
            </div>
            <div id="aef-cmd-results" className="aef-cmd-results" role="listbox" aria-label="Command results">
              {filtered.length > 0 ? filtered.map((cmd, i) => {
                const CmdIcon = cmd.icon;
                const isFocused = focusedResultIdx === i;
                return (
                  <div
                    key={cmd.label}
                    ref={el => { resultRefs.current[i] = el; }}
                    className={`aef-cmd-item${isFocused ? ' aef-cmd-item--focused' : ''}`}
                    role="option"
                    aria-selected={isFocused}
                    tabIndex={isFocused ? 0 : -1}
                    onClick={close}
                    onKeyDown={e => handleResultKeyDown(e, i)}
                  >
                    <CmdIcon size={14} aria-hidden />
                    <span className="aef-cmd-label">{cmd.label}</span>
                    <kbd className="aef-cmd-kbd">{cmd.kbd}</kbd>
                  </div>
                );
              }) : (
                <div className="aef-cmd-empty">No results</div>
              )}
            </div>
          </div>
        </div>
      )}
    </DemoCard>
  );
}

const CTX_NORMAL_ITEMS: Array<{ label: string; icon: ElementType; kbd: string }> = [
  { label: 'Add Entity', icon: Plus,     kbd: 'A' },
  { label: 'Edit Node',  icon: Edit3,    kbd: 'E' },
  { label: 'Copy',       icon: Copy,     kbd: 'Ctrl+C' },
  { label: 'Export',     icon: Download, kbd: 'Ctrl+E' },
];
const CTX_DESTRUCTIVE: Array<{ label: string; icon: ElementType; kbd: string }> = [
  { label: 'Delete',     icon: Trash2,   kbd: 'Del' },
];

function ContextMenuDemo() {
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const surfaceRef  = useRef<HTMLDivElement>(null);
  const menuRef     = useRef<HTMLDivElement>(null);
  const itemRefs    = useRef<(HTMLDivElement | null)[]>([]);
  // All items in visual order: normal items then destructive
  const allItems    = [...CTX_NORMAL_ITEMS, ...CTX_DESTRUCTIVE];
  const [focusedMenuIdx, setFocusedMenuIdx] = useState(0);

  const closeMenu = useCallback(() => {
    setMenu(null);
    setFocusedMenuIdx(0);
    requestAnimationFrame(() => surfaceRef.current?.focus());
  }, []);

  const openMenu = useCallback((x: number, y: number) => {
    setMenu({ x, y });
    setFocusedMenuIdx(0);
  }, []);

  const handleContextMenu = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    e.preventDefault();
    openMenu(e.clientX, e.clientY);
  }, [openMenu]);

  // Focus first item when menu opens
  useEffect(() => {
    if (menu) {
      requestAnimationFrame(() => itemRefs.current[0]?.focus());
    }
  }, [menu]);

  // Outside click to close
  useEffect(() => {
    if (!menu) return;
    function onDown(e: MouseEvent) {
      if (!menuRef.current?.contains(e.target as Node)) closeMenu();
    }
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [menu, closeMenu]);

  const handleMenuKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); closeMenu(); return; }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      const next = Math.min(focusedMenuIdx + 1, allItems.length - 1);
      setFocusedMenuIdx(next);
      itemRefs.current[next]?.focus();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      const prev = Math.max(focusedMenuIdx - 1, 0);
      setFocusedMenuIdx(prev);
      itemRefs.current[prev]?.focus();
    } else if (e.key === 'Home') {
      e.preventDefault();
      setFocusedMenuIdx(0);
      itemRefs.current[0]?.focus();
    } else if (e.key === 'End') {
      e.preventDefault();
      const last = allItems.length - 1;
      setFocusedMenuIdx(last);
      itemRefs.current[last]?.focus();
    }
  }, [focusedMenuIdx, allItems.length, closeMenu]);

  return (
    <DemoCard style={{ gridColumn: 'span 2' }}>
      <span className="ds-demo-card-label">Right-click Context Menu (Paryty work surface only)</span>
      <div
        ref={surfaceRef}
        className="aef-context-surface"
        onContextMenu={handleContextMenu}
        role="region"
        aria-label="Work surface - right-click or Shift+F10 for actions"
        tabIndex={0}
        onKeyDown={e => {
          if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
            e.preventDefault();
            const rect = surfaceRef.current!.getBoundingClientRect();
            openMenu(rect.left + 16, rect.top + 16);
          }
        }}
      >
        <Map size={20} className="aef-context-surface__icon" aria-hidden />
        <span>Right-click or Shift+F10 inside this canvas surface</span>
        <span className="aef-context-surface__hint">Native right-click is not suppressed outside this surface</span>
      </div>
      {menu && (
        <div
          ref={menuRef}
          className="aef-context-menu"
          role="menu"
          aria-label="Surface actions"
          style={{ position: 'fixed', left: menu.x, top: menu.y }}
          onKeyDown={handleMenuKeyDown}
        >
          {CTX_NORMAL_ITEMS.map((item, i) => {
            const ItemIcon = item.icon;
            return (
              <div
                key={item.label}
                ref={el => { itemRefs.current[i] = el; }}
                className="aef-context-item"
                role="menuitem"
                tabIndex={focusedMenuIdx === i ? 0 : -1}
                onClick={closeMenu}
                onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); closeMenu(); } }}
              >
                <ItemIcon size={13} aria-hidden />
                <span>{item.label}</span>
                <kbd className="aef-context-kbd">{item.kbd}</kbd>
              </div>
            );
          })}
          <div className="aef-context-divider" />
          {CTX_DESTRUCTIVE.map((item, di) => {
            const globalIdx = CTX_NORMAL_ITEMS.length + di;
            const ItemIcon  = item.icon;
            return (
              <div
                key={item.label}
                ref={el => { itemRefs.current[globalIdx] = el; }}
                className="aef-context-item aef-context-item--destructive"
                role="menuitem"
                tabIndex={focusedMenuIdx === globalIdx ? 0 : -1}
                onClick={closeMenu}
                onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); closeMenu(); } }}
              >
                <ItemIcon size={13} aria-hidden />
                <span>{item.label}</span>
                <kbd className="aef-context-kbd">{item.kbd}</kbd>
              </div>
            );
          })}
        </div>
      )}
    </DemoCard>
  );
}

function MenusSection() {
  return (
    <Section title="Dropdowns, Popups & Menus">
      <div className="ds-menus-grid">
        <DropdownDemo />
        <PopoverDemo />
        <ModalDemo />
        <ToastDemo />
        <CommandPaletteDemo />
        <ContextMenuDemo />
      </div>
    </Section>
  );
}

/* ============================================================
   SECTION 15 ? Animated Navigation
   ============================================================ */

const NAV_ITEM_H   = 34;
const NAV_ITEM_GAP = 2;
const NAV_PAD_TOP  = 8;

function AnimatedNavSection() {
  const [expanded, setExpanded] = useState(true);
  const [activeIdx, setActiveIdx] = useState(1);
  /* Pill top = padding + index * (item height + gap) */
  const pillTop = NAV_PAD_TOP + activeIdx * (NAV_ITEM_H + NAV_ITEM_GAP);

  return (
    <Section title="Animated Navigation">
      <div className="ds-anim-nav-demo">
        <div style={{ display: 'flex', gap: 8, marginBottom: 12, alignItems: 'center', flexWrap: 'wrap' }}>
          <button
            className="aef-btn aef-btn-inactive"
            onClick={() => setExpanded(e => !e)}
            aria-label={expanded ? 'Collapse sidebar' : 'Expand sidebar'}
          >
            <PanelLeft size={14} aria-hidden />
            {expanded ? 'Collapse' : 'Expand'}
          </button>
          <span className="ds-token-label">Click nav items to glide the active pill</span>
        </div>
        <aside
          className="aef-anim-sidebar"
          style={{ width: expanded ? 'var(--aef-sidebar-width)' : 'var(--aef-sidebar-collapsed)' }}
          aria-label="Animated sidebar demo"
        >
          {/* Brand */}
          <div className="ds-sidebar__brand" style={{ overflow: 'hidden', flexShrink: 0 }}>
            {expanded ? (
              <span className="ds-sidebar__brand-name" style={{ opacity: 1, transition: `opacity var(--aef-duration-fast) var(--aef-ease-exit)` }}>
                Paryty
              </span>
            ) : (
              <span className="ds-sidebar__brand-mark" style={{ display: 'block', textAlign: 'center' }}>PT</span>
            )}
          </div>

          {/* Nav with gliding pill */}
          <nav
            style={{
              position: 'relative',
              padding: `${NAV_PAD_TOP}px 4px`,
              display: 'flex',
              flexDirection: 'column',
              gap: `${NAV_ITEM_GAP}px`,
              flex: 1,
              overflow: 'hidden',
            }}
            aria-label="Primary navigation"
          >
            {/* Gliding selector -- pill (expanded) or circle (collapsed) */}
            <div
              className="aef-nav-glidepill"
              style={{
                position: 'absolute',
                top: pillTop,
                left: 4,
                // expanded: pill spanning full nav row width; collapsed: circle behind icon only
                width: expanded ? 'calc(100% - 8px)' : NAV_ITEM_H,
                height: NAV_ITEM_H,
                borderRadius: expanded ? 'var(--aef-radius-full)' : '50%',
              }}
              aria-hidden
            />
            {PRIMARY_NAV.map((item, i) => (
              <div
                key={item.label}
                className={`aef-anim-nav-item${i === activeIdx ? ' aef-anim-nav-item--active' : ''}`}
                style={{ height: NAV_ITEM_H }}
                onClick={() => setActiveIdx(i)}
                role="button"
                aria-pressed={i === activeIdx}
                tabIndex={0}
                onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') setActiveIdx(i); }}
                aria-label={item.label}
                title={!expanded ? item.label : undefined}
              >
                <span className="aef-anim-nav-icon">{item.icon}</span>
                <span
                  className="aef-anim-nav-label"
                  style={{ opacity: expanded ? 1 : 0, width: expanded ? 'auto' : 0 }}
                  aria-hidden={!expanded}
                >
                  {item.label}
                </span>
              </div>
            ))}
          </nav>

          {/* Settings */}
          <div className="ds-sidebar__bottom" style={{ flexShrink: 0 }}>
            <div
              className="aef-anim-nav-item"
              style={{ height: NAV_ITEM_H }}
              role="button"
              tabIndex={0}
              aria-label="Settings"
              title={!expanded ? 'Settings' : undefined}
            >
              <span className="aef-anim-nav-icon"><Settings size={16} /></span>
              <span
                className="aef-anim-nav-label"
                style={{ opacity: expanded ? 1 : 0, width: expanded ? 'auto' : 0 }}
                aria-hidden={!expanded}
              >
                Settings
              </span>
            </div>
          </div>
        </aside>
      </div>
    </Section>
  );
}

/* ============================================================
   SECTION 16 ? Motion System
   ============================================================ */

const MOTION_TOKEN_GROUPS = [
  {
    group: 'Duration',
    tokens: [
      { name: '--aef-duration-instant',  value: '60ms',  desc: 'immediate feedback' },
      { name: '--aef-duration-fast',     value: '120ms', desc: 'quick transitions' },
      { name: '--aef-duration-standard', value: '220ms', desc: 'default transitions' },
      { name: '--aef-duration-slow',     value: '380ms', desc: 'large surface changes' },
    ],
  },
  {
    group: 'Easing',
    tokens: [
      { name: '--aef-ease-emphasized', value: 'cubic-bezier(0.2, 0, 0, 1)',       desc: 'enter / expand' },
      { name: '--aef-ease-settle',     value: 'cubic-bezier(0.34, 1.56, 0.64, 1)', desc: 'spring settle' },
      { name: '--aef-ease-exit',       value: 'cubic-bezier(0.4, 0, 1, 1)',        desc: 'exit / collapse' },
      { name: '--aef-ease-linear',     value: 'linear',                            desc: 'looping / progress' },
    ],
  },
];

function MotionSection() {
  const [loadingActive, setLoadingActive] = useState(false);
  const [enterKey, setEnterKey] = useState(0);

  const triggerLoading = () => {
    setLoadingActive(true);
    setTimeout(() => setLoadingActive(false), 2000);
  };

  const triggerEnter = () => setEnterKey(k => k + 1);

  return (
    <Section title="Motion System">
      {MOTION_TOKEN_GROUPS.map(group => (
        <div key={group.group} className="ds-motion-token-group">
          <span className="ds-token-label" style={{ marginBottom: 8, display: 'block' }}>{group.group}</span>
          <div className="ds-motion-token-row">
            {group.tokens.map(t => (
              <div key={t.name} className="ds-motion-token-cell">
                <span className="ds-token-label">{t.name}</span>
                <span className="ds-token-value">{t.value}</span>
                <span className="ds-radius-desc">{t.desc}</span>
              </div>
            ))}
          </div>
        </div>
      ))}

      <div className="ds-motion-demos">
        <span className="ds-token-label" style={{ marginBottom: 8, display: 'block' }}>Interactive demos</span>
        <div className="ds-motion-demo-row">
          <div className="ds-motion-demo-cell">
            <div className="aef-motion-hover-lift aef-motion-demo-box" role="presentation">Hover lift</div>
          </div>
          <div className="ds-motion-demo-cell">
            <button className="aef-motion-press-depth aef-motion-demo-box">Press depth</button>
          </div>
          <div className="ds-motion-demo-cell">
            <button className="aef-motion-focus-ring aef-motion-demo-box">Focus ring</button>
          </div>
          <div className="ds-motion-demo-cell">
            <button
              className={`aef-motion-demo-box${loadingActive ? ' aef-motion-loading' : ''}`}
              onClick={triggerLoading}
              aria-label={loadingActive ? 'Loading...' : 'Trigger loading state'}
            >
              {loadingActive
                ? <Loader2 size={16} className="aef-spin" aria-hidden />
                : 'Loading'}
            </button>
          </div>
          <div className="ds-motion-demo-cell">
            <button className="aef-motion-demo-box" onClick={triggerEnter} aria-label="Trigger panel enter animation">
              {enterKey > 0
                ? <span className="aef-motion-enter-el" key={enterKey}>Panel enter</span>
                : <span style={{ opacity: 0.4 }}>Tap to enter</span>
              }
            </button>
          </div>
          <div className="ds-motion-demo-cell">
            <div className="aef-motion-demo-box" role="presentation">
              <span className="aef-motion-state-dot" aria-label="Live status pulse" />
              <span style={{ marginLeft: 6 }}>State pulse</span>
            </div>
          </div>
        </div>
      </div>
    </Section>
  );
}


export function DesignSystemPage() {
  return (
    <div className="ds-root">
      {/* Page header */}
      <header className="ds-page-header">
        <div>
          <h1 className="ds-page-header__title">Paryty</h1>
          <p className="ds-page-header__sub">Design System - V1.0</p>
        </div>
        <span className="ds-page-header__badge">Design Reference</span>
      </header>

      <main className="ds-main">

        {/* 1 - Color tokens */}
        <Section title="Color Tokens">
          <div className="ds-token-grid">
            <TokenSwatch label="--aef-bg"             value="#000000" border />
            <TokenSwatch label="--aef-surface-low"    value="#080808" border />
            <TokenSwatch label="--aef-surface-mid"    value="#101010" border />
            <TokenSwatch label="--aef-surface-card"   value="#141414" border />
            <TokenSwatch label="--aef-border"         value="#212121" border />
            <TokenSwatch label="--aef-text-primary"   value="#ffffff" border />
            <TokenSwatch label="--aef-text-secondary" value="#656565" border />
            <TokenSwatch label="--aef-selected-bg"    value="#ffffff" border />
            <TokenSwatch label="--aef-counter-variant-a" value="#4719FF" />
            <TokenSwatch label="--aef-counter-variant-b" value="#DC4714" />
          </div>
        </Section>

        {/* 2 - Typography */}
        <Section title="Typography">
          <div className="ds-type-specimens">
            <div>
              <p className="ds-type-label">Heading / Title - Geist Variable</p>
              <p className="ds-type-heading">Universe Builder</p>
            </div>
            <div>
              <p className="ds-type-label">Body - IBM Plex Mono</p>
              <p className="ds-type-body">Compile the universe before deployment. 1,024 entities resolved.</p>
            </div>
            <div>
              <p className="ds-type-label">Secondary text</p>
              <p className="ds-type-secondary">Define the physical topology and compile before deployment.</p>
            </div>
          </div>
        </Section>

        {/* 3 - Navigation states */}
        <Section title="Navigation - Expanded">
          <div className="ds-nav-preview-row">
            <ExpandedSidebar />
            <div className="ds-annotation">
              <p>Active item: white pill, black text/icon.</p>
              <p>Inactive item: transparent bg, white text/icon.</p>
              <p>Sidebar width: 244px. Border: 1px solid #212121.</p>
            </div>
          </div>
        </Section>

        {/* 4 - Collapsed nav */}
        <Section title="Navigation - Collapsed (Icon-Only Derivative)">
          <div className="ds-nav-preview-row">
            <CollapsedSidebar />
            <div className="ds-annotation">
              <p>Collapsed width: 56px. Same active-pill logic, label hidden.</p>
              <p>Icon-only state keeps the same active-pill behavior with labels hidden.</p>
            </div>
          </div>
        </Section>

        {/* 5 - Button states */}
        <Section title="Button States">
          <div className="ds-row">
            <AefButton active>Compile Universe</AefButton>
            <AefButton>Deploy Scenario</AefButton>
            <AefButton>Export Report</AefButton>
          </div>
          <div className="ds-annotation" style={{ marginTop: 12 }}>
            <p>Active: bg #ffffff, no border, text #000000.</p>
            <p>Inactive: bg #080808, border 1px solid #212121, text #ffffff.</p>
          </div>
        </Section>

        {/* 6 - Counter cards */}
        <Section title="Counter Cards">
          <div className="ds-counter-row">
            <CounterCard label="Entities"   value="142" icon={<Cpu size={12} />} variant="neutral" />
            <CounterCard label="Compiled"   value="139" icon={<CheckCircle2 size={12} />} variant="active" />
            <CounterCard label="Flows"      value="28"  icon={<GitBranch size={12} />} variant="neutral" />
            <CounterCard label="Violations" value="3"   icon={<AlertTriangle size={12} />} variant="variant-b" />
            <CounterCard label="Reports"    value="12"  icon={<FileText size={12} />} variant="variant-a" />
            <CounterCard label="Scenarios"  value="7"   icon={<Sliders size={12} />} variant="neutral" />
          </div>
        </Section>

        {/* 7 - Progress & indicators */}
        <Section title="Progress & Indicator Dots">
          <ProgressDemo />
          <IndicatorDots />
        </Section>

        {/* 8 - Table Card */}
        <Section title="Table Card">
          <div style={{ maxWidth: 560 }}>
            <TableCard />
          </div>
        </Section>

        {/* 9 - Container card */}
        <Section title="Container Card">
          <div className="ds-container-card-demo">
            <ContainerCard title="Universe Topology" icon={<Globe size={14} />} metaLabel="Compiled">
              <VizWell label="Visualization viewport" />
              <div className="ds-preview-stat-row" style={{ marginTop: 8 }}>
                <StatModule label="Nodes" value="142" />
                <StatModule label="Edges" value="317" />
                <StatModule label="Compile" value="v12" />
              </div>
            </ContainerCard>
            <ContainerCard title="Runtime Metrics" icon={<Activity size={14} />}>
              <StatModule label="Tick Rate" value="60 Hz" />
              <StatModule label="Latency"   value="3.2 ms" />
              <StatModule label="Epoch"     value="#0044" />
            </ContainerCard>
          </div>
        </Section>

        {/* 10 - Full dashboard preview */}
        <Section title="Dashboard Preview (Full Layout)">
          <div className="ds-dashboard-outer">
            <DashboardPreview />
          </div>
        </Section>

        {/* 11 - Radius Mathematics */}
        <RadiusSection />

        {/* 12 - Proprietary Scroll */}
        <ScrollSection />

        {/* 13 - Icon Library */}
        <IconLibrarySection />

        {/* 14 - Dropdowns, Popups & Menus */}
        <MenusSection />

        {/* 15 - Animated Navigation */}
        <AnimatedNavSection />

        {/* 16 - Motion System */}
        <MotionSection />

      </main>
    </div>
  );
}




