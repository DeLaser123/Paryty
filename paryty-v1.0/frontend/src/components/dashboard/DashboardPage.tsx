/**
 * DashboardPage — Redesigned dashboard with counter strip, two-pane layout,
 * accordion sections, forecast pane, and twin detail sub-view.
 *
 * Layout:
 *   ┌──────────────────────────────────────────────────┐
 *   │ Page Header (title + New Digital Paryty button)  │
 *   ├──────────────────────────────────────────────────┤
 *   │ Counter Strip (horizontal scroll, no indicator)   │
 *   ├────────────┬─────────────────────────────────────┤
 *   │ Info Pane  │ Main Pane (accordion sections)      │
 *   │ (forecasts)│  1. Your Digital Parytys             │
 *   │            │  2. Top Agents                       │
 *   │            │  3. Recent Alerts                    │
 *   │            │  4. Metric Aggregations              │
 *   └────────────┴─────────────────────────────────────┘
 *
 * All styling uses --aef-* tokens from the Paryty Design System.
 * No hard-coded hex values. Fully data-driven.
 */

import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  Plus, Globe, BarChart2, Bell, Activity, TrendingUp, FlaskConical,
  ArrowUpRight, CheckCircle2, X, Box, AlertTriangle,
  Server, Heart, AlertCircle, ChevronDown, ArrowLeft,
  Cpu, Shield, ExternalLink, Eye, Check,
  HardDrive, Network,
} from 'lucide-react';
import clsx from 'clsx';

import { useDashboardStore } from '../../stores/dashboardStore';
import { usePlanStore } from '../../stores/planStore';
import { useAlertsStore } from '../../stores/alertsStore';
import { useIntelStore } from '../../stores/intelStore';
import { useTwinStore } from '../../stores/twinStore';
import { getRestClient } from '../../api/rest';
import { useToastStore } from '../../stores/toastStore';
import { ABILITY_CATALOGUE, ABILITY_FEATURE_MAP } from '../../types/ability';
import { ParytySelect } from '../common/ParytySelect';
import { useIntel } from '../../hooks/useIntel';
import { useDashboardPolling } from '../../hooks/useDashboardPolling';
import type { DigitalParyty } from '../../types/digitalParyty';
import type { AbilityMeta } from '../../types/ability';
import { AgentDetailModal } from '../agent/AgentDetailModal';
import { DetachableCard } from '../common/DetachableCard';
import type { HealthStatus, AgentManagementInfo } from '../../types/agent';
import type { Alert, AlertSeverity } from '../../types/alert';
import './dashboard.css';

// ─── Helpers ──────────────────────────────────────────────────────────────

function abilityIcon(iconName: string, size = 14): ReactNode {
  const icons: Record<string, ReactNode> = {
    Globe:        <Globe size={size} />,
    BarChart2:    <BarChart2 size={size} />,
    Bell:         <Bell size={size} />,
    Activity:     <Activity size={size} />,
    TrendingUp:   <TrendingUp size={size} />,
    FlaskConical: <FlaskConical size={size} />,
  };
  return icons[iconName] ?? <Box size={size} />;
}

function severityColor(severity: AlertSeverity): string {
  switch (severity) {
    case 'critical': return 'var(--aef-counter-variant-b)';
    case 'warning':  return 'var(--aef-status-warning)';
    default:         return 'var(--aef-transport-rest)';
  }
}

function severityBadgeClass(severity: AlertSeverity): string {
  switch (severity) {
    case 'critical': return 'badge-warning';
    case 'warning':  return 'badge-warning';
    default:         return 'badge-pending';
  }
}

function healthBadgeClass(status: HealthStatus): string {
  switch (status) {
    case 'healthy':   return 'badge-valid';
    case 'degraded':  return 'badge-warning';
    case 'unhealthy': return 'badge-warning';
    default:          return 'badge-pending';
  }
}

// ─── Health dot ───────────────────────────────────────────────────────────

function HealthDot({ status }: { status: HealthStatus }) {
  return (
    <span
      className={clsx('dp-card__health-dot', `dp-card__health-dot--${status}`)}
      aria-label={`Health: ${status}`}
    />
  );
}

// ─── Ability badge ────────────────────────────────────────────────────────

function AbilityBadge({ ability }: { ability: AbilityMeta }) {
  return (
    <span className="dp-ability-badge">
      {abilityIcon(ability.iconName)}
      {ability.name}
    </span>
  );
}

// ─── Counter card (interactive) ─────────────────────────────────────────

function CounterCard({
  icon, label, value, variant, onClick,
}: {
  icon: ReactNode;
  label: string;
  value: string | number;
  variant?: string;
  onClick?: () => void;
}) {
  return (
    <div
      className={clsx('aef-counter', variant)}
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onClick?.(); } }}
      data-testid={`counter-${label.toLowerCase().replace(/\s+/g, '-')}`}
    >
      <div className="aef-counter__icon">{icon}</div>
      <div className="aef-counter__body">
        <span className="aef-counter__label">{label}</span>
        <span className="aef-counter__value">{value}</span>
      </div>
    </div>
  );
}

// ─── Accordion section ────────────────────────────────────────────────────

function AccordionSection({
  icon, title, meta, isOpen, onToggle, children, footer,
}: {
  icon: ReactNode;
  title: string;
  meta?: string | number;
  isOpen: boolean;
  onToggle: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <div className={clsx('dp-accordion-section', isOpen && 'dp-accordion-section--open')}>
      <div
        className="dp-accordion-header"
        onClick={onToggle}
        role="button"
        tabIndex={0}
        aria-expanded={isOpen}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onToggle(); } }}
        data-testid={`accordion-header-${title.toLowerCase().replace(/\s+/g, '-')}`}
      >
        <span className="dp-accordion-header__icon">{icon}</span>
        <span className="dp-accordion-header__title">{title}</span>
        {meta !== undefined && <span className="dp-accordion-header__meta">{meta}</span>}
        <span className="dp-accordion-header__chevron"><ChevronDown size={14} /></span>
      </div>
      <div className="dp-accordion-body">
        <div className="dp-accordion-body__inner">
          {children}
          {footer}
        </div>
      </div>
    </div>
  );
}

// ─── Digital Paryty card (container-card based) ──────────────────────────

function DigitalParytyCard({
  paryty, isActive, onSelect,
}: {
  paryty: DigitalParyty;
  isActive: boolean;
  onSelect: (id: string) => void;
}) {
  const enabledMetas = paryty.abilities
    .map((a) => ABILITY_CATALOGUE.find((m) => m.id === a.id))
    .filter((m): m is AbilityMeta => m !== undefined);

  const handleClick = useCallback(() => onSelect(paryty.id), [onSelect, paryty.id]);

  const created = new Date(paryty.createdAt).toLocaleDateString('en-US', {
    month: 'short', day: 'numeric', year: 'numeric',
  });

  return (
    <article
      className={clsx('aef-container-card dp-card', isActive && 'dp-card--active')}
      tabIndex={0}
      role="button"
      aria-label={`Open ${paryty.name}`}
      onClick={handleClick}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); handleClick(); } }}
      data-testid={`twin-card-${paryty.id}`}
    >
      <div className="aef-container-card__body dp-card__inner">
        {/* Header row */}
        <div className="dp-card__head">
          <HealthDot status={paryty.health} />
          <div className="dp-card__title-block">
            <div>
              <div className="dp-card__name">{paryty.name}</div>
              <div className="dp-card__system">{paryty.systemLabel}</div>
            </div>
            <span className="dp-active-pill" data-testid="active-pill">
              <span className="dp-active-dot" />
              Active
            </span>
          </div>
        </div>

        {/* Summary stats */}
        {(paryty.summary.nodeCount !== undefined ||
          paryty.summary.activeAlerts !== undefined ||
          paryty.summary.activeTraces !== undefined) && (
          <div className="dp-card__stats">
            {paryty.summary.nodeCount !== undefined && (
              <div className="dp-stat">
                <span className="dp-stat__label">Nodes</span>
                <span className="dp-stat__value">{paryty.summary.nodeCount}</span>
              </div>
            )}
            {paryty.summary.activeTraces !== undefined && (
              <div className="dp-stat">
                <span className="dp-stat__label">Traces</span>
                <span className="dp-stat__value">{paryty.summary.activeTraces}</span>
              </div>
            )}
            {paryty.summary.activeAlerts !== undefined && (
              <div className="dp-stat">
                <span className="dp-stat__label">Alerts</span>
                <span className="dp-stat__value">{paryty.summary.activeAlerts}</span>
              </div>
            )}
            {paryty.summary.forecastHorizonDays !== undefined && (
              <div className="dp-stat">
                <span className="dp-stat__label">Forecast</span>
                <span className="dp-stat__value">{paryty.summary.forecastHorizonDays}d</span>
              </div>
            )}
          </div>
        )}

        {/* Ability badges */}
        {enabledMetas.length > 0 && (
          <div className="dp-card__abilities" aria-label="Enabled abilities">
            {enabledMetas.map((m) => <AbilityBadge key={m.id} ability={m} />)}
          </div>
        )}

        {/* Footer */}
        <div className="dp-card__footer">
          <span className="dp-card__date">Created {created}</span>
          <button className="dp-card__open-btn" tabIndex={-1} aria-hidden>
            Open <ArrowUpRight size={12} />
          </button>
        </div>
      </div>
    </article>
  );
}

