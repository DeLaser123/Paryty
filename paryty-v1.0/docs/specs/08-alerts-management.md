# Step 8: Alerts Management — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### Critical Gaps

1. **Acknowledge/Silence buttons only modify local Zustand state** — never call backend
2. **AlertPanel component exists but is never rendered** by any parent
3. **Frontend AlertState type missing 'acknowledged'**
4. **Backend GetAlerts returns events, not alerts from state machine**
5. **Missing silence/resolve REST endpoints**

### What Exists

| Component | File | Status |
|-----------|------|--------|
| AlertView | `frontend/src/components/alerts/AlertView.tsx` | Alert list view |
| alertsStore | `frontend/src/stores/alertsStore.ts` | Zustand store with fetch/acknowledge/silence |
| AlertPanel | `frontend/src/components/alerts/AlertPanel.tsx` | Detail panel — COMPLETE but never rendered |
| Backend state machine | `cluster/internal/storage/alert_ops.go` | 648 lines, fully implemented |
| Backend handlers | `cluster/internal/api/query/rest.go` | GetAlerts endpoint |

### 1.2 Backend Status Correction

- **Endpoint EXISTS** at rest.go:955 with full state machine integration
- GetAlerts calls `store.GetActiveAlerts()` which queries the alert state machine (not raw events)
- POST /alerts/rules endpoint exists returning 4 default rules (High CPU, Critical CPU, High Memory, High Disk)

### 1.3 Alert State Machine Details

**Valid transitions:**
```
firing → {resolved, acknowledged, silenced}
acknowledged → {firing, resolved, silenced}
silenced → {firing, resolved}
resolved → terminal (no further transitions)
```

**Deduplication:** `DeduplicateAlert()` with fingerprint from rule_id + sorted labels

**TTL:**
- 5min for active alerts
- 1min for resolved
- 1h for history

**Tenant-scoped Redis keys** with automatic cleanup

### 1.4 Frontend Alert Features

| Feature | Details |
|---------|---------|
| Alert grouping | `groupBy` (severity\|rule\|node), `setGroupBy()`, `groupedAlerts()` returns Map<string, Alert[]> |
| Alert trend | `alertTrend()` returns increasing\|decreasing\|stable based on firing count vs history |
| Alert history | `alertHistory: Alert[]` (capped at 100), acknowledge moves alert to history |
| AlertSilence type | id, alertFingerprint, reason, startsAt, endsAt, createdBy |
| WebSocket channels | `alerts` (high priority), `alerts.update` (high priority) |
| Local silence tracking | `silencedAlerts: Map<string, number>` (alertId → silencedUntil timestamp) |
| Filter UI | AlertView.tsx has ParytySelect dropdowns for state and severity filtering |

### 1.5 Missing REST Endpoints

| Endpoint | Status |
|----------|--------|
| POST /alerts/:id/silence | State machine supports silenced transition but no REST handler |
| POST /alerts/:id/resolve | State machine supports resolved transition but no REST handler |

### Backend State Machine (alert_ops.go)

Fully implemented with:
- State transitions: firing → acknowledged → silenced → resolved
- Deduplication by rule + tenant
- Tenant-scoped Redis keys
- Transition validation
- Auto-resolve after silence duration

---

## 2. Target State

**User Journey:** See alert list → Filter by severity/status → Click alert → See details → Acknowledge → Silence (with duration) → Resolve → Real-time new alert notifications

---

## 3. UI Specification

### 3.1 Alert List Page

```tsx
<div className="dp-page">
  <div className="dp-page__header">
    <h1>Alerts</h1>
    <FilterBar>
      <FilterGroup label="Severity">
        <FilterChip value="critical" color="var(--aef-counter-variant-b)" />
        <FilterChip value="warning" color="var(--aef-status-warning)" />
        <FilterChip value="info" color="var(--aef-text-secondary)" />
      </FilterGroup>
      <FilterGroup label="Status">
        <FilterChip value="firing" />
        <FilterChip value="acknowledged" />
        <FilterChip value="silenced" />
        <FilterChip value="resolved" />
      </FilterGroup>
    </FilterBar>
  </div>
  
  <div className="alerts-list">
    {filteredAlerts.map(alert => (
      <AlertRow key={alert.id} alert={alert} onClick={() => selectAlert(alert)} />
    ))}
  </div>
</div>
```

### 3.2 Alert Row

```tsx
<div className={clsx('dp-alert-row', severityClass)}>
  <SeverityIcon size={14} style={{ color: severityColor(alert.severity) }} />
  <div className="dp-alert-row__body">
    <div className="dp-alert-row__title">{alert.ruleName}</div>
    <div className="dp-alert-row__detail">
      Value: {alert.value} (threshold: {alert.threshold}) · {timeAgo}
    </div>
    <div className="dp-alert-row__tags">
      <span className="aef-meta-pill"><Eye /> {twinName}</span>
    </div>
  </div>
  <span className={clsx('aef-badge', severityBadgeClass)}>
    {alert.severity}
  </span>
  <span className={clsx('aef-badge', statusBadgeClass)}>
    {alert.status}
  </span>
</div>
```

