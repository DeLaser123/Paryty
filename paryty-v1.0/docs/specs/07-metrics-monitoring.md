# Step 7: Metrics Monitoring — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### Critical Gap

**MetricChart exists but was NEVER wired into MetricsView.** Line 89-93 of MetricsView.tsx renders a placeholder div with text `"{X} series, {Y} data points"` instead of the actual `<MetricChart>` component. MetricCards and Sparkline also exist but are never imported.

### What Exists

| Component | File | Status |
|-----------|------|--------|
| MetricsView | `frontend/src/components/MetricsView.tsx` | Placeholder text, no chart |
| MetricChart | `frontend/src/components/metrics/MetricChart.tsx` | Recharts LineChart — COMPLETE but never imported |
| MetricCards | `frontend/src/components/metrics/MetricCards.tsx` | CPU/Mem/Disk/Net cards — COMPLETE but never imported |
| Sparkline | `frontend/src/components/metrics/Sparkline.tsx` | Tiny AreaChart — COMPLETE but never imported |
| metricsStore | `frontend/src/stores/metricsStore.ts` | Series, LTTB downsampling, streaming, ring buffer |
| useMetrics | `frontend/src/hooks/useMetrics.ts` | Fetches via rest.queryMetrics() with polling |
| Backend | `cluster/internal/api/query/rest.go` | POST /api/v1/metrics/query, GET /api/v1/metrics/names |
| Hot Storage | `cluster/internal/storage/hot/metrics_ops.go` | Dragonfly sorted-sets |
| Warm Storage | `cluster/internal/storage/warm/` | QuestDB ILP + query optimizer |
| Agent Scrapers | `agent/src/metal/` | CPU, memory, disk, network, process, container |

### 1.2 Frontend Data Pipeline

| Layer | Details |
|-------|----------|
| Processing Worker | `processingClient` → `processingWorker` bridge computes min/max/avg/p50/p90/p99 on shared thread |
| LTTB Downsampling | Largest-Triangle-Three-Buckets algorithm in metricsStore.ts, resolution targets: raw→10000, 1m→500, 5m→200, 1h→100, 1d→50 |
| Ring Buffer | Zero-allocation point merging with MAX_POINTS = 10000 via engine/ringBuffer.ts |
| Series Eviction | MAX_SERIES_NAMES = 50 with LRU eviction by seriesLastUpdated |
| Comparison Mode | `compareMode`, `comparedMetrics[]`, `toggleCompare()`, `comparedSeries()` for multi-metric overlay |
| Derived Metrics | `rate(series)` and `increase(series)` functions for per-second rate-of-change and cumulative increase |
| Streaming Controls | `isStreaming`, `streamingAgentId`, `startStream(agentId)`, `stopStream()` |
| Auto-Downsample | `autoDownsample`, `setAutoDownsample()`, `setResolution(res)` |

### 1.3 Known Issues

**Metric naming inconsistency:** Frontend uses `cpu_usage` (underscore) in MetricCards.tsx while backend uses `cpu.usage_percent` (dot notation). This mismatch needs resolution.

### Other Issues

1. No twin/agent selector
2. No time range presets (only raw datetime-local inputs)
3. No feature gate
4. No loading skeleton
5. No error retry
6. No export
7. No real-time indicator
8. No empty state guidance

---

## 2. Target State

**User Journey:** Select twin → See metric summary cards (CPU/Memory/Disk/Network with sparklines) → Click card → Detailed chart → Select time range → Chart renders → Real-time updates (pulsing indicator) → Export data

### Architecture

```
Rust Agent (metal scrapers)
  → gRPC StreamMetrics
  → Go Ingestion → Hot (Dragonfly) + Warm (QuestDB)
  → Go Query Service (rest.go)
  → Frontend useMetrics hook → metricsStore → MetricCards + MetricChart
```

---

## 3. UI Specification

### 3.1 Page Header

```tsx
<div style={{ display: 'flex', gap: 'var(--aef-space-4)', alignItems: 'center' }}>
  <h1>Metrics</h1>
  <ParytySelect options={agents} onChange={setSelectedAgent} />
  <TimeRangePicker presets={['1h', '6h', '24h', '7d', '30d']} />
  <div className="live-indicator" /> {/* Pulsing green dot when streaming */}
  <button className="aef-btn aef-btn-inactive"><Download /> Export</button>
</div>
```

### 3.2 Metric Summary Cards

Grid of 4 cards: CPU, Memory, Disk, Network

Each card: `--aef-counter-neutral-bg`, `--aef-counter-neutral-border`, icon, label, value, sparkline

**Variant logic:**
- < 60%: neutral
- 60-80%: active (`--aef-counter-active-bg`)
- > 80%: variant-b (`--aef-counter-variant-b`)

### 3.3 Detailed Metric Chart