// ─── Agent row ────────────────────────────────────────────────────────────

function AgentRow({ agent, onClick }: { agent: AgentManagementInfo; onClick: () => void }) {
  const timeAgo = (() => {
    if (!agent.last_seen) return 'Never';
    const diff = Date.now() - new Date(agent.last_seen).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return 'Just now';
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    return `${Math.floor(hours / 24)}d ago`;
  })();

  return (
    <div
      className="dp-agent-row"
      data-testid={`agent-row-${agent.agent_id}`}
      onClick={onClick}
      style={{ cursor: 'pointer' }}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onClick(); } }}
    >
      <span className="dp-agent-row__icon"><Server size={14} /></span>
      <div className="dp-agent-row__info">
        <div className="dp-agent-row__name">{agent.name || agent.hostname || 'Unnamed Agent'}</div>
        <div className="dp-agent-row__meta">{agent.hostname} · {agent.os} · {timeAgo}</div>
      </div>
      <span className={`aef-badge ${agent.status === 'deployed' ? 'badge-valid' : 'badge-pending'}`}>
        {agent.status}
      </span>
    </div>
  );
}

// ─── Alert row ────────────────────────────────────────────────────────────

function AlertRow({ alert }: { alert: Alert }) {
  const severityClass = alert.severity === 'critical'
    ? 'dp-alert-row--critical'
    : alert.severity === 'warning'
      ? 'dp-alert-row--warning'
      : 'dp-alert-row--info';

  const SeverityIcon = alert.severity === 'critical'
    ? AlertTriangle
    : alert.severity === 'warning'
      ? AlertCircle
      : Bell;

  const twinName = alert.labels?.twin_name || alert.labels?.twin || alert.labels?.tenant_id || 'Unknown Twin';
  const agentName = alert.labels?.agent_id || alert.labels?.hostname || 'Unknown Agent';

  const timeAgo = (() => {
    const diff = Date.now() - new Date(alert.startedAt).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return 'Just now';
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    return `${Math.floor(hours / 24)}d ago`;
  })();

  return (
    <div className={clsx('dp-alert-row', severityClass)} data-testid={`alert-row-${alert.id}`}>
      <span className="dp-alert-row__icon" style={{ color: severityColor(alert.severity) }}>
        <SeverityIcon size={14} />
      </span>
      <div className="dp-alert-row__body">
        <div className="dp-alert-row__title">{alert.ruleName}</div>
        <div className="dp-alert-row__detail">
          Value: {alert.value} (threshold: {alert.threshold}) · {timeAgo}
        </div>
        <div className="dp-alert-row__tags">
          <span className="aef-meta-pill"><Eye size={10} /> {twinName}</span>
          <span className="aef-meta-pill"><Server size={10} /> {agentName}</span>
        </div>
      </div>
      <span className={clsx('aef-badge', severityBadgeClass(alert.severity))}>
        {alert.severity}
      </span>
    </div>
  );
}

// ─── Metric card (per-twin aggregation) ───────────────────────────────────

function MetricCard({ twin }: { twin: DigitalParyty }) {
  return (
    <div className="dp-metric-card" data-testid={`metric-card-${twin.id}`}>
      <div className="dp-metric-card__header">
        <Server size={12} />
        {twin.name}
      </div>
      <div className="dp-metric-card__row">
        <span className="dp-metric-card__label">Health</span>
        <span className={clsx('aef-badge', healthBadgeClass(twin.health))}>{twin.health}</span>
      </div>
      <div className="dp-metric-card__row">
        <span className="dp-metric-card__label">Nodes</span>
        <span className="dp-metric-card__value">{twin.summary.nodeCount ?? '—'}</span>
      </div>
      <div className="dp-metric-card__row">
        <span className="dp-metric-card__label">Active Alerts</span>
        <span className="dp-metric-card__value">{twin.summary.activeAlerts ?? '—'}</span>
      </div>
      <div className="dp-metric-card__row">
        <span className="dp-metric-card__label">Traces</span>
        <span className="dp-metric-card__value">{twin.summary.activeTraces ?? '—'}</span>
      </div>
      {twin.summary.forecastHorizonDays !== undefined && (
        <div className="dp-metric-card__row">
          <span className="dp-metric-card__label">Forecast Horizon</span>
          <span className="dp-metric-card__value">{twin.summary.forecastHorizonDays}d</span>
        </div>
      )}
    </div>
  );
}

// ─── Info Pane ────────────────────────────────────────────────────────────