**Severity colors:**
- critical: `--aef-counter-variant-b` (#DC4714), AlertTriangle icon
- warning: `--aef-status-warning` (#fb923c), AlertCircle icon
- info: `--aef-text-secondary` (#656565), Bell icon

**Status badges:**
- firing: pulsing red
- acknowledged: yellow
- silenced: gray
- resolved: green (`--aef-status-live`)

### 3.3 Alert Detail Panel

```tsx
<div className="aef-container-card" style={{ maxWidth: 480 }}>
  <div className="aef-container-card__header">
    <span>{alert.ruleName}</span>
    <button onClick={onClose}>✕</button>
  </div>
  <div className="aef-container-card__body">
    <div className="dp-confirm-row">
      <span>Severity</span>
      <SeverityBadge severity={alert.severity} />
    </div>
    <div className="dp-confirm-row">
      <span>Status</span>
      <StatusBadge status={alert.status} />
    </div>
    <div className="dp-confirm-row">
      <span>Value</span>
      <span>{alert.value}</span>
    </div>
    <div className="dp-confirm-row">
      <span>Threshold</span>
      <span>{alert.threshold}</span>
    </div>
    <div className="dp-confirm-row">
      <span>Triggered</span>
      <span>{new Date(alert.startedAt).toLocaleString()}</span>
    </div>
    <div className="dp-confirm-row">
      <span>Duration</span>
      <span>{formatDuration(alert.duration)}</span>
    </div>
    <div className="dp-confirm-row">
      <span>Twin</span>
      <span>{twinName}</span>
    </div>
    <p>{alert.description}</p>
  </div>
  <div className="aef-container-card__body">
    {alert.status === 'firing' && (
      <button className="aef-btn aef-btn-active" onClick={handleAcknowledge}>
        <Check size={12} /> Acknowledge
      </button>
    )}
    {(alert.status === 'firing' || alert.status === 'acknowledged') && (
      <>
        <button className="aef-btn aef-btn-inactive" onClick={handleSilence}>
          <VolumeX size={12} /> Silence
        </button>
        <button className="aef-btn aef-btn-inactive" onClick={handleResolve}>
          <CheckCircle size={12} /> Resolve
        </button>
      </>
    )}
  </div>
</div>
```

### 3.4 Silence Duration Picker

```tsx
<Modal title="Silence Alert">
  <p>Silence this alert for:</p>
  <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
    {['15m', '1h', '4h', '24h', 'forever'].map(d => (
      <button
        key={d}
        className={clsx('aef-btn', duration === d ? 'aef-btn-active' : 'aef-btn-inactive')}
        onClick={() => setDuration(d)}
      >
        {d}
      </button>
    ))}
  </div>
  <button className="aef-btn aef-btn-active" onClick={confirmSilence}>
    Silence Alert
  </button>
</Modal>
```

### 3.5 Alert Count Badge (Sidebar/Header)

```tsx
<span className="aef-badge" style={{
  background: firingCount > 0 ? 'var(--aef-counter-variant-b)' : 'var(--aef-border)',
  color: '#fff',
  minWidth: 20,
  textAlign: 'center',
}}>
  {firingCount}
</span>
```

### 3.6 Real-Time Alert Toast

```tsx
<Toast
  type="error"
  message={`New alert: ${alert.ruleName}`}
  icon={<AlertTriangle size={14} />}
  duration={5000}
/>
```

---

## 4. API Contract

### GET /api/v1/alerts

**Query:** `?status=firing&severity=critical&limit=50`

**Response:**
```json
[{
  "id": "uuid",
  "ruleName": "High CPU Usage",
  "severity": "critical",
  "status": "firing",
  "value": 95.2,
  "threshold": 90,
  "twinId": "uuid",
  "startedAt": "2026-06-18T10:00:00Z",
  "duration": 3600,
  "description": "CPU usage exceeded 90% for 5 minutes"
}]
```

### POST /api/v1/alerts/:id/acknowledge

**Response:** `{ "status": "acknowledged" }`

### POST /api/v1/alerts/:id/silence

**Request:** `{ "duration": "1h" }`
**Response:** `{ "status": "silenced", "silenceUntil": "..." }`

### POST /api/v1/alerts/:id/resolve

**Response:** `{ "status": "resolved" }`

---

## 5. Implementation Tasks

### Phase 1: Backend — Wire State Machine to REST

| Task | File | Description |
|------|------|-------------|
| B-01 | rest.go | Add POST /alerts/:id/acknowledge endpoint |
| B-02 | rest.go | Add POST /alerts/:id/silence endpoint |
| B-03 | rest.go | Add POST /alerts/:id/resolve endpoint |
| B-04 | rest.go | Fix GetAlerts to query alert state machine |

### Phase 2: Frontend — Wire Buttons to Backend

| Task | File | Description |
|------|------|-------------|
| F-01 | alertsStore.ts | Update acknowledgeAlert to call API |
| F-02 | alertsStore.ts | Update silenceAlert to call API |
| F-03 | alertsStore.ts | Add resolveAlert action |
| F-04 | AlertView.tsx | Render AlertPanel component |
| F-05 | AlertView.tsx | Add severity/status filter UI |

### Phase 3: Frontend — Polish

| Task | File | Description |
|------|------|-------------|
| F-06 | New: AlertToast.tsx | Real-time alert toast notification |
| F-07 | AlertView.tsx | Add alert count badge |
| F-08 | types/alert.ts | Fix AlertState to include 'acknowledged' |
| F-09 | Sidebar.tsx | Add alert count to sidebar |

---

## 6. Acceptance Criteria

| AC | Criterion |
|----|-----------|
| AC-01 | Acknowledge button calls backend |
| AC-02 | Silence button shows duration picker, calls backend |
| AC-03 | Resolve button calls backend |
| AC-04 | Alert status updates after action |
| AC-05 | AlertPanel detail renders when clicking alert |
| AC-06 | Severity filter works |
| AC-07 | Status filter works |
| AC-08 | Real-time alerts appear via WebSocket |
| AC-09 | Alert count badge updates |
| AC-10 | Empty state shows when no alerts |