```tsx
<ResponsiveContainer height={300}>
  <LineChart data={points}>
    <CartesianGrid stroke="var(--aef-border)" strokeDasharray="3 3" vertical={false} />
    <XAxis stroke="var(--aef-text-secondary)" tick={{ fontSize: 10 }} />
    <YAxis stroke="var(--aef-text-secondary)" tick={{ fontSize: 10 }} width={40} />
    <Tooltip contentStyle={{ background: 'var(--aef-surface-card)', border: '1px solid var(--aef-border)' }} />
    <Area fill="var(--aef-surface-mid)" fillOpacity={0.3} />
    <Line stroke="var(--aef-text-primary)" strokeWidth={1.5} dot={false} />
  </LineChart>
</ResponsiveContainer>
```

### 3.4 Time Range Selector

Button group: `1h`, `6h`, `24h`, `7d`, `30d` + custom datetime inputs

Active: `--aef-btn-active-bg`, `--aef-btn-active-text`
Inactive: `--aef-btn-inactive-bg`, `--aef-btn-inactive-border`

### 3.5 Aggregated Stats Panel

Below chart: min / max / avg / p50 / p90 / p99

### 3.6 Live Indicator

Pulsing dot: `--aef-status-live`, scale 1→2→1 with opacity fade

### 3.7 Empty State

```tsx
<div style={{
  background: 'var(--aef-surface-low)',
  border: '1px dashed var(--aef-border-strong)',
  borderRadius: 'var(--aef-radius-card)',
  padding: 'var(--aef-space-10)',
  textAlign: 'center',
}}>
  <Activity size={32} style={{ color: 'var(--aef-text-secondary)' }} />
  <p>No metrics collected yet</p>
  <p>Connect an agent to start collecting metrics.</p>
</div>
```

### 3.8 Feature Gate

When `planStore.hasFeature('metrics') === false`:
```tsx
<div style={{ padding: 'var(--aef-space-8)', textAlign: 'center' }}>
  <Lock size={24} />
  <h3>Metrics Monitoring</h3>
  <p>Upgrade your plan to access real-time metrics.</p>
  <button className="aef-btn aef-btn-active">Upgrade Plan</button>
</div>
```

---

## 4. API Contract

### 4.1 Single Metric Query

```json
POST /api/v1/metrics/query
{
  "agentId": "agent-uuid",
  "name": "cpu.usage_percent",
  "startTime": "2026-06-18T10:00:00Z",
  "endTime": "2026-06-18T11:00:00Z",
  "step": "1m",
  "aggregation": "avg"
}
```

**Response:** `[{ "name": "cpu.usage_percent", "points": [{ "timestamp": "...", "value": 45.2 }] }]`

### 4.2 Batch Query

```json
POST /api/v1/metrics/query
{
  "agentId": "agent-uuid",  // optional — omit for auto-discovery of tenant agents
  "names": ["cpu.usage_percent", "memory.usage_percent"],  // multi-metric query
  "startTime": "...",
  "endTime": "...",
  "step": "1m",
  "aggregation": "avg"
}
```

**Response includes** `labels: {}` field per series.

**Agent auto-discovery:** When no agentId provided, backend queries GetAllAgentStates().

### 4.3 Metric Name Discovery

`GET /api/v1/metrics/names` — returns available metric names from QuestDB with fallback to defaults.

### 4.4 Per-Agent Metrics

`GET /api/v1/metrics/:agent_id` — latest MetricBatch for agent

`GET /api/v1/metrics/:agent_id/aggregated` — aggregated metrics with min/max/avg/p50/p90/p99

---

## 5. Implementation Tasks

| Task | File | Description |
|------|------|-------------|
| T-01 | MetricsView.tsx | Wire MetricChart into MetricsView (replace placeholder) |
| T-02 | MetricsView.tsx | Import and render MetricCards |
| T-03 | MetricsView.tsx | Add twin/agent selector |
| T-04 | MetricsView.tsx | Add time range preset buttons |
| T-05 | MetricsView.tsx | Add live indicator |
| T-06 | MetricsView.tsx | Add feature gate |
| T-07 | New: MetricChartSkeleton.tsx | Loading skeleton |
| T-08 | MetricsView.tsx | Add error state with retry |
| T-09 | MetricsView.tsx | Add empty state |
| T-10 | New: metricExport.ts | CSV/JSON export |
| T-11 | MetricChart.tsx | Add area fill below line |
| T-12 | useMetrics.ts | Add agentId filtering |

---

## 6. Acceptance Criteria

| AC | Criterion |
|----|-----------|
| AC-01 | MetricCards render with real data |
| AC-02 | MetricChart renders below cards |
| AC-03 | Time range presets work |
| AC-04 | Twin/agent selector filters data |
| AC-05 | Live indicator shows when streaming |
| AC-06 | Historical data loads for past ranges |
| AC-07 | Export CSV/JSON works |
| AC-08 | Feature gate blocks non-metrics plans |
| AC-09 | Empty state shows when no data |
| AC-10 | Error state shows with retry |
