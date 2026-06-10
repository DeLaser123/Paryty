/**
 * ModelAccuracy — Model accuracy dashboard table.
 *
 * Displays per-model accuracy (Linear, Prophet, XGBoost, Ensemble)
 * with weight bars, status indicators, and a retrain action.
 *
 * @module components/intel/ModelAccuracy
 */

import { memo, useCallback } from 'react';
import { RefreshCw, CheckCircle, AlertCircle, XCircle, Target } from 'lucide-react';
import type { ModelInfo } from '../../types/intel';

// ─── Status Indicator ────────────────────────────────────────────

type ModelStatus = 'trained' | 'stale' | 'not-trained';

function getModelStatus(
  lastTrained: string,
  accuracy: number,
): ModelStatus {
  if (accuracy === 0) return 'not-trained';
  const ageMs = Date.now() - new Date(lastTrained).getTime();
  const staleThreshold = 7 * 24 * 60 * 60 * 1000; // 7 days
  if (ageMs > staleThreshold) return 'stale';
  return 'trained';
}

interface StatusIndicatorProps {
  status: ModelStatus;
}

const StatusIndicator = memo(function StatusIndicator({
  status,
}: StatusIndicatorProps) {
  switch (status) {
    case 'trained':
      return (
        <span className="model-status model-status--trained">
          <CheckCircle size={12} />
          Trained
        </span>
      );
    case 'stale':
      return (
        <span className="model-status model-status--stale">
          <AlertCircle size={12} />
          Stale
        </span>
      );
    case 'not-trained':
      return (
        <span className="model-status model-status--not-trained">
          <XCircle size={12} />
          Not Trained
        </span>
      );
  }
});

// ─── Weight Bar ──────────────────────────────────────────────────

interface WeightBarProps {
  weight: number; // 0-1
}

const WeightBar = memo(function WeightBar({ weight }: WeightBarProps) {
  const pct = Math.round(weight * 100);
  return (
    <div className="model-weight-bar" title={`Ensemble weight: ${pct}%`}>
      <div
        className="model-weight-bar__fill"
        style={{ width: `${pct}%` }}
      />
      <span className="model-weight-bar__label">{pct}%</span>
    </div>
  );
});

// ─── Component ───────────────────────────────────────────────────

interface ModelAccuracyProps {
  modelAccuracy: Record<string, ModelInfo> | null;
  isRetraining: boolean;
  onRetrain: (metricName?: string) => void;
}

/**
 * Model accuracy table showing per-model metrics.
 * Columns: Model, Weight, MAPE (accuracy), Last Trained, Status.
 */
export const ModelAccuracy = memo(function ModelAccuracy({
  modelAccuracy,
  isRetraining,
  onRetrain,
}: ModelAccuracyProps) {
  const handleRetrain = useCallback(() => {
    onRetrain();
  }, [onRetrain]);

  if (!modelAccuracy) {
    return (
      <div className="aef-table-card" data-testid="model-accuracy">
        <div className="aef-table-card__header">
          <Target size={14} />
          Model Accuracy
        </div>
        <div className="model-accuracy__empty">
          <span>Loading model data…</span>
        </div>
      </div>
    );
  }

  const entries = Object.entries(modelAccuracy);
  const modelNames = [
    'Linear Regression',
    'Prophet',
    'XGBoost',
    'Ensemble',
  ];

  // Flatten all models from all metric entries
  const allModels: Array<{
    name: string;
    weight: number;
    accuracy: number;
    lastTrained: string;
    status: ModelStatus;
  }> = [];

  for (const name of modelNames) {
    // Find this model across all metric entries
    let totalWeight = 0;
    let totalAccuracy = 0;
    let count = 0;
    let latestTrained = '';

    for (const [, info] of entries) {
      const w = info.weights[name] ?? 0;
      const a = info.accuracy[name] ?? 0;
      totalWeight += w;
      totalAccuracy += a;
      count++;
      if (!latestTrained || info.lastTrained > latestTrained) {
        latestTrained = info.lastTrained;
      }
    }

    const avgWeight = count > 0 ? totalWeight / count : 0;
    const avgAccuracy = count > 0 ? totalAccuracy / count : 0;
    const status = getModelStatus(latestTrained, avgAccuracy);

    allModels.push({
      name,
      weight: avgWeight,
      accuracy: avgAccuracy,
      lastTrained: latestTrained,
      status,
    });
  }

  return (
    <div className="aef-table-card" data-testid="model-accuracy">
      <div className="aef-table-card__header">
        <Target size={14} />
        Model Accuracy
        <button
          className="aef-btn aef-btn-inactive model-accuracy__retrain"
          onClick={handleRetrain}
          disabled={isRetraining}
          data-testid="model-retrain-button"
          style={{ marginLeft: 'auto' }}
        >
          <RefreshCw size={12} className={isRetraining ? 'spin' : ''} />
          {isRetraining ? 'Retraining…' : 'Retrain'}
        </button>
      </div>

      <table className="aef-table">
        <thead>
          <tr>
            <th>Model</th>
            <th>Weight</th>
            <th>MAPE</th>
            <th>Last Trained</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {allModels.map((model) => (
            <tr key={model.name} data-testid={`model-row-${model.name.replace(/\s+/g, '-').toLowerCase()}`}>
              <td className="model-accuracy__model-name">{model.name}</td>
              <td>
                <WeightBar weight={model.weight} />
              </td>
              <td className="model-accuracy__mape">
                {model.accuracy > 0 ? `${model.accuracy.toFixed(1)}%` : '—'}
              </td>
              <td className="model-accuracy__date">
                {model.lastTrained
                  ? new Date(model.lastTrained).toLocaleDateString()
                  : '—'}
              </td>
              <td>
                <StatusIndicator status={model.status} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {/* Best model callout */}
      {entries.length > 0 && entries[0] && (
        <div className="model-accuracy__best">
          Best model for <strong>{entries[0][0]}</strong>:{' '}
          {entries[0][1].bestModel}
          <span className="model-accuracy__samples">
            ({entries[0][1].trainingSamples.toLocaleString()} samples)
          </span>
        </div>
      )}
    </div>
  );
});
