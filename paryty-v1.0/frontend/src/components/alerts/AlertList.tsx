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
import { ParytySelect } from '../common/ParytySelect';
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
    (val: string) => {
      setFilterState(val ? (val as AlertState) : null);
    },
    [setFilterState],
  );

  const handleFilterSeverity = useCallback(
    (val: string) => {
      setFilterSeverity(val || null);
    },
    [setFilterSeverity],
  );

  const handleGroupBy = useCallback(
    (val: string) => {
      setGroupBy(val as AlertGroupBy);
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
        <ParytySelect
          options={[
            { label: 'All states', value: '' },
            { label: 'Firing', value: 'firing' },
            { label: 'Pending', value: 'pending' },
            { label: 'Resolved', value: 'resolved' },
            { label: 'Silenced', value: 'silenced' },
          ]}
          value={filterState ?? ''}
          onChange={handleFilterState}
          placeholder="All states"
          testId="alert-filter-state"
        />
        <ParytySelect
          options={[
            { label: 'All severities', value: '' },
            { label: 'Critical', value: 'critical' },
            { label: 'Warning', value: 'warning' },
            { label: 'Info', value: 'info' },
          ]}
          value={filterSeverity ?? ''}
          onChange={handleFilterSeverity}
          placeholder="All severities"
          testId="alert-filter-severity"
        />
        <ParytySelect
          options={[
            { label: 'Group by Severity', value: 'severity' },
            { label: 'Group by Rule', value: 'rule' },
            { label: 'Group by Node', value: 'node' },
          ]}
          value={groupBy}
          onChange={handleGroupBy}
          testId="alert-group-by"
        />
      </div>

      {/* Alert list */}
      <div className="alert-list__items aef-scroll-thin">
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
