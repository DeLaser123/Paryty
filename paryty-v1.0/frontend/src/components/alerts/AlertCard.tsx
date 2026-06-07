/**
 * AlertCard — Individual alert card component.
 *
 * Shows severity badge, alert name, source node, triggered time,
 * duration, and acknowledge button for firing alerts.
 *
 * @module components/alerts/AlertCard
 */

import { memo, useCallback, useMemo } from 'react';
import { AlertTriangle, Info, Bell, CheckCircle2 } from 'lucide-react';
import clsx from 'clsx';
import type { Alert } from '../../types/alert';
import { useAlertsStore } from '../../stores/alertsStore';

/** Props for AlertCard. */
interface AlertCardProps {
  /** The alert to display. */
  alert: Alert;
  /** Callback when the card is selected. */
  onSelect: (alert: Alert) => void;
}

/** Map severity to icon. */
function getSeverityIcon(severity: Alert['severity']): React.ReactNode {
  switch (severity) {
    case 'critical':
      return <AlertTriangle size={14} />;
    case 'warning':
      return <Bell size={14} />;
    case 'info':
    default:
      return <Info size={14} />;
  }
}

/**
 * Format duration from startedAt to now.
 */
function formatDuration(startedAt: string): string {
  const ms = Date.now() - new Date(startedAt).getTime();
  if (ms < 60_000) return `${Math.floor(ms / 1000)}s`;
  if (ms < 3600_000) return `${Math.floor(ms / 60_000)}m`;
  if (ms < 86400_000) return `${Math.floor(ms / 3600_000)}h`;
  return `${Math.floor(ms / 86400_000)}d`;
}

/**
 * Individual alert card with severity styling and acknowledge action.
 *
 * Uses the container card pattern from the design system.
 * Severity colors applied as left border.
 */
export const AlertCard = memo(function AlertCard({ alert, onSelect }: AlertCardProps) {
  const acknowledgeAlert = useAlertsStore((s) => s.acknowledgeAlert);

  const handleAcknowledge = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      acknowledgeAlert(alert.id);
    },
    [acknowledgeAlert, alert.id],
  );

  const duration = useMemo(() => formatDuration(alert.startedAt), [alert.startedAt]);

  return (
    <div
      className={clsx(
        'alert-card',
        `severity-${alert.severity}`,
        `state-${alert.state}`,
      )}
      onClick={() => onSelect(alert)}
      data-testid={`alert-card-${alert.id}`}
    >
      <div className="alert-header">
        <span className="alert-severity">
          {getSeverityIcon(alert.severity)}
          {alert.severity}
        </span>
        <span className="alert-state">{alert.state}</span>
      </div>
      <div className="alert-body">
        <h4>{alert.ruleName}</h4>
        <p>Value: {alert.value} (threshold: {alert.threshold})</p>
        <p className="alert-time">
          Started: {new Date(alert.startedAt).toLocaleString()} · {duration}
        </p>
      </div>
      {alert.state === 'firing' && (
        <button
          className="aef-btn aef-btn-inactive"
          onClick={handleAcknowledge}
          data-testid={`alert-ack-${alert.id}`}
        >
          <CheckCircle2 size={12} />
          Acknowledge
        </button>
      )}
    </div>
  );
});
