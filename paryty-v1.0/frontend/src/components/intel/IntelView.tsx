/**
 * IntelView — Main Paryty Intelligence dashboard.
 *
 * Composes ForecastCards, ForecastChart, AnomalyPanel, and
 * ModelAccuracy into a responsive CSS Grid layout.
 *
 * @module components/intel/IntelView
 */

import { useCallback, useEffect } from 'react';
import { Brain, RefreshCw } from 'lucide-react';
import { useIntel } from '../../hooks/useIntel';
import { useIntelStore } from '../../stores/intelStore';
import { ForecastCards } from './ForecastCards';
import { ForecastChart } from './ForecastChart';
import { AnomalyPanel } from './AnomalyPanel';
import { ModelAccuracy } from './ModelAccuracy';
import { ParytySelect } from '../common/ParytySelect';
import { HORIZON_PRESETS } from '../../types/intel';
import type { AnomalySeverity } from '../../types/intel';

/**
 * Paryty Intelligence dashboard view.
 *
 * Layout:
 * - Header: title, horizon selector, refresh button
 * - Row 1: ForecastCards (4 metric summary cards)
 * - Row 2: ForecastChart (detailed chart with confidence bands)
 * - Row 3: AnomalyPanel (left) + ModelAccuracy (right)
 */
export default function IntelView() {
  const intel = useIntel();

  // Get stable action references via selectors
  const fetchForecasts = useIntelStore((s) => s.fetchForecasts);
  const fetchDetectionStatus = useIntelStore((s) => s.fetchDetectionStatus);
  const fetchModelAccuracy = useIntelStore((s) => s.fetchModelAccuracy);
  const setHorizon = useIntelStore((s) => s.setHorizon);
  const setSelectedMetric = useIntelStore((s) => s.setSelectedMetric);
  const setSeverityFilter = useIntelStore((s) => s.setSeverityFilter);
  const retrainModels = useIntelStore((s) => s.retrainModels);

  // Re-fetch forecasts when horizon changes
  useEffect(() => {
    const metricNames = ['cpu_usage_percent', 'memory_usage_percent', 'disk_usage_percent', 'network_io_bytes'];
    fetchForecasts(metricNames);
  }, [intel.horizonSeconds, fetchForecasts]);

  const handleRefresh = useCallback(() => {
    const metricNames = ['cpu_usage_percent', 'memory_usage_percent', 'disk_usage_percent', 'network_io_bytes'];
    fetchForecasts(metricNames);
    fetchDetectionStatus();
    fetchModelAccuracy();
  }, [fetchForecasts, fetchDetectionStatus, fetchModelAccuracy]);

  const handleHorizonChange = useCallback(
    (val: string) => {
      setHorizon(Number(val));
    },
    [setHorizon],
  );

  const handleMetricChange = useCallback(
    (metric: string) => {
      setSelectedMetric(metric);
    },
    [setSelectedMetric],
  );

  const handleSeverityFilterChange = useCallback(
    (severity: AnomalySeverity | null) => {
      setSeverityFilter(severity);
    },
    [setSeverityFilter],
  );

  const handleRetrain = useCallback(
    (metricName?: string) => {
      retrainModels(metricName);
    },
    [retrainModels],
  );

  return (
    <div className="intel-view" data-testid="intel-view">
      {/* ─── Header ─────────────────────────────────────────────── */}
      <div className="intel-view__header">
        <div className="intel-view__title-row">
          <Brain size={20} className="intel-view__brain-icon" />
          <h2 className="intel-view__title">Paryty Intelligence</h2>
        </div>

        <div className="intel-view__controls">
          {/* Horizon selector */}
          <ParytySelect
            options={HORIZON_PRESETS.map((p) => ({ label: p.label, value: String(p.seconds) }))}
            value={String(intel.horizonSeconds)}
            onChange={handleHorizonChange}
            placeholder="Horizon"
            testId="intel-horizon-select"
          />

          {/* Refresh button */}
          <button
            className="intel-view__refresh-btn"
            onClick={handleRefresh}
            disabled={intel.isLoading}
            data-testid="intel-refresh-button"
            aria-label="Refresh intelligence data"
          >
            <RefreshCw size={14} className={intel.isLoading ? 'spin' : ''} />
            Refresh
          </button>
        </div>
      </div>

      {/* ─── Loading / Error States ─────────────────────────────── */}
      {intel.error && (
        <div className="intel-view__error" data-testid="intel-error" role="alert">
          {intel.error}
        </div>
      )}

      {/* ─── Grid Layout ────────────────────────────────────────── */}
      <div className="intel-grid">
        {/* Row 1: Forecast summary cards */}
        <div className="intel-grid__cards">
          <ForecastCards forecasts={intel.forecasts} />
        </div>

        {/* Row 2: Detailed forecast chart */}
        <div className="intel-grid__chart">
          <ForecastChart
            series={intel.forecasts.get(intel.selectedMetric)}
            selectedMetric={intel.selectedMetric}
            onMetricChange={handleMetricChange}
          />
        </div>

        {/* Row 3: Anomaly panel + Model accuracy */}
        <div className="intel-grid__bottom">
          <AnomalyPanel
            anomalies={intel.sortedAnomalies()}
            severityFilter={intel.severityFilter}
            onSeverityFilterChange={handleSeverityFilterChange}
          />
          <ModelAccuracy
            modelAccuracy={intel.modelAccuracy}
            isRetraining={intel.isRetraining}
            onRetrain={handleRetrain}
          />
        </div>
      </div>

      {/* Loading overlay */}
      {intel.isLoading && intel.forecasts.size === 0 && (
        <div className="intel-view__loading" data-testid="intel-loading">
          <RefreshCw size={24} className="spin" />
          <span>Loading intelligence data…</span>
        </div>
      )}
    </div>
  );
}
