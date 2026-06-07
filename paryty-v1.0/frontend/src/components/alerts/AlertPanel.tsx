/**
 * AlertPanel — Slide-out detail panel for selected alert.
 *
 * Shows full alert details, rule definition, affected nodes, and
 * acknowledge/silence actions.
 *
 * @module components/alerts/AlertPanel
 */

import { memo, useCallback, useMemo } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { X, CheckCircle2, BellOff } from 'lucide-react';
import type { Alert } from '../../types/alert';
import { useAlertsStore } from '../../stores/alertsStore';

/** Props for AlertPanel. */
interface AlertPanelProps {
  /** The selected alert to display details for. */
  alert: Alert;
  /** Callback when the panel close button is clicked. */
  onClose: () => void;
}

/**
 * Slide-out detail panel for a selected alert.
 *
 * Uses container card pattern from the design system.
 * Animated with framer-motion slide-in from right.
 */
export const AlertPanel = memo(function AlertPanel({ alert, onClose }: AlertPanelProps) {
  const acknowledgeAlert = useAlertsStore((s) => s.acknowledgeAlert);
  const silenceAlert = useAlertsStore((s) => s.silenceAlert);
  const rules = useAlertsStore((s) => s.rules);

  const rule = useMemo(
    () => rules.find((r) => r.id === alert.ruleId),
    [rules, alert.ruleId],
  );

  const handleAcknowledge = useCallback(() => {
    acknowledgeAlert(alert.id);
    onClose();
  }, [acknowledgeAlert, alert.id, onClose]);

  const handleSilence = useCallback(() => {
    silenceAlert(alert.id, 3600_000); // Silence for 1 hour
    onClose();
  }, [silenceAlert, alert.id, onClose]);

  return (
    <AnimatePresence>
      <motion.div
        className="detail-panel aef-container-card"
        initial={{ x: '100%', opacity: 0 }}
        animate={{ x: 0, opacity: 1 }}
        exit={{ x: '100%', opacity: 0 }}
        transition={{ duration: 0.2, ease: [0.2, 0, 0, 1] }}
        data-testid="alert-panel"
      >
        {/* Header */}
        <div className="aef-container-card__header">
          <span className="aef-container-card__title aef-truncate">{alert.ruleName}</span>
          <span className={`aef-badge badge-${alert.severity === 'critical' ? 'warning' : alert.severity === 'warning' ? 'warning' : 'pending'}`}>
            {alert.severity}
          </span>
          <button
            className="aef-modal-close"
            onClick={onClose}
            aria-label="Close panel"
            data-testid="alert-panel-close"
          >
            <X size={16} />
          </button>
        </div>

        {/* Body */}
        <div className="aef-container-card__body aef-scroll">
          {/* Alert details */}
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">State</span>
            <span className="aef-stat-module__value">{alert.state}</span>
          </div>
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Value</span>
            <span className="aef-stat-module__value">{alert.value}</span>
          </div>
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Threshold</span>
            <span className="aef-stat-module__value">{alert.threshold}</span>
          </div>
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Started</span>
            <span className="aef-stat-module__value">{new Date(alert.startedAt).toLocaleString()}</span>
          </div>
          {alert.resolvedAt && (
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">Resolved</span>
              <span className="aef-stat-module__value">{new Date(alert.resolvedAt).toLocaleString()}</span>
            </div>
          )}

          {/* Rule info */}
          {rule && (
            <>
              <div className="aef-stat-module__label" style={{ marginTop: 'var(--aef-space-2)' }}>
                Rule
              </div>
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">Description</span>
                <span className="aef-stat-module__value">{rule.description}</span>
              </div>
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">Metric</span>
                <span className="aef-stat-module__value">{rule.metricName}</span>
              </div>
              <div className="aef-stat-module">
                <span className="aef-stat-module__label">Duration</span>
                <span className="aef-stat-module__value">{rule.duration}</span>
              </div>
            </>
          )}

          {/* Labels */}
          {Object.keys(alert.labels).length > 0 && (
            <>
              <div className="aef-stat-module__label" style={{ marginTop: 'var(--aef-space-2)' }}>
                Labels
              </div>
              {Object.entries(alert.labels).map(([key, value]) => (
                <div key={key} className="aef-stat-module">
                  <span className="aef-stat-module__label">{key}</span>
                  <span className="aef-stat-module__value aef-truncate">{value}</span>
                </div>
              ))}
            </>
          )}

          {/* Actions */}
          {alert.state === 'firing' && (
            <div className="alert-panel__actions">
              <button
                className="aef-btn aef-btn-active"
                onClick={handleAcknowledge}
                data-testid="alert-panel-ack"
              >
                <CheckCircle2 size={14} />
                Acknowledge
              </button>
              <button
                className="aef-btn aef-btn-inactive"
                onClick={handleSilence}
                data-testid="alert-panel-silence"
              >
                <BellOff size={14} />
                Silence (1h)
              </button>
            </div>
          )}
        </div>
      </motion.div>
    </AnimatePresence>
  );
});
