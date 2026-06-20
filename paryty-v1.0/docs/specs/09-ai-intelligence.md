# Step 9: AI Intelligence — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### What Exists

| Component | File | Status |
|-----------|------|--------|
| IntelView | `frontend/src/components/intel/IntelView.tsx` | Main intelligence view |
| ForecastCards | `frontend/src/components/intel/ForecastCards.tsx` | Forecast summary with Canvas sparklines |
| ForecastChart | `frontend/src/components/intel/ForecastChart.tsx` | Full forecast visualization |
| AnomalyPanel | `frontend/src/components/intel/AnomalyPanel.tsx` | Anomaly display with severity filtering |
| ModelAccuracy | `frontend/src/components/intel/ModelAccuracy.tsx` | Model accuracy display |
| intelStore | `frontend/src/stores/intelStore.ts` | Zustand store for forecasts, anomalies, accuracy |
| useIntel | `frontend/src/hooks/useIntel.ts` | React hook for intelligence lifecycle |
| Backend intel_handlers | `cluster/internal/api/query/intel_handlers.go` | REST endpoints for forecasting/anomaly |
| gRPC clients | `cluster/internal/intelligence/` | Forecasting + anomaly gRPC clients |
| Python ML service | `intelligence/` | Prophet, XGBoost, Isolation Forest, Autoencoder |
| Proto | `proto/paryty/v1/forecasting.proto`, `anomaly.proto` | gRPC service definitions |

### Critical Gaps

1. **No anomaly list REST endpoint** — anomalies only arrive via WebSocket
2. **Model accuracy returns zeros** when no models trained
3. **Forecast confidence bands may be zero** if model untrained
4. **No feature gate** for Pro+/Enterprise
5. **No tab navigation** in IntelView
6. **Missing anomaly detail modal**
7. **No real-time anomaly toast notifications**

### 1.3 Caching Strategy

- **Forecast cache:** 5-minute TTL (`forecasting_client.go:194`)
- **Anomaly cache:** 1-minute TTL (`anomaly_client.go:228`)
- **Cache key format:** `forecast:{tenant}:{agent}:{metric}` or `anomaly:{tenant}:{agent}:{metric}`

### 1.4 Frontend Processing

- **Forecast point cap:** 500 points max (`intelStore.ts:28`)
- **Anomaly deduplication** by ID (`intelStore.ts:246`)
- **Anomaly sorting** by severity then score (`intelStore.ts:341`)
- **Dynamic metric discovery** from data (`IntelView.tsx:44`)
- **Cross-metric anomaly detection:** `DetectCrossMetricAnomalies` RPC (`anomaly.proto:16`)

### 1.5 Current UI Layout

- IntelView uses **single grid layout** (not tabs) — all components visible simultaneously
- AnomalyPanel uses **expandable cards** with inline details (not modal)
- **No feature gate** for paryty_intel — always accessible
- **No real-time anomaly toast notifications**

---

## 2. Target State

**User Journey:** View forecasts → See anomaly detections → Check model accuracy → Trigger retraining → Understand AI predictions

---

## 3. UI Specification

### 3.1 Tab Navigation

```tsx
<div className="intel-tabs" role="tablist">
  <button role="tab" aria-selected={tab === 'forecasts'}>Forecasts</button>
  <button role="tab" aria-selected={tab === 'anomalies'}>Anomalies</button>
  <button role="tab" aria-selected={tab === 'models'}>Models</button>
</div>
```

### 3.2 Forecasts Tab

```tsx
<div className="intel-forecasts">
  <ForecastCards forecasts={forecasts} />
  <ForecastChart series={selectedForecast} />
  <div className="intel-confidence">
    <span>Confidence: {(overallConfidence * 100).toFixed(0)}%</span>
  </div>
</div>
```

**ForecastCard:** Each card shows metric name, 7-day sparkline, confidence band, horizon label. Uses `--aef-counter-neutral-*` tokens.

### 3.3 Anomalies Tab

```tsx
<div className="intel-anomalies">
  <FilterBar>
    <FilterChip value="critical" />
    <FilterChip value="high" />
    <FilterChip value="medium" />
    <FilterChip value="low" />
    <FilterChip value="info" />
  </FilterBar>
  
  {anomalies.map(anomaly => (
    <AnomalyRow key={anomaly.id} anomaly={anomaly} onClick={() => openDetail(anomaly)} />
  ))}
</div>
```

**AnomalyRow:** Severity dot, metric name, score, contributing factors, timestamp, severity badge.

### 3.4 Anomaly Detail Modal

```tsx
<Modal title="Anomaly Detail">
  <div className="dp-confirm-row">
    <span>Metric</span><span>{anomaly.metric}</span>
  </div>
  <div className="dp-confirm-row">
    <span>Severity</span><SeverityBadge severity={anomaly.severity} />
  </div>
  <div className="dp-confirm-row">
    <span>Score</span><span>{anomaly.score.toFixed(2)}</span>
  </div>
  <div className="dp-confirm-row">
    <span>Detected At</span><span>{formatTime(anomaly.timestamp)}</span>
  </div>
  <div className="dp-confirm-row">
    <span>Contributing Factors</span>
    <ul>{anomaly.factors.map(f => <li key={f}>{f}</li>)}</ul>
  </div>
  <div className="dp-confirm-row">
    <span>Recommended Action</span><span>{anomaly.recommendation}</span>
  </div>
</Modal>
```

