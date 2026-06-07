/**
 * AlertList — Filterable list of alerts.
 *
 * Supports filtering by state (firing/resolved) and severity (critical/warning/info).
 * Renders AlertCard for each alert with group headers when groupBy is set.
 *
 * @module components/alerts/AlertList
 */

import { memo, useCallback, useState } from 'react';
import { useAlertsStore, type AlertGroupBy } from '../../stores/alertsStore';
import { AlertCard } from './AlertCard';
import type { Alert, AlertState } from '../../types/alert';

/**
 * Filterable, groupable alert list.
 *
 * Uses alertsStore for data and filtering.
 * Renders group headers when `groupBy` is active.
 */
export const AlertList = memo(function AlertList() {
  const filterState = useAlertsStore((s) => s.filterState);
  const filterSeverity = useAlertsStore((s) => s.filterSeverity);
  const groupBy = useAlertsStore((s) => s.groupBy);
  const filteredAlerts = useAlertsStore((s) => s.filteredAlerts());
  const groupedAlerts = useAlertsStore((s) => s.groupedAlerts());
  const setFilterState = useAlertsStore((s) => s.setFilterState);
  const setFilterSeverity = useAlertsStore((s) => s.setFilterSeverity);
  const setGroupBy = useAlertsStore((s) => s.setGroupBy);
  const selectAlert = useAlertsStore((s) => s.selectAlert);
  const isLoading = useAlertsStore((s) => s.isLoading);

  const [useGroups, setUseGroups] = useState(false);

  const handleFilterState = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const val = e.target.value;
      setFilterState(val ? (val as AlertState) : null);
    },
    [setFilterState],
  );

  const handleFilterSeverity = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const val = e.target.value;
      setFilterSeverity(val || null);
    },
    [setFilterSeverity],
  );

  const handleGroupBy = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const val = e.target.value as AlertGroupBy;
      setGroupBy(val);
      setUseGroups(true);
    },
    [setGroupBy],
  );

  if (isLoading) {
    return (
      <div className="aef-viz-well" data-testid="alert-list-loading">
        <span className="aef-viz-well__label">Loading alerts…</span>
      </div>
    );
  }

  const renderAlerts = (alertList: Alert[]) =>
    alertList.map((alert) => (
      <AlertCard key={alert.id} alert={alert} onSelect={selectAlert} />
    ));

  return (
    <div className="alert-list-container" data-testid="alert-list">
      {/* Filters */}
      <div className="alert-list__filters">
        <select
          value={filterState ?? ''}
          onChange={handleFilterState}
          data-testid="alert-filter-state"
        >
          <option value="">All states</option>
          <option value="firing">Firing</option>
          <option value="pending">Pending</option>
          <option value="resolved">Resolved</option>
          <option value="silenced">Silenced</option>
        </select>
        <select
          value={filterSeverity ?? ''}
          onChange={handleFilterSeverity}
          data-testid="alert-filter-severity"
        >
          <option value="">All severities</option>
          <option value="critical">Critical</option>
          <option value="warning">Warning</option>
          <option value="info">Info</option>
        </select>
        <select
          value={groupBy}
          onChange={handleGroupBy}
          data-testid="alert-group-by"
        >
          <option value="severity">Group by Severity</option>
          <option value="rule">Group by Rule</option>
          <option value="node">Group by Node</option>
        </select>
      </div>

      {/* Alert list */}
      <div className="alert-list__items aef-scroll">
        {filteredAlerts.length === 0 && (
          <div className="aef-viz-well" data-testid="alert-list-empty">
            <span className="aef-viz-well__label">No alerts matching filters</span>
          </div>
        )}

        {useGroups ? (
          Array.from(groupedAlerts.entries()).map(([group, groupAlerts]) => (
            <div key={group} className="alert-list__group">
              <div className="alert-list__group-header">
                <span>{group}</span>
                <span className="aef-meta-pill">{groupAlerts.length}</span>
              </div>
              {renderAlerts(groupAlerts)}
            </div>
          ))
        ) : (
          renderAlerts(filteredAlerts)
        )}
      </div>
    </div>
  );
});
