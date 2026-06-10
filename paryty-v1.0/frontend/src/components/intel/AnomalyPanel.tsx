/**
 * AnomalyPanel — Active anomalies panel with severity filtering.
 *
 * Displays anomaly cards sorted by severity with expandable details,
 * score bars, contributing factors, and recommendations.
 *
 * @module components/intel/AnomalyPanel
 */

import { memo, useState, useCallback } from 'react';
import {
  AlertTriangle,
  AlertCircle,
  Info,
  ChevronDown,
  ChevronUp,
  ShieldCheck,
  Filter,
} from 'lucide-react';
import type { Anomaly, AnomalySeverity } from '../../types/intel';

// ─── Severity Badge ──────────────────────────────────────────────

const SEVERITY_CONFIG: Record<
  AnomalySeverity,
  { label: string; className: string; icon: React.ReactNode }
> = {
  critical: {
    label: 'CRIT',
    className: 'severity-critical',
    icon: <AlertTriangle size={12} />,
  },
  high: {
    label: 'HIGH',
    className: 'severity-high',
    icon: <AlertTriangle size={12} />,
  },
  medium: {
    label: 'MED',
    className: 'severity-medium',
    icon: <AlertCircle size={12} />,
  },
  low: {
    label: 'LOW',
    className: 'severity-low',
    icon: <Info size={12} />,
  },
  info: {
    label: 'INFO',
    className: 'severity-info',
    icon: <Info size={12} />,
  },
};

interface SeverityBadgeProps {
  severity: AnomalySeverity;
}

const SeverityBadge = memo(function SeverityBadge({
  severity,
}: SeverityBadgeProps) {
  const config = SEVERITY_CONFIG[severity];
  return (
    <span className={`severity-badge ${config.className}`}>
      {config.icon}
      {config.label}
    </span>
  );
});

// ─── Score Bar ───────────────────────────────────────────────────

interface ScoreBarProps {
  score: number;
}

const ScoreBar = memo(function ScoreBar({ score }: ScoreBarProps) {
  const pct = Math.round(score * 100);
  return (
    <div className="anomaly-score-bar" title={`Anomaly score: ${pct}%`}>
      <div
        className="anomaly-score-bar__fill"
        style={{ width: `${pct}%` }}
      />
      <span className="anomaly-score-bar__label">{pct}%</span>
    </div>
  );
});

// ─── Anomaly Card ────────────────────────────────────────────────

interface AnomalyCardProps {
  anomaly: Anomaly;
}

