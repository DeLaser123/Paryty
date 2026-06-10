import { useAlerts } from '../hooks/useAlerts';
import { ParytySelect } from './common/ParytySelect';

export default function AlertView() {
  const alerts = useAlerts();

  return (
    <div className="view-container">
      <div className="view-header">
        <h2>Alerts ({alerts.firingCount()} firing)</h2>
        <div className="view-controls">
          <ParytySelect
            options={[
              { label: 'All states', value: '' },
              { label: 'Firing', value: 'firing' },
              { label: 'Pending', value: 'pending' },
              { label: 'Resolved', value: 'resolved' },
              { label: 'Silenced', value: 'silenced' },
            ]}
            value={alerts.filterState ?? ''}
            onChange={(val) => alerts.setFilterState(val as 'firing' | 'resolved' | 'pending' | null || null)}
            placeholder="All states"
          />
          <ParytySelect
            options={[
              { label: 'All severities', value: '' },
              { label: 'Critical', value: 'critical' },
              { label: 'Warning', value: 'warning' },
              { label: 'Info', value: 'info' },
            ]}
            value={alerts.filterSeverity ?? ''}
            onChange={(val) => alerts.setFilterSeverity(val || null)}
            placeholder="All severities"
          />
        </div>
      </div>
      <div className="alerts-content">
        {alerts.isLoading && <div className="loading">Loading alerts...</div>}
        {alerts.error && <div className="error">{alerts.error}</div>}
        {alerts.filteredAlerts().length === 0 && !alerts.isLoading && (
          <div className="empty-state">
            <p>No alerts matching filters</p>
          </div>
        )}
        <div className="alert-list">
          {alerts.filteredAlerts().map((alert) => (
            <div
              key={alert.id}
              className={`alert-card severity-${alert.severity} state-${alert.state}`}
              onClick={() => alerts.selectAlert(alert)}
            >
              <div className="alert-header">
                <span className="alert-severity">{alert.severity}</span>
                <span className="alert-state">{alert.state}</span>
              </div>
              <div className="alert-body">
                <h4>{alert.ruleName}</h4>
                <p>Value: {alert.value} (threshold: {alert.threshold})</p>
                <p className="alert-time">Started: {alert.startedAt}</p>
              </div>
              {alert.state === 'firing' && (
                <button
                  className="aef-btn aef-btn-inactive"
                  onClick={(e) => {
                    e.stopPropagation();
                    alerts.selectAlert(alert);
                  }}
                >
                  Acknowledge
                </button>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