function InfoPane({
  catalogue, forecasts, anomalies,
}: {
  catalogue: DigitalParyty[];
  forecasts: ReturnType<typeof useIntelStore.getState>['forecasts'];
  anomalies: ReturnType<typeof useIntelStore.getState>['anomalies'];
}) {
  const [selectedNode, setSelectedNode] = useState('all');
  const [selectedSeverity, setSelectedSeverity] = useState('all');
  const [viewFullForecasts, setViewFullForecasts] = useState(false);
  const [viewFullInsights, setViewFullInsights] = useState(false);
  const [barAnimated, setBarAnimated] = useState(false);
  const [detailForecast, setDetailForecast] = useState<ReturnType<typeof useIntelStore.getState>['forecasts'] extends Map<string, infer V> ? V | null : never>(null);
  const [detailAnomaly, setDetailAnomaly] = useState<ReturnType<typeof useIntelStore.getState>['anomalies'] extends (infer A)[] ? A | null : never>(null);
  const navigate = useNavigate();

  const nodeOptions = [
    { label: 'All Nodes', value: 'all' },
    ...catalogue.map((t) => ({ label: t.name, value: t.id })),
  ];

  const severityOptions = [
    { label: 'All Severities', value: 'all' },
    { label: 'Critical', value: 'critical' },
    { label: 'High', value: 'high' },
    { label: 'Medium', value: 'medium' },
    { label: 'Low', value: 'low' },
    { label: 'Info', value: 'info' },
  ];

  const forecastList = Array.from(forecasts.values());
  const filteredForecasts = selectedNode === 'all'
    ? forecastList
    : forecastList.filter((f) => {
        const twin = catalogue.find((t) => t.id === selectedNode);
        return twin?.config.agentIds.includes(f.agentId);
      });

  const filteredInsights = selectedSeverity === 'all'
    ? anomalies
    : anomalies.filter((a) => a.severity === selectedSeverity);

  const PREVIEW_COUNT = 4;
  const visibleForecasts = viewFullForecasts ? filteredForecasts : filteredForecasts.slice(0, PREVIEW_COUNT);
  const visibleInsights = viewFullInsights ? filteredInsights : filteredInsights.slice(0, PREVIEW_COUNT);

  // Trigger progress bar fill animation after mount / filter change
  useEffect(() => {
    setBarAnimated(false);
    const t = requestAnimationFrame(() => setBarAnimated(true));
    return () => cancelAnimationFrame(t);
  }, [selectedNode, selectedSeverity, filteredForecasts.length, filteredInsights.length]);

  const bandClass = (pct: number) =>
    pct >= 0.7 ? 'high' : pct >= 0.4 ? 'medium' : 'low';

  const METRIC_ICON_MAP: Record<string, ReactNode> = {
    cpu_usage_percent: <Cpu size={13} />,
    memory_usage_percent: <HardDrive size={13} />,
    disk_usage_percent: <Activity size={13} />,
    network_io_bytes: <Network size={13} />,
  };
  const getMetricIcon = (metricName: string) =>
    METRIC_ICON_MAP[metricName] ?? <TrendingUp size={13} />;

  const getAnomalyIcon = (severity: string) => {
    if (severity === 'critical' || severity === 'high') return <AlertCircle size={13} />;
    if (severity === 'medium') return <AlertTriangle size={13} />;
    return <Activity size={13} />;
  };

  return (
    <>
      {/* ── Live Forecasts ─────────────────────────────────────────── */}
      <DetachableCard
        title="Live Forecasts"
        icon={<TrendingUp size={14} />}
        metaLabel={String(filteredForecasts.length)}
        headerAction={
          <div className="dp-info-card__header-select">
            <ParytySelect
              options={nodeOptions}
              value={selectedNode}
              onChange={setSelectedNode}
              placeholder="Select a node"
              testId="forecast-node-select"
            />
          </div>
        }
        className="dp-info-card"
        testId="forecast-info-card"
        footer={
          <>
            {filteredForecasts.length > PREVIEW_COUNT && (
              <button
                className={clsx('dp-info-card__footer-btn', viewFullForecasts && 'dp-info-card__footer-btn--active')}
                onClick={() => setViewFullForecasts((v) => !v)}
                data-testid="view-full-forecasts"
                type="button"
              >
                <Eye size={12} />
                {viewFullForecasts ? 'Show less' : `Show all (${filteredForecasts.length})`}
                <ChevronDown size={12} className="dp-info-card__footer-chevron" />
              </button>
            )}
            <div className="dp-section-footer">
              <button className="dp-drill-link" onClick={() => navigate('/intel')} data-testid="drill-forecasts" type="button">
                View All Forecasts <ExternalLink size={10} />
              </button>
            </div>
          </>
        }
      >
          {visibleForecasts.length > 0 ? (
            <div className="dp-info-card__grid">
              {visibleForecasts.map((f) => {
                const conf = Math.min(f.overallConfidence, 1);
                const pct = Math.round(conf * 100);
                const band = bandClass(conf);
                return (
                  <button
                    key={`${f.agentId}-${f.metricName}`}
                    className="dp-info-item"
                    onClick={() => setDetailForecast(f)}
                    data-testid={`forecast-item-${f.metricName}`}
                    type="button"
                  >
                    <div className="dp-info-item__gauge-row">
                      <div className="dp-info-item__gauge">
                        <svg viewBox="0 0 36 36" className="dp-info-item__gauge-svg">
                          <circle className="dp-info-item__gauge-track" cx="18" cy="18" r="15.915" />
                          <circle
                            className={`dp-info-item__gauge-fill dp-info-item__gauge-fill--${band}`}
                            cx="18" cy="18" r="15.915"
                            strokeDasharray={barAnimated ? `${pct} 100` : '0 100'}
                          />
                        </svg>
                        <span className="dp-info-item__gauge-label">{pct}%</span>
                      </div>
                      <div className="dp-info-item__content">
                        <div className="dp-info-item__header">
                          <span className="dp-info-item__icon">{getMetricIcon(f.metricName)}</span>
                          <span className="dp-info-item__metric">{f.metricName}</span>
                        </div>
                        <span className="dp-info-item__value">
                          {f.points.length > 0
                            ? f.points[f.points.length - 1].value.toFixed(2)
                            : '\u2014'}
                        </span>
                        <span className="dp-info-item__meta">
                          Confidence: {pct}%
                        </span>
                      </div>
                    </div>
                  </button>
                );
              })}
            </div>
          ) : (
            <div className="aef-viz-well">
              <TrendingUp size={20} className="aef-viz-well__icon" />
              <span className="aef-viz-well__label">No forecasts available</span>
            </div>
          )}
      </DetachableCard>

      {/* ── Key Insights ──────────────────────────────────────────── */}
      <DetachableCard
        title="Key Insights"
        icon={<Shield size={14} />}
        metaLabel={String(filteredInsights.length)}
        headerAction={
          <div className="dp-info-card__header-select">
            <ParytySelect
              options={severityOptions}
              value={selectedSeverity}
              onChange={setSelectedSeverity}
              placeholder="Filter severity"
              testId="insight-severity-select"
            />
          </div>
        }
        className="dp-info-card"
        testId="insight-info-card"
        footer={
          <>
            {filteredInsights.length > PREVIEW_COUNT && (
              <button
                className={clsx('dp-info-card__footer-btn', viewFullInsights && 'dp-info-card__footer-btn--active')}
                onClick={() => setViewFullInsights((v) => !v)}
                data-testid="view-full-insights"
                type="button"
              >
                <Eye size={12} />
                {viewFullInsights ? 'Show less' : `Show all (${filteredInsights.length})`}
                <ChevronDown size={12} className="dp-info-card__footer-chevron" />
              </button>
            )}
            <div className="dp-section-footer">
              <button className="dp-drill-link" onClick={() => navigate('/intel')} data-testid="drill-insights" type="button">
                View Intel <ExternalLink size={10} />
              </button>
            </div>
          </>
        }
      >
          {visibleInsights.length > 0 ? (
            <div className="dp-info-card__grid">
              {visibleInsights.map((a) => {
                const pct = Math.round(a.score * 100);
                const band = bandClass(a.score);
                return (
                  <button
                    key={a.id}
                    className="dp-info-item"
                    onClick={() => setDetailAnomaly(a)}
                    data-testid={`insight-item-${a.id}`}
                    type="button"
                  >
                    <div className="dp-info-item__gauge-row">
                      <div className="dp-info-item__gauge">
                        <svg viewBox="0 0 36 36" className="dp-info-item__gauge-svg">
                          <circle className="dp-info-item__gauge-track" cx="18" cy="18" r="15.915" />
                          <circle
                            className={`dp-info-item__gauge-fill dp-info-item__gauge-fill--${band}`}
                            cx="18" cy="18" r="15.915"
                            strokeDasharray={barAnimated ? `${pct} 100` : '0 100'}
                          />
                        </svg>
                        <span className="dp-info-item__gauge-label">{pct}%</span>
                      </div>
                      <div className="dp-info-item__content">
                        <div className="dp-info-item__header">
                          <span className="dp-info-item__icon">{getAnomalyIcon(a.severity)}</span>
                          <span className="dp-info-item__metric">{a.metricName}</span>
                        </div>
                        <span className="dp-info-item__value">
                          {a.explanation}
                        </span>
                        <span className="dp-info-item__meta">
                          Score: {pct}%&nbsp;&middot;&nbsp;
                          <span className={clsx('dp-info-item__sev', `dp-info-item__sev--${a.severity}`)}>{a.severity}</span>
                        </span>
                      </div>
                    </div>
                  </button>
                );
              })}
            </div>
          ) : (
            <div className="aef-viz-well">
              <Shield size={20} className="aef-viz-well__icon" />
              <span className="aef-viz-well__label">No anomalies detected</span>
            </div>
          )}
      </DetachableCard>

      {/* ── Forecast Detail Modal ────────────────────────────────── */}
      {detailForecast && (
        <div
          className="aef-modal-overlay"
          onClick={() => setDetailForecast(null)}
          data-testid="forecast-detail-modal"
        >
          <div className="aef-modal" style={{ maxWidth: 440 }} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Forecast Detail">
            <div className="aef-modal-header">
              <span className="aef-modal-title">Forecast Detail</span>
              <button className="aef-modal-close" onClick={() => setDetailForecast(null)} aria-label="Close" type="button">
                <X size={14} />
              </button>
            </div>
            <div className="aef-modal-body">
              <div className="dp-detail-modal__stats">
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Metric</span>
                  <span className="dp-detail-modal__value">{detailForecast.metricName}</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Agent</span>
                  <span className="dp-detail-modal__value">{detailForecast.agentId}</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Confidence</span>
                  <span className="dp-detail-modal__value">{(Math.min(detailForecast.overallConfidence, 1) * 100).toFixed(0)}%</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Latest Value</span>
                  <span className="dp-detail-modal__value">
                    {detailForecast.points.length > 0
                      ? detailForecast.points[detailForecast.points.length - 1].value.toFixed(4)
                      : '\u2014'}
                  </span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Data Points</span>
                  <span className="dp-detail-modal__value">{detailForecast.points.length}</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Model</span>
                  <span className="dp-detail-modal__value">{detailForecast.modelInfo?.bestModel ?? '\u2014'}</span>
                </div>
                {detailForecast.modelInfo?.lastTrained && (
                  <div className="dp-detail-modal__stat">
                    <span className="dp-detail-modal__label">Last Trained</span>
                    <span className="dp-detail-modal__value">
                      {new Date(detailForecast.modelInfo.lastTrained).toLocaleDateString('en-US', {
                        month: 'short', day: 'numeric', year: 'numeric',
                      })}
                    </span>
                  </div>
                )}
              </div>
            </div>
            <div className="aef-modal-footer">
              <div style={{ display: 'flex', gap: 'var(--aef-space-2)', marginLeft: 'auto' }}>
                <button className="aef-btn aef-btn-inactive" onClick={() => setDetailForecast(null)} type="button">
                  Close
                </button>
                <button
                  className="aef-btn aef-btn-active"
                  onClick={() => { setDetailForecast(null); navigate('/intel'); }}
                  data-testid="go-to-intel-from-forecast"
                  type="button"
                >
                  Go to Intel <ExternalLink size={12} />
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Anomaly Detail Modal ─────────────────────────────────── */}
      {detailAnomaly && (
        <div
          className="aef-modal-overlay"
          onClick={() => setDetailAnomaly(null)}
          data-testid="anomaly-detail-modal"
        >
          <div className="aef-modal" style={{ maxWidth: 440 }} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Anomaly Detail">
            <div className="aef-modal-header">
              <span className="aef-modal-title">Anomaly Detail</span>
              <button className="aef-modal-close" onClick={() => setDetailAnomaly(null)} aria-label="Close" type="button">
                <X size={14} />
              </button>
            </div>
            <div className="aef-modal-body">
              <div className="dp-detail-modal__stats">
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Metric</span>
                  <span className="dp-detail-modal__value">{detailAnomaly.metricName}</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Agent</span>
                  <span className="dp-detail-modal__value">{detailAnomaly.agentId}</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Severity</span>
                  <span className={clsx('dp-info-item__sev', `dp-info-item__sev--${detailAnomaly.severity}`)}>
                    {detailAnomaly.severity}
                  </span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Score</span>
                  <span className="dp-detail-modal__value">{(detailAnomaly.score * 100).toFixed(0)}%</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Value at Anomaly</span>
                  <span className="dp-detail-modal__value">{detailAnomaly.value.toFixed(4)}</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Type</span>
                  <span className="dp-detail-modal__value">{detailAnomaly.type}</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Detection Method</span>
                  <span className="dp-detail-modal__value">{detailAnomaly.detectionMethod}</span>
                </div>
                <div className="dp-detail-modal__stat">
                  <span className="dp-detail-modal__label">Timestamp</span>
                  <span className="dp-detail-modal__value">
                    {new Date(detailAnomaly.timestamp).toLocaleString('en-US', {
                      month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
                    })}
                  </span>
                </div>
              </div>
              <div className="dp-detail-modal__explanation">{detailAnomaly.explanation}</div>
              {detailAnomaly.contributingFactors.length > 0 && (
                <div>
                  <span className="dp-detail-modal__label">Contributing Factors</span>
                  <div className="dp-detail-modal__factors">
                    {detailAnomaly.contributingFactors.map((factor, i) => (
                      <span key={i} className="dp-detail-modal__factor">{factor}</span>
                    ))}
                  </div>
                </div>
              )}
            </div>
            <div className="aef-modal-footer">
              <div style={{ display: 'flex', gap: 'var(--aef-space-2)', marginLeft: 'auto' }}>
                <button className="aef-btn aef-btn-inactive" onClick={() => setDetailAnomaly(null)} type="button">
                  Close
                </button>
                <button
                  className="aef-btn aef-btn-active"
                  onClick={() => { setDetailAnomaly(null); navigate('/intel'); }}
                  data-testid="go-to-intel-from-anomaly"
                  type="button"
                >
                  Go to Intel <ExternalLink size={12} />
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

// ─── Twin Detail Sub-View ────────────────────────────────────────────────

function TwinDetailSubView({
  twinId, onBack,
}: {
  twinId: string;
  onBack: () => void;
}) {
  const navigate = useNavigate();
  const catalogue = useDashboardStore((s) => s.catalogue);
  const activeTwinId = useDashboardStore((s) => s.activeTwinId);
  const setActiveTwin = useDashboardStore((s) => s.setActiveTwin);
  const alerts = useAlertsStore((s) => s.alerts);
  const selectedTwin = useTwinStore((s) => s.selectedTwin);
  const assignedAgents = useTwinStore((s) => s.assignedAgents);
  const fetchTwin = useTwinStore((s) => s.fetchTwin);
  const fetchAgents = useTwinStore((s) => s.fetchAgents);
  const clearSelection = useTwinStore((s) => s.clearSelection);
  const [confirmActivate, setConfirmActivate] = useState(false);

  useEffect(() => {
    fetchTwin(twinId);
    fetchAgents(twinId);
    return () => clearSelection();
  }, [twinId, fetchTwin, fetchAgents, clearSelection]);

  const twin = catalogue.find((t) => t.id === twinId);
  const isThisActive = twinId === activeTwinId;
  const twinAlerts = alerts.filter((a) => {
    const labelMatch = a.labels?.twin_id === twinId || a.labels?.twin === twin?.name;
    const agentMatch = twin?.config.agentIds.includes(a.labels?.agent_id);
    return labelMatch || agentMatch;
  }).slice(0, 10);

  const handleActivate = useCallback(() => {
    setActiveTwin(twinId);
    setConfirmActivate(false);
  }, [setActiveTwin, twinId]);

  return (
    <div className="dp-page">
      <div className="dp-page__inner">
        <div className="dp-twin-detail">
          {/* Header */}
          <div className="dp-twin-detail__header">
            <button className="dp-twin-detail__back" onClick={onBack} data-testid="twin-detail-back">
              <ArrowLeft size={14} /> Back to Overview
            </button>
            <span className="dp-twin-detail__title">
              {twin?.name || selectedTwin?.name || 'Twin Details'}
            </span>
            {twin && (
              <span className={clsx('aef-badge', healthBadgeClass(twin.health))}>
                {twin.health}
              </span>
            )}
            {isThisActive && (
              <span className="dp-active-pill" style={{ display: 'inline-flex' }} data-testid="detail-active-pill">
                <span className="dp-active-dot" />
                Active
              </span>
            )}
            <button
              className={clsx('aef-btn', isThisActive ? 'aef-btn-active' : 'aef-btn-inactive')}
              onClick={() => setConfirmActivate(true)}
              disabled={isThisActive}
              style={{ marginLeft: 'auto', opacity: isThisActive ? 0.6 : 1 }}
              data-testid="twin-set-active"
            >
              {isThisActive ? <><Check size={12} /> Active Twin</> : 'Set as Active'}
            </button>
            <button
              className="aef-btn aef-btn-inactive"
              onClick={() => navigate(`/twins/${twinId}`)}
              data-testid="twin-detail-full"
            >
              View Full Details <ExternalLink size={12} />
            </button>
          </div>

          {/* Twin metadata */}
          <div className="dp-twin-detail__grid">
            <DetachableCard
              title={twin?.name || 'Twin Metadata'}
              icon={<Box size={14} />}
              metaLabel={twin?.systemLabel || ''}
              testId="twin-metadata-section"
            >
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">System</span>
                <span className="aef-stat-module__value">{twin?.systemLabel || '—'}</span>
              </div>
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">Health</span>
                <span className={clsx('aef-badge', healthBadgeClass(twin?.health || 'unknown'))}>
                  {twin?.health || 'unknown'}
                </span>
              </div>
                <div className="aef-stat-module">
                  <span className="aef-stat-module__label">Nodes</span>
                  <span className="aef-stat-module__value">{twin?.summary.nodeCount ?? '—'}</span>
                </div>
                <div className="aef-stat-module">
                  <span className="aef-stat-module__label">Active Alerts</span>
                  <span className="aef-stat-module__value">{twin?.summary.activeAlerts ?? '—'}</span>
                </div>
                <div className="aef-stat-module">
                  <span className="aef-stat-module__label">Created</span>
                  <span className="aef-stat-module__value">
                    {twin ? new Date(twin.createdAt).toLocaleDateString('en-US', {
                      month: 'short', day: 'numeric', year: 'numeric',
                    }) : '—'}
                  </span>
                </div>
            </DetachableCard>

            {/* Assigned agents */}
            <DetachableCard
              title="Assigned Agents"
              icon={<Server size={14} />}
              metaLabel={String(assignedAgents.length)}
              testId="twin-assigned-agents"
            >
                {assignedAgents.length > 0 ? (
                  <div className="aef-table-card">
                    <div style={{ overflowX: 'auto' }}>
                      <table className="aef-table">
                        <thead>
                          <tr>
                            <th>Agent ID</th>
                            <th>Hostname</th>
                            <th>Status</th>
                            <th>OS</th>
                          </tr>
                        </thead>
                        <tbody>
                          {assignedAgents.map((agent) => (
                            <tr key={agent.agentId}>
                              <td><code style={{ fontSize: 10 }}>{agent.agentId.slice(0, 8)}...</code></td>
                              <td>{agent.hostname}</td>
                              <td>
                                <span className={`aef-badge ${agent.status === 'online' ? 'badge-valid' : 'badge-pending'}`}>
                                  {agent.status}
                                </span>
                              </td>
                              <td>{agent.os}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                ) : (
                  <div className="aef-viz-well">
                    <Server size={20} className="aef-viz-well__icon" />
                    <span className="aef-viz-well__label">No agents assigned</span>
                  </div>
                )}
            </DetachableCard>

            {/* Alerts for this twin */}
            <DetachableCard
              title="Alerts"
              icon={<Bell size={14} />}
              metaLabel={String(twinAlerts.length)}
              testId="twin-alerts-section"
            >
                {twinAlerts.length > 0 ? (
                  <div className="dp-alerts-list">
                    {twinAlerts.map((alert) => <AlertRow key={alert.id} alert={alert} />)}
                  </div>
                ) : (
                  <div className="aef-viz-well">
                    <Bell size={20} className="aef-viz-well__icon" />
                    <span className="aef-viz-well__label">No active alerts</span>
                  </div>
                )}
            </DetachableCard>
          </div>
        </div>
      </div>

      {/* Confirmation modal for Set as Active */}
      {confirmActivate && (
        <div
          className="aef-modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) setConfirmActivate(false); }}
        >
          <div className="aef-modal" style={{ maxWidth: 420 }} role="dialog" aria-modal="true" aria-label="Confirm active twin change">
            <div className="aef-modal-header">
              <span className="aef-modal-title">Set Active Twin</span>
              <button className="aef-modal-close" onClick={() => setConfirmActivate(false)} aria-label="Close">
                <X size={14} />
              </button>
            </div>
            <div className="aef-modal-body">
              <p style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 12,
                color: 'var(--aef-text-primary)',
                lineHeight: 1.6,
              }}>
                Are you sure you want to set <strong>{twin?.name || 'this twin'}</strong> as your active Digital Paryty?
              </p>
              <p style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 11,
                color: 'var(--aef-text-secondary)',
                lineHeight: 1.6,
                marginTop: 'var(--aef-space-2)',
              }}>
                All dashboard data and intelligence queries will be scoped to this twin.
              </p>
            </div>
            <div className="aef-modal-footer">
              <div style={{ display: 'flex', gap: 'var(--aef-space-2)', marginLeft: 'auto' }}>
                <button
                  className="aef-btn aef-btn-inactive"
                  onClick={() => setConfirmActivate(false)}
                  data-testid="confirm-activate-cancel"
                >
                  Cancel
                </button>
                <button
                  className="aef-btn aef-btn-active"
                  onClick={handleActivate}
                  data-testid="confirm-activate-accept"
                >
                  Confirm
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Empty state ──────────────────────────────────────────────────────────

function EmptyState({ onNew }: { onNew: () => void }) {
  return (
    <div className="dp-empty">
      <Box size={40} className="dp-empty__icon" />
      <div>
        <p className="dp-empty__title">No Digital Parytys yet</p>
        <p className="dp-empty__sub">
          Create your first Digital Paryty to start observing, forecasting, and
          running Watif drills on your software systems.
        </p>
      </div>
      <button className="aef-btn aef-btn-active" onClick={onNew} data-testid="empty-new-twin">
        <Plus size={14} /> New Digital Paryty
      </button>
    </div>
  );
}

// ─── Wizard ──────────────────────────────────────────────────────────────

const WIZARD_STEPS = ['Identity', 'Abilities', 'Agents', 'Confirm'];

function WizardStepDots({ current, total }: { current: number; total: number }) {
  return (
    <div className="dp-step-dots" aria-label={`Step ${current + 1} of ${total}`}>
      {Array.from({ length: total }).map((_, i) => (
        <span key={i} className={clsx('dp-step-dot', i === current && 'dp-step-dot--active')} />
      ))}
    </div>
  );
}

function StepIdentity() {
  const name = useDashboardStore((s) => s.draft.name);
  const systemLabel = useDashboardStore((s) => s.draft.systemLabel);
  const setName = useDashboardStore((s) => s.setDraftName);
  const setSystem = useDashboardStore((s) => s.setDraftSystemLabel);

  return (
    <>
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="dp-name">Digital Paryty name</label>
        <input
          id="dp-name" className="dp-field__input" type="text"
          placeholder="e.g. Payment Gateway Twin"
          value={name} onChange={(e) => setName(e.target.value)}
          autoFocus maxLength={80}
        />
      </div>
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="dp-system">Software system being twinned</label>
        <input
          id="dp-system" className="dp-field__input" type="text"
          placeholder="e.g. Payment Gateway v3, Auth Service"
          value={systemLabel} onChange={(e) => setSystem(e.target.value)}
          maxLength={80}
        />
      </div>
      <p style={{
        fontFamily: 'var(--aef-font-body)', fontSize: 11,
        color: 'var(--aef-text-secondary)', lineHeight: 1.6,
      }}>
        A Digital Paryty is a live-telemetry-driven digital twin of one of your
        software systems. You can create as many as your subscription allows.
      </p>
    </>
  );
}

function StepAbilities() {
  const selected = useDashboardStore((s) => s.draft.selectedAbilities);
  const toggle = useDashboardStore((s) => s.toggleDraftAbility);
  const hasFeature = usePlanStore((s) => s.hasFeature);

  return (
    <>
      <p style={{
        fontFamily: 'var(--aef-font-body)', fontSize: 11,
        color: 'var(--aef-text-secondary)', lineHeight: 1.6,
      }}>
        Select the abilities you want to activate on this Digital Paryty.
      </p>
      <div className="dp-ability-grid">
        {ABILITY_CATALOGUE.map((ability) => {
          const isSelected = selected.includes(ability.id);
          const featureName = ABILITY_FEATURE_MAP[ability.id];
          const isAvailable = featureName ? hasFeature(featureName) : true;
          return (
            <button
              key={ability.id} type="button"
              className={clsx(
                'dp-ability-tile',
                isSelected && 'dp-ability-tile--selected',
                !isAvailable && 'dp-ability-tile--disabled',
              )}
              onClick={() => isAvailable && toggle(ability.id)}
              aria-pressed={isSelected}
              disabled={!isAvailable}
            >
              <CheckCircle2 size={14} className="dp-ability-tile__check" aria-hidden />
              <span className="dp-ability-tile__icon">{abilityIcon(ability.iconName, 16)}</span>
              <span className="dp-ability-tile__name">
                {ability.name}{!isAvailable && ' (Upgrade to unlock)'}
              </span>
              <span className="dp-ability-tile__desc">{ability.description}</span>
            </button>
          );
        })}
      </div>
    </>
  );
}

function StepAgents() {
  const agents = useDashboardStore((s) => s.agents);
  const agentsLoading = useDashboardStore((s) => s.agentsLoading);
  const draftAgentIds = useDashboardStore((s) => s.draft.agentIds);
  const setDraftAgentIds = useDashboardStore((s) => s.setDraftAgentIds);

  const toggleAgent = (agentId: string) => {
    const isSelected = draftAgentIds.includes(agentId);
    setDraftAgentIds(
      isSelected
        ? draftAgentIds.filter((id) => id !== agentId)
        : [...draftAgentIds, agentId]
    );
  };

  if (agentsLoading) {
    return (
      <p style={{
        fontFamily: 'var(--aef-font-body)', fontSize: 11,
        color: 'var(--aef-text-secondary)', textAlign: 'center',
        padding: 'var(--aef-space-6) 0',
      }}>
        Loading agents…
      </p>
    );
  }

  if (agents.length === 0) {
    return (
      <p style={{
        fontFamily: 'var(--aef-font-body)', fontSize: 11,
        color: 'var(--aef-text-secondary)', lineHeight: 1.6,
      }}>
        No agents available. Agents will be linked after creation.
      </p>
    );
  }

  return (
    <>
      <p style={{
        fontFamily: 'var(--aef-font-body)', fontSize: 11,
        color: 'var(--aef-text-secondary)', lineHeight: 1.6,
      }}>
        Select agents to assign to this Digital Paryty. You can skip this step and assign agents later.
      </p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--aef-space-2)' }}>
        {agents.map((agent) => {
          const isSelected = draftAgentIds.includes(agent.agent_id);
          return (
            <button
              key={agent.agent_id}
              type="button"
              className={clsx('dp-ability-tile', isSelected && 'dp-ability-tile--selected')}
              onClick={() => toggleAgent(agent.agent_id)}
              style={{ flexDirection: 'row', alignItems: 'center', gap: 'var(--aef-space-3)', padding: 'var(--aef-space-2) var(--aef-space-3)' }}
            >
              <CheckCircle2 size={14} className="dp-ability-tile__check" aria-hidden />
              <span style={{ fontFamily: 'var(--aef-font-body)', fontSize: 11, color: 'var(--aef-text-primary)' }}>
                {agent.hostname || agent.agent_id}
              </span>
              <span style={{ fontFamily: 'var(--aef-font-body)', fontSize: 10, color: 'var(--aef-text-secondary)', marginLeft: 'auto' }}>
                {agent.os} · {agent.status}
              </span>
            </button>
          );
        })}
      </div>
    </>
  );
}

function StepConfirm() {
  const draft = useDashboardStore((s) => s.draft);
  const enabledMetas = draft.selectedAbilities
    .map((id) => ABILITY_CATALOGUE.find((m) => m.id === id))
    .filter((m): m is AbilityMeta => m !== undefined);

  return (
    <>
      <p style={{
        fontFamily: 'var(--aef-font-body)', fontSize: 11,
        color: 'var(--aef-text-secondary)', lineHeight: 1.6,
      }}>
        Review your choices before creating the Digital Paryty.
      </p>
      <div>
        <div className="dp-confirm-row">
          <span className="dp-confirm-row__label">Name</span>
          <span className="dp-confirm-row__value">{draft.name || '—'}</span>
        </div>
        <div className="dp-confirm-row">
          <span className="dp-confirm-row__label">System</span>
          <span className="dp-confirm-row__value">{draft.systemLabel || '—'}</span>
        </div>
        <div className="dp-confirm-row">
          <span className="dp-confirm-row__label">Abilities ({enabledMetas.length})</span>
          {enabledMetas.length === 0 ? (
            <span className="dp-confirm-row__value" style={{ color: 'var(--aef-text-secondary)' }}>
              None selected — you can add abilities after creation.
            </span>
          ) : (
            <div className="dp-confirm-abilities">
              {enabledMetas.map((m) => <AbilityBadge key={m.id} ability={m} />)}
            </div>
          )}
        </div>
      </div>
    </>
  );
}

function CreateParytyWizard() {
  const wizardOpen = useDashboardStore((s) => s.wizardOpen);
  const step       = useDashboardStore((s) => s.wizardStep);
  const draft      = useDashboardStore((s) => s.draft);
  const close      = useDashboardStore((s) => s.closeWizard);
  const next       = useDashboardStore((s) => s.nextStep);
  const prev       = useDashboardStore((s) => s.prevStep);
  const commit     = useDashboardStore((s) => s.commitDraft);
  const backdropRef = useRef<HTMLDivElement>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);

  useEffect(() => {
    if (!wizardOpen) return;
    function onKey(e: KeyboardEvent) { if (e.key === 'Escape') close(); }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [wizardOpen, close]);

  if (!wizardOpen) return null;

  const isLastStep = step === WIZARD_STEPS.length - 1;
  const canAdvance = step === 0 ? draft.name.trim().length > 0 : true;
  const stepContent = [<StepIdentity />, <StepAbilities />, <StepAgents />, <StepConfirm />][step];
  const primaryLabel = isLastStep ? 'Create Digital Paryty' : 'Continue';

  return (
    <div
      className="aef-modal-overlay"
      ref={backdropRef}
      onClick={(e) => { if (e.target === backdropRef.current) close(); }}
    >
      <div className="aef-modal" style={{ maxWidth: 560 }} role="dialog" aria-modal="true" aria-label={`Create Digital Paryty — ${WIZARD_STEPS[step]}`}>
        <div className="aef-modal-header">
          <span className="aef-modal-title">New Digital Paryty — {WIZARD_STEPS[step]}</span>
          <button className="aef-modal-close" onClick={close} aria-label="Close wizard">
            <X size={14} />
          </button>
        </div>
        <div className="aef-modal-body">
          {stepContent}
          {createError && (
            <div style={{
              marginTop: 'var(--aef-space-3)', padding: 'var(--aef-space-3)',
              background: 'rgba(239, 68, 68, 0.1)',
              borderRadius: 'var(--aef-radius-control)',
              color: 'var(--aef-counter-variant-b)',
              fontSize: 12, fontFamily: 'var(--aef-font-body)',
            }}>
              {createError}
            </div>
          )}
        </div>
        <div className="aef-modal-footer">
          <WizardStepDots current={step} total={WIZARD_STEPS.length} />
          <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
            {step > 0 && (
              <button className="aef-btn aef-btn-inactive" onClick={prev}>Back</button>
            )}
            <button
              className="aef-btn aef-btn-active"
              onClick={isLastStep
                ? async () => {
                    setIsCreating(true); setCreateError(null);
                    try { await commit(); }
                    catch (err) { setCreateError(err instanceof Error ? err.message : 'Failed to create'); }
                    finally { setIsCreating(false); }
                  }
                : next}
              disabled={!canAdvance || isCreating}
              style={{ opacity: canAdvance && !isCreating ? 1 : 0.4 }}
            >
              {isCreating ? 'Creating…' : primaryLabel}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// MAIN DASHBOARD PAGE
// ═══════════════════════════════════════════════════════════════════════════

export function DashboardPage() {
  // ─── State ──────────────────────────────────────────────────────────────

  const [expandedSection, setExpandedSection] = useState(0);
  const [selectedTwinId, setSelectedTwinId] = useState<string | null>(null);
  const [activeCounterModal, setActiveCounterModal] = useState<string | null>(null);
  const [selectedAgent, setSelectedAgent] = useState<AgentManagementInfo | null>(null);
  const pageRef = useRef<HTMLDivElement>(null);
  const mainPaneRef = useRef<HTMLDivElement>(null);
  const counterStripRef = useRef<HTMLDivElement>(null);

  // ─── Store hooks ────────────────────────────────────────────────────────

  const catalogue     = useDashboardStore((s) => s.catalogue);
  const openWizard    = useDashboardStore((s) => s.openWizard);
  const isLoading     = useDashboardStore((s) => s.isLoading);
  const error         = useDashboardStore((s) => s.error);
  const fetchTwins    = useDashboardStore((s) => s.fetchTwins);
  const activeTwinId  = useDashboardStore((s) => s.activeTwinId);
  const quotaExceeded = useDashboardStore((s) => s.quotaExceeded);
  const clearQuotaExceeded = useDashboardStore((s) => s.clearQuotaExceeded);
  const currentPlan   = usePlanStore((s) => s.currentPlan);
  const alerts        = useAlertsStore((s) => s.alerts);
  const forecasts     = useIntelStore((s) => s.forecasts);
  const anomalies     = useIntelStore((s) => s.anomalies);
  const agents        = useDashboardStore((s) => s.agents);
  const agentsLoading = useDashboardStore((s) => s.agentsLoading);
  const navigate      = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [showQuotaModal, setShowQuotaModal] = useState(false);

  // Quota pre-check: verify catalogue count < maxTwins before opening wizard.
  const handleNewTwin = useCallback(() => {
    const maxTwins = currentPlan?.limits?.maxTwins ?? 0;
    if (maxTwins > 0 && catalogue.length >= maxTwins) {
      setShowQuotaModal(true);
      return;
    }
    openWizard();
  }, [catalogue.length, currentPlan, openWizard]);

  // ─── Live polling hooks ─────────────────────────────────────────────────

  useIntel();              // 60s polling for forecasts + anomalies
  useDashboardPolling();   // 30s twins + 15s agents/alerts

  // Scroll-driven CSS custom properties for smooth header & counter-strip transitions.
  // Uses requestAnimationFrame to stay in sync with the browser paint cycle — zero
  // React re-renders. --scroll-progress lerps 0→1 over 0–60px on the main pane.
  useEffect(() => {
    const scrollEl = mainPaneRef.current;
    const targetEl = pageRef.current;
    if (!scrollEl || !targetEl) return;

    let rafId: number;
    const RANGE = 60; // full transition over 60px of scroll

    const onScroll = () => {
      cancelAnimationFrame(rafId);
      rafId = requestAnimationFrame(() => {
        const raw = Math.min(Math.max(scrollEl.scrollTop / RANGE, 0), 1);
        const p = Number(raw.toFixed(4));
        targetEl.style.setProperty('--scroll-progress', String(p));

        // Hysteresis on data-scrolled prevents a layout feedback loop:
        // toggling compact mode changes the counter strip height → grid reflows
        // → scroll position shifts → progress crosses threshold again → oscillates.
        // Dead zone: enter compact at 0.6, exit at 0.3 (0.3-wide gap absorbs jitter).
        const currentlyCompact = targetEl.hasAttribute('data-scrolled');
        if (!currentlyCompact && p > 0.6) {
          targetEl.setAttribute('data-scrolled', '');
        } else if (currentlyCompact && p < 0.3) {
          targetEl.removeAttribute('data-scrolled');
        }
      });
    };

    scrollEl.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      scrollEl.removeEventListener('scroll', onScroll);
      cancelAnimationFrame(rafId);
    };
  }, [selectedTwinId]);

  // ─── Agent delete handler ─────────────────────────────────────────────

  const handleDeleteAgent = useCallback(async (agentId: string) => {
    try {
      const client = getRestClient();
      await client.delete(`/api/v1/agents/${encodeURIComponent(agentId)}`);
      useToastStore.getState().addToast({ type: 'success', message: 'Agent deleted.' });
      fetchTwins(); // refresh catalogue including agents
    } catch {
      useToastStore.getState().addToast({ type: 'error', message: 'Failed to delete agent.' });
    }
  }, [fetchTwins]);

  const handleAgentUpdated = useCallback((_agent: AgentManagementInfo) => {
    setSelectedAgent(null);
    fetchTwins(); // refresh catalogue including agents
  }, [fetchTwins]);

  // Auto-open wizard when ?new=true
  useEffect(() => {
    if (searchParams.get('new') === 'true') {
      handleNewTwin();
      searchParams.delete('new');
      setSearchParams(searchParams, { replace: true });
    }
  }, [searchParams, handleNewTwin, setSearchParams]);

  // ─── Derived data ───────────────────────────────────────────────────────

  const totalTwins   = catalogue.length;
  const totalErrors  = catalogue.reduce((acc, p) => acc + (p.summary.activeAlerts ?? 0), 0);
  const healthyCount = catalogue.filter((p) => p.health === 'healthy').length;
  const needsChecking = catalogue.filter((p) => p.health !== 'healthy').length;

  const topAgents = [...(Array.isArray(agents) ? agents : [])]
    .sort((a, b) => new Date(b.last_seen).getTime() - new Date(a.last_seen).getTime())
    .slice(0, 5);

  const recentAlerts = [...(Array.isArray(alerts) ? alerts : [])]
    .sort((a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime())
    .slice(0, 5);

  // ─── Handlers ───────────────────────────────────────────────────────────

  // Attach a native (non-passive) wheel listener to the counter strip so
  // vertical wheel deltas can be redirected to horizontal scroll.
  // React's onWheel is passive by default — calling preventDefault() inside
  // it triggers "Unable to preventDefault inside passive event listener".
  useEffect(() => {
    const el = counterStripRef.current;
    if (!el) return;
    const handler = (e: WheelEvent) => {
      if (Math.abs(e.deltaY) > Math.abs(e.deltaX)) {
        e.preventDefault();
        el.scrollLeft += e.deltaY;
      }
    };
    el.addEventListener('wheel', handler, { passive: false });
    return () => el.removeEventListener('wheel', handler);
  }, []);

  const toggleSection = useCallback((index: number) => {
    setExpandedSection((prev) => (prev === index ? -1 : index));
  }, []);

  const handleSelectTwin = useCallback((id: string) => {
    setSelectedTwinId(id);
  }, []);

  // ─── Twin detail sub-view ───────────────────────────────────────────────

  if (selectedTwinId) {
    return (
      <TwinDetailSubView
        twinId={selectedTwinId}
        onBack={() => setSelectedTwinId(null)}
      />
    );
  }

  // ─── Render ─────────────────────────────────────────────────────────────

  return (
    <div className="dp-page">
      <div className="dp-page__inner" ref={pageRef}>

        {/* Page header — fades on scroll */}
        <div className="dp-page__header">
          <div>
            <h1 className="dp-page__header-title">Dashboard</h1>
            <p className="dp-page__header-sub">
              Your operational overview — {totalTwins} Digital {totalTwins === 1 ? 'Paryty' : 'Parytys'}
            </p>
          </div>
          <div className="dp-page__header-actions">
            <button
              className="aef-btn aef-btn-active"
              onClick={handleNewTwin}
              data-testid="dashboard-new-twin"
            >
              <Plus size={14} /> New Digital Paryty
            </button>
          </div>
        </div>

        {/* Error banner */}
        {error && (
          <div
            className="aef-container-card"
            style={{
              padding: 'var(--aef-space-3) var(--aef-space-4)',
              display: 'flex', alignItems: 'center', gap: 'var(--aef-space-3)',
              borderColor: 'var(--aef-counter-variant-b)',
            }}
            data-testid="dashboard-error"
          >
            <AlertTriangle size={16} style={{ color: 'var(--aef-counter-variant-b)', flexShrink: 0 }} />
            <span style={{
              fontFamily: 'var(--aef-font-body)', fontSize: 12,
              color: 'var(--aef-text-primary)', flex: 1,
            }}>
              {error}
            </span>
            <button className="aef-btn aef-btn-inactive" onClick={() => fetchTwins()}>Retry</button>
          </div>
        )}

        {/* Counter strip */}
        <div
          className="dp-counter-strip"
          ref={counterStripRef}
          data-testid="counter-strip"
        >
          <CounterCard icon={<Server size={14} />} label="Total Twins" value={totalTwins} onClick={() => setActiveCounterModal('twins')} />
          <CounterCard
            icon={<AlertTriangle size={14} />}
            label="Total Errors"
            value={totalErrors}
            variant="dp-counter-error"
            onClick={() => setActiveCounterModal('errors')}
          />
          <CounterCard
            icon={<Heart size={14} />}
            label="Healthy"
            value={healthyCount}
            variant="dp-counter-live"
            onClick={() => setActiveCounterModal('healthy')}
          />
          <CounterCard
            icon={<AlertCircle size={14} />}
            label="Needs Attention"
            value={needsChecking}
            variant="dp-counter-warning"
            onClick={() => setActiveCounterModal('attention')}
          />
        </div>

        {/* Two-pane layout */}
        <div className="dp-two-pane">
          {/* Main pane (left) */}
          <div className="dp-main-pane" ref={mainPaneRef}>
            {isLoading ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--aef-space-3)', padding: 'var(--aef-space-4)' }}>
                <div className="dp-skeleton dp-skeleton-card" />
                <div className="dp-skeleton dp-skeleton-card" />
                <div className="dp-skeleton dp-skeleton-card" />
              </div>
            ) : catalogue.length === 0 ? (
              <EmptyState onNew={handleNewTwin} />
            ) : (
              <div className="dp-accordion">
                {/* Section 1: Your Digital Parytys */}
                <AccordionSection
                  icon={<Box size={14} />}
                  title="Your Digital Parytys"
                  meta={totalTwins}
                  isOpen={expandedSection === 0}
                  onToggle={() => toggleSection(0)}
                  footer={
                    <div className="dp-section-footer">
                      <button
                        className="dp-drill-link"
                        onClick={() => navigate('/twins/new')}
                        data-testid="drill-twins"
                      >
                        Create New Paryty <ExternalLink size={10} />
                      </button>
                    </div>
                  }
                >
                  {catalogue.length > 0 ? (
                    <div className="dp-twins-grid">
                      {catalogue.map((p) => (
                        <DigitalParytyCard
                          key={p.id}
                          paryty={p}
                          isActive={p.id === activeTwinId}
                          onSelect={handleSelectTwin}
                        />
                      ))}
                    </div>
                  ) : (
              <EmptyState onNew={handleNewTwin} />
                  )}
                </AccordionSection>

                {/* Section 2: Top Agents */}
                <AccordionSection
                  icon={<Server size={14} />}
                  title="Top Agents"
                  meta={topAgents.length}
                  isOpen={expandedSection === 1}
                  onToggle={() => toggleSection(1)}
                  footer={
                    <div className="dp-section-footer">
                      <button
                        className="dp-drill-link"
                        onClick={() => navigate('/agents')}
                        data-testid="drill-agents"
                      >
                        View All Agents <ExternalLink size={10} />
                      </button>
                    </div>
                  }
                >
                  {agentsLoading ? (
                    <div style={{
                      padding: 'var(--aef-space-4)', textAlign: 'center',
                      color: 'var(--aef-text-secondary)', fontSize: 10,
                    }}>
                      Loading agents…
                    </div>
                  ) : topAgents.length > 0 ? (
                    <div className="dp-agents-list">
                      {topAgents.map((agent) => <AgentRow key={agent.agent_id} agent={agent} onClick={() => setSelectedAgent(agent)} />)}
                    </div>
                  ) : (
                    <div className="aef-viz-well">
                      <Server size={20} className="aef-viz-well__icon" />
                      <span className="aef-viz-well__label">No agents registered</span>
                    </div>
                  )}
                </AccordionSection>

                {/* Section 3: Recent Alerts */}
                <AccordionSection
                  icon={<Bell size={14} />}
                  title="Recent Alerts"
                  meta={recentAlerts.length}
                  isOpen={expandedSection === 2}
                  onToggle={() => toggleSection(2)}
                  footer={
                    <div className="dp-section-footer">
                      <button
                        className="dp-drill-link"
                        onClick={() => navigate('/alerts')}
                        data-testid="drill-alerts"
                      >
                        View All Alerts <ExternalLink size={10} />
                      </button>
                    </div>
                  }
                >
                  {recentAlerts.length > 0 ? (
                    <div className="dp-alerts-list">
                      {recentAlerts.map((alert) => <AlertRow key={alert.id} alert={alert} />)}
                    </div>
                  ) : (
                    <div className="aef-viz-well">
                      <Bell size={20} className="aef-viz-well__icon" />
                      <span className="aef-viz-well__label">No active alerts</span>
                    </div>
                  )}
                </AccordionSection>

                {/* Section 4: Twin Summary */}
                <AccordionSection
                  icon={<Cpu size={14} />}
                  title="Twin Summary"
                  meta={catalogue.length}
                  isOpen={expandedSection === 3}
                  onToggle={() => toggleSection(3)}
                  footer={
                    <div className="dp-section-footer">
                      <button
                        className="dp-drill-link"
                        onClick={() => navigate('/metrics')}
                        data-testid="drill-metrics"
                      >
                        View Metrics <ExternalLink size={10} />
                      </button>
                    </div>
                  }
                >
                  {catalogue.length > 0 ? (
                    <div className="dp-metric-grid">
                      {catalogue.map((twin) => <MetricCard key={twin.id} twin={twin} />)}
                    </div>
                  ) : (
                    <div className="aef-viz-well">
                      <Cpu size={20} className="aef-viz-well__icon" />
                      <span className="aef-viz-well__label">No metric data available</span>
                    </div>
                  )}
                </AccordionSection>
              </div>
            )}
          </div>

          {/* Info pane (right) */}
          <div className="dp-info-pane">
            <InfoPane
              catalogue={catalogue}
              forecasts={forecasts}
              anomalies={anomalies}
            />
          </div>
        </div>
      </div>

      {/* Wizard modal */}
      <CreateParytyWizard />

      {/* Quota exceeded modal */}
      {(showQuotaModal || quotaExceeded) && (
        <div className="aef-modal-overlay" onClick={(e) => { if (e.target === e.currentTarget) { setShowQuotaModal(false); clearQuotaExceeded(); } }}>
          <div className="aef-modal" style={{ maxWidth: 420 }} role="dialog" aria-modal="true" aria-label="Twin Limit Reached">
            <div className="aef-modal-header">
              <span className="aef-modal-title">Twin Limit Reached</span>
              <button className="aef-modal-close" onClick={() => { setShowQuotaModal(false); clearQuotaExceeded(); }} aria-label="Close">
                <X size={14} />
              </button>
            </div>
            <div className="aef-modal-body">
              <p className="aef-modal-para">
                You've reached the maximum of {currentPlan?.limits?.maxTwins ?? '?'} Digital Parytys for your {currentPlan?.planName ?? 'current'} plan.
              </p>
              <p className="aef-modal-para" style={{ color: 'var(--aef-text-secondary)' }}>
                Upgrade your plan to create more Digital Parytys.
              </p>
            </div>
            <div className="aef-modal-footer">
              <button className="aef-btn aef-btn-inactive" onClick={() => { setShowQuotaModal(false); clearQuotaExceeded(); }}>
                Cancel
              </button>
              <button className="aef-btn aef-btn-active" onClick={() => { setShowQuotaModal(false); clearQuotaExceeded(); navigate('/settings'); }}>
                Upgrade Plan
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Counter detail modal — accent matches card variant */}
      {activeCounterModal && (() => {
        const modalConfigs: Record<string, { title: string; icon: ReactNode; items: DigitalParyty[]; accent: string }> = {
          twins: {
            title: 'All Digital Parytys',
            icon: <Server size={14} />,
            items: catalogue,
            accent: 'aef-modal--accent-neutral',
          },
          errors: {
            title: 'Twins with Active Alerts',
            icon: <AlertTriangle size={14} />,
            items: catalogue.filter((p) => (p.summary.activeAlerts ?? 0) > 0),
            accent: 'aef-modal--accent-variant-b',
          },
          healthy: {
            title: 'Healthy Twins',
            icon: <Heart size={14} />,
            items: catalogue.filter((p) => p.health === 'healthy'),
            accent: 'aef-modal--accent-live',
          },
          attention: {
            title: 'Needs Attention',
            icon: <AlertCircle size={14} />,
            items: catalogue.filter((p) => p.health !== 'healthy'),
            accent: 'aef-modal--accent-warning',
          },
        };
        const config = modalConfigs[activeCounterModal];
        if (!config) return null;
        return (
          <div
            className="aef-modal-overlay"
            onClick={(e) => { if (e.target === e.currentTarget) setActiveCounterModal(null); }}
          >
            <div className={clsx('aef-modal', config.accent)} style={{ maxWidth: 480 }} role="dialog" aria-modal="true" aria-label={config.title}>
              <div className="aef-modal-header">
                <span className="aef-modal-title">{config.title}</span>
                <button className="aef-modal-close" onClick={() => setActiveCounterModal(null)} aria-label="Close">
                  <X size={14} />
                </button>
              </div>
              <div className="aef-modal-body aef-scroll" style={{ maxHeight: 400 }}>
                {config.items.length > 0 ? (
                  <div className="dp-agents-list">
                    {config.items.map((twin) => (
                      <div
                        key={twin.id}
                        className="dp-agent-row"
                        style={{ cursor: 'pointer' }}
                        onClick={() => { setActiveCounterModal(null); setSelectedTwinId(twin.id); }}
                        data-testid={`counter-modal-twin-${twin.id}`}
                      >
                        <span className="dp-agent-row__icon">
                          <HealthDot status={twin.health} />
                        </span>
                        <div className="dp-agent-row__info">
                          <div className="dp-agent-row__name">{twin.name}</div>
                          <div className="dp-agent-row__meta">{twin.systemLabel}</div>
                        </div>
                        <span className={clsx('aef-badge', healthBadgeClass(twin.health))}>
                          {twin.health}
                        </span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="aef-viz-well">
                    {config.icon}
                    <span className="aef-viz-well__label">No items to display</span>
                  </div>
                )}
              </div>
            </div>
          </div>
        );
      })()}

      {/* Agent Detail Modal */}
      {selectedAgent && (
        <AgentDetailModal
          agent={selectedAgent}
          onClose={() => setSelectedAgent(null)}
          onDelete={handleDeleteAgent}
          onUpdated={handleAgentUpdated}
        />
      )}
    </div>
  );
}