const AnomalyCard = memo(function AnomalyCard({ anomaly }: AnomalyCardProps) {
  const [expanded, setExpanded] = useState(false);

  const toggle = useCallback(() => {
    setExpanded((prev) => !prev);
  }, []);

  const time = new Date(anomaly.timestamp);
  const timeStr = `${time.getMonth() + 1}/${time.getDate()} ${time.getHours()}:${String(time.getMinutes()).padStart(2, '0')}`;

  return (
    <div
      className={`anomaly-card anomaly-card--${anomaly.severity}`}
      data-testid={`anomaly-card-${anomaly.id}`}
    >
      <button
        className="anomaly-card__header"
        onClick={toggle}
        aria-expanded={expanded}
        data-testid={`anomaly-toggle-${anomaly.id}`}
      >
        <div className="anomaly-card__header-left">
          <SeverityBadge severity={anomaly.severity} />
          <span className="anomaly-card__metric">{anomaly.metricName}</span>
        </div>
        <div className="anomaly-card__header-right">
          <span className="anomaly-card__time">{timeStr}</span>
          {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
        </div>
      </button>

      <div className="anomaly-card__body">
        <ScoreBar score={anomaly.score} />
        <p className="anomaly-card__explanation">{anomaly.explanation}</p>
      </div>

      {expanded && (
        <div className="anomaly-card__details" data-testid={`anomaly-details-${anomaly.id}`}>
          <div className="anomaly-card__detail-section">
            <h5 className="anomaly-card__detail-title">Type</h5>
            <span className="anomaly-card__detail-value">{anomaly.type}</span>
          </div>

          <div className="anomaly-card__detail-section">
            <h5 className="anomaly-card__detail-title">Detection Method</h5>
            <span className="anomaly-card__detail-value">{anomaly.detectionMethod}</span>
          </div>

          <div className="anomaly-card__detail-section">
            <h5 className="anomaly-card__detail-title">Agent</h5>
            <span className="anomaly-card__detail-value">{anomaly.agentId}</span>
          </div>

          {anomaly.contributingFactors.length > 0 && (
            <div className="anomaly-card__detail-section">
              <h5 className="anomaly-card__detail-title">Contributing Factors</h5>
              <ul className="anomaly-card__factors">
                {anomaly.contributingFactors.map((factor, i) => (
                  <li key={i} className="anomaly-card__factor">
                    {factor}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="anomaly-card__detail-section">
            <h5 className="anomaly-card__detail-title">Value at Anomaly</h5>
            <span className="anomaly-card__detail-value">
              {anomaly.value.toFixed(2)}
            </span>
          </div>
        </div>
      )}
    </div>
  );
});

// ─── Anomaly Panel ───────────────────────────────────────────────

interface AnomalyPanelProps {
  anomalies: Anomaly[];
  severityFilter: AnomalySeverity | null;
  onSeverityFilterChange: (severity: AnomalySeverity | null) => void;
}

/**
 * Panel displaying active anomalies with severity filtering.
 * Cards sorted by severity (critical first) then by score descending.
 */
export const AnomalyPanel = memo(function AnomalyPanel({
  anomalies,
  severityFilter,
  onSeverityFilterChange,
}: AnomalyPanelProps) {
  const handleFilterClick = useCallback(
    (severity: AnomalySeverity | null) => {
      onSeverityFilterChange(severityFilter === severity ? null : severity);
    },
    [severityFilter, onSeverityFilterChange],
  );

  const severities: AnomalySeverity[] = ['critical', 'high', 'medium', 'low', 'info'];

  return (
    <div className="aef-container-card" data-testid="anomaly-panel">
      <div className="aef-container-card__header">
        <div className="aef-container-card__icon"><AlertTriangle size={16} /></div>
        <h3 className="aef-container-card__title">Anomalies</h3>
        {anomalies.length > 0 && (
          <span className="anomaly-panel__count">{anomalies.length}</span>
        )}
      </div>

      <div className="aef-container-card__body">
        {/* Severity filter chips */}
        <div className="anomaly-panel__filters" data-testid="anomaly-filters">
          <Filter size={12} />
          {severities.map((sev) => {
            const config = SEVERITY_CONFIG[sev];
            const isActive = severityFilter === sev;
            return (
              <button
                key={sev}
                className={`anomaly-filter-chip ${isActive ? 'anomaly-filter-chip--active' : ''} ${config.className}`}
                onClick={() => handleFilterClick(sev)}
                data-testid={`anomaly-filter-${sev}`}
              >
                {config.label}
              </button>
            );
          })}
          {severityFilter && (
            <button
              className="anomaly-filter-chip anomaly-filter-chip--clear"
              onClick={() => onSeverityFilterChange(null)}
              data-testid="anomaly-filter-clear"
            >
              Clear
            </button>
          )}
        </div>

        {/* Anomaly list */}
        <div className="anomaly-panel__list aef-scroll-thin">
          {anomalies.length === 0 && (
            <div className="anomaly-panel__empty" data-testid="anomaly-empty">
              <ShieldCheck size={32} />
              <span>No anomalies detected</span>
              <span className="anomaly-panel__empty-sub">
                All metrics within expected bounds
              </span>
            </div>
          )}
          {anomalies.map((anomaly) => (
            <AnomalyCard key={anomaly.id} anomaly={anomaly} />
          ))}
        </div>
      </div>
    </div>
  );
});