### 3.5 Models Tab

```tsx
<div className="intel-models">
  <div className="aef-container-card">
    <div className="aef-container-card__header">
      <span>Model Accuracy</span>
    </div>
    <div className="aef-container-card__body">
      {models.map(model => (
        <ModelRow key={model.name} model={model} />
      ))}
    </div>
  </div>
  
  <button className="aef-btn aef-btn-active" onClick={handleRetrain}>
    <RefreshCw size={12} /> Retrain Models
  </button>
</div>
```

**ModelRow:** Model name, accuracy score, training status, last trained date.

### 3.6 Feature Gate

When `planStore.hasFeature('paryty_intel') === false`:
```tsx
<div style={{ padding: 'var(--aef-space-8)', textAlign: 'center' }}>
  <Brain size={24} style={{ color: 'var(--aef-text-secondary)' }} />
  <h3>AI Intelligence</h3>
  <p>Upgrade to Pro+ for forecasting, anomaly detection, and AI-powered insights.</p>
  <button className="aef-btn aef-btn-active">Upgrade Plan</button>
</div>
```

### 3.7 Real-Time Anomaly Toast

```tsx
<Toast
  type="warning"
  message={`Anomaly detected: ${anomaly.metric} (score: ${anomaly.score.toFixed(2)})`}
  icon={<AlertTriangle size={14} />}
  duration={8000}
/>
```

---

## 4. API Contract

### POST /api/v1/intel/forecast

```json
{ "metric": "cpu.usage_percent", "serviceId": "uuid", "horizonSeconds": 604800 }
```

**Response:** `{ "metric": "...", "predictions": [...], "overallConfidence": 0.85, "model": "ensemble" }`

### POST /api/v1/intel/anomalies/detect

```json
{ "metric": "cpu.usage_percent", "agentId": "uuid", "windowMinutes": 60, "sensitivity": 0.5 }
```

### GET /api/v1/intel/anomalies/list (NEW — currently missing)

**Query:** `?severity=critical&limit=50`

**Response:** `[{ "id": "uuid", "metric": "...", "severity": "critical", "score": 0.95, "timestamp": "...", "factors": [...], "recommendation": "..." }]`

### GET /api/v1/intel/models/accuracy

**Response:**
```json
{
  "cpu.usage_percent": {
    "bestModel": "ensemble",
    "weights": {"linear": 0.2, "prophet": 0.3, "xgboost": 0.5},
    "accuracy": {"linear": 0.72, "prophet": 0.81, "xgboost": 0.85},
    "lastTrained": "2026-06-18T10:00:00Z",
    "trainingSamples": 1000
  }
}
```

### 4.3 Batch Forecasting

POST /api/v1/intel/forecast/batch

```json
{
  "metrics": ["cpu.usage_percent", "memory.usage_percent", "disk.io_bytes"],
  "serviceId": "uuid",
  "horizonSeconds": 604800
}
```

**Response:** `{ "forecasts": [{ "metric": "...", "predictions": [...], "overallConfidence": 0.85, "model": "ensemble" }] }`

### 4.4 Anomaly Explanation

POST /api/v1/intel/anomalies/explain

```json
{ "anomalyId": "uuid" }
```

**Response:** `{ "anomalyId": "uuid", "metric": "...", "contributingFactors": [...], "explanation": "...", "suggestedActions": [...] }`

### 4.5 Model Retraining

POST /api/v1/intel/models/retrain

```json
{ "metric": "cpu.usage_percent", "force": false }
```

**Response:** `{ "status": "started", "estimatedDuration": "2m", "model": "ensemble" }`

---

## 5. WebSocket Channels

- `intel.anomalies` — Real-time anomaly detections
- `intel.forecasts` — Forecast updates
- `intel.drift` — Model drift events

---

## 6. Implementation Tasks

### Backend

| Task | File | Description |
|------|------|-------------|
| B-01 | intel_handlers.go | Add GET /intel/anomalies/list endpoint |
| B-02 | intel_handlers.go | Fix model accuracy to return real data (not zeros) |
| B-03 | New: anomaly_store.go | Store detected anomalies for REST querying |

### Frontend

| Task | File | Description |
|------|------|-------------|
| F-01 | IntelView.tsx | Add tab navigation (Forecasts/Anomalies/Models) |
| F-02 | New: AnomalyDetailModal.tsx | Anomaly detail modal |
| F-03 | IntelView.tsx | Add feature gate for paryty_intel |
| F-04 | intelStore.ts | Add fetchAnomalyList (REST) action |
| F-05 | IntelView.tsx | Add real-time anomaly toast |
| F-06 | IntelView.tsx | Add empty states for each tab |
| F-07 | IntelView.tsx | Add loading states |

---

## 7. Acceptance Criteria

| AC | Criterion |
|----|-----------|
| AC-01 | Forecasts render with real data |
| AC-02 | Anomaly list loads from REST endpoint |
| AC-03 | Anomaly detail modal shows all fields |
| AC-04 | Model accuracy shows real values (not zeros) |
| AC-05 | Retrain button triggers retraining |
| AC-06 | Feature gate blocks non-Pro+ plans |
| AC-07 | Real-time anomaly toast appears |
| AC-08 | Tab navigation works |
| AC-09 | Empty states show for each tab |
| AC-10 | Loading states show during fetch |
