import { useAlerts } from '../hooks/useAlerts';

export default function AlertView() {
  const alerts = useAlerts();

  return (
    <div className="view-container">
      <div className="view-header">
        <h2>Alerts ({alerts.firingCount()} firing)</h2>
        <div className="view-controls">
          <select
            value={alerts.filterState ?? ''}
            onChange={(e) => alerts.setFilterState(e.target.value as 'firing' | 'resolved' | 'pending' | null || null)}
          >
            <option value="">All states</option>
            <option value="firing">Firing</option>
            <option value="pending">Pending</option>
            <option value="resolved">Resolved</option>
            <option value="silenced">Silenced</option>
          </select>
          <select
            value={alerts.filterSeverity ?? ''}
            onChange={(e) => alerts.setFilterSeverity(e.target.value || null)}
          >
            <option value="">All severities</option>
            <option value="critical">Critical</option>
            <option value="warning">Warning</option>
            <option value="info">Info</option>
          </select>
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
