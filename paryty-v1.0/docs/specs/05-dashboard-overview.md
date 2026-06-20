# Step 5: Dashboard Overview — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### 1.1 What Exists

| Component | Status | Notes |
|-----------|--------|-------|
| Page Header | ✅ | Title + "New Digital Paryty" button with scroll-driven fade |
| Counter Strip | ✅ | 4 counters with horizontal scroll + wheel redirect |
| Two-Pane Layout | ✅ | 3fr/2fr grid with Info Pane and Main Pane |
| Your Digital Parytys | ✅ | Grid of twin cards with health dots, stats, ability badges |
| Top Agents | ✅ | List of top 5 agents sorted by last_seen |
| Recent Alerts | ✅ | List of top 5 alerts with severity badges |
| Metric Aggregations | ⚠️ Misleading | Shows twin.summary fields, NOT real metric data |
| Info Pane (Forecasts) | ✅ | Live Forecasts + Key Insights cards |
| Twin Detail Sub-View | ✅ | Full sub-view with metadata, agents, alerts |
| Creation Wizard | ✅ | 3-step modal |
| Counter Detail Modals | ✅ | Click counter → filtered twin list |
| Scroll-Driven Compact Mode | ✅ | data-scrolled at 0.6/0.3 hysteresis |
| Empty State | ✅ | Box icon + message + CTA |
| Error State | ✅ | AlertTriangle banner with retry |
| Loading Skeleton | ❌ | Only "Loading agents…" text |

### 1.2 What's Misleading

**Metric Aggregations Section:** Shows `twin.summary.health`, `nodeCount`, `activeAlerts`, `activeTraces`. These are static twin metadata, NOT time-series metric data. Section title implies CPU/memory/disk/network.

**Fix:** Either rename to "Twin Summary" or fetch real metric aggregations.

### 1.3 Missing from Spec

- **Twin deletion flow:** `dashboardStore.deleteTwin(id)` calls DELETE /api/v1/twins/${id}, removes from catalogue, clears activeTwinId if deleted twin was active, syncs to localStorage
- **localStorage persistence:** Key `paryty_active_twin`, stale ID validated against fetched twins on each fetchTwins()
- **Info Pane interaction:** Forecast/anomaly items are clickable and open detail modals with metric name, agent ID, confidence %, severity, score, contributing factors
- **DetachableCard component:** Used for Info Pane items
- **Counter detail modal accent system:** `aef-modal--accent-variant-a/b/live/warning/neutral` with colored top border stripe
- **URL deep-linking:** `?new=true` URL param auto-opens creation wizard (DashboardPage.tsx:1398-1404)
- **Accordion drill links:** Footer navigation to `/twins/new`, `/agents`, `/alerts`, `/metrics`, `/intel`

---

## 2. Target State

### User Journey

```
Load Dashboard
    → See Page Header (title + "New Digital Paryty" button)
    → See Counter Strip (Total Twins, Errors, Healthy, Needs Attention)
    → See Two-Pane Layout:
        LEFT: Accordion (Your Parytys → Top Agents → Recent Alerts → Metric Aggregations)
        RIGHT: Info Pane (Live Forecasts, Key Insights)
    → Scroll → Header fades, counters compact, button floats
    → Click Counter → Counter Detail Modal
    → Click Twin Card → Twin Detail Sub-View
    → Click "New Digital Paryty" → Creation Wizard
```

### Polling

| Data | Interval | Action |
|------|----------|--------|
| Twins | 30s | fetchTwins() |
| Agents | 15s | fetchAgents() |
| Alerts | 15s | fetchAlerts() |
| Forecasts | 60s | fetchForecasts() |

---

## 3. UI Specification

### 3.1 Page Header

- Title: `var(--aef-font-heading)`, 21px, 600, `var(--aef-text-primary)`
- Subtitle: `var(--aef-font-body)`, 12px, `var(--aef-text-secondary)`
- Scroll behavior: opacity lerps 1→0, translateY lerps 0→-8px

### 3.2 Counter Strip

| Counter | Label | Icon | Variant |
|---------|-------|------|---------|
| Total Twins | "Total Twins" | Server | neutral |
| Total Errors | "Total Errors" | AlertTriangle | variant-b (red-orange) |
| Healthy | "Healthy" | Heart | live (green) |
| Needs Attention | "Needs Attention" | AlertCircle | warning (amber) |

**Tokens:** `--aef-counter-neutral-bg`, `--aef-counter-variant-b`, `--aef-status-live`, `--aef-status-warning`

**Compact mode:** Font sizes decrease, icon scale decreases, gap decreases, border + shadow appear when stuck.

### 3.3 Digital Paryty Cards

```tsx
<article className="aef-container-card dp-card" tabIndex={0} role="button">
  <div className="aef-container-card__body dp-card__inner">
    <div className="dp-card__head">
      <HealthDot status={paryty.health} />
      <div className="dp-card__title-block">
        <div className="dp-card__name">{paryty.name}</div>
        <div className="dp-card__system">{paryty.systemLabel}</div>
        <span className="dp-active-pill">Active</span>
      </div>
    </div>
    <div className="dp-card__stats">...</div>
    <div className="dp-card__abilities">
      {enabledMetas.map((m) => <AbilityBadge key={m.id} ability={m} />)}
    </div>
    <div className="dp-card__footer">
      <span>Created {created}</span>
      <button>Open <ArrowUpRight /></button>
    </div>
  </div>
</article>
```

**Active state:** `--aef-selected-bg` border, left stripe, scale(1.01)

**Health Dot:** healthy→`--aef-status-live`, degraded→`--aef-status-warning`, unhealthy→`--aef-counter-variant-b`, unknown→`--aef-border-strong`

### 3.4 Accordion Sections

| Section | Icon | Title | Default |
|---------|------|-------|---------|
| 0 | Box | "Your Digital Parytys" | Expanded |
| 1 | Server | "Top Agents" | Collapsed |
| 2 | Bell | "Recent Alerts" | Collapsed |
| 3 | Cpu | "Metric Aggregations" | Collapsed |

Single-expand behavior. Chevron rotates 180° when expanded.

### 3.5 Empty State

```tsx
<div className="dp-empty">
  <Box size={40} />
  <p>No Digital Parytys yet</p>
  <p>Create your first Digital Paryty to start observing...</p>
  <button className="aef-btn aef-btn-active"><Plus /> New Digital Paryty</button>
</div>
```

### 3.6 Loading Skeleton (NOT IMPLEMENTED)

```css
.dp-skeleton {
  background: linear-gradient(90deg, var(--aef-surface-low) 25%, var(--aef-surface-card) 50%, var(--aef-surface-low) 75%);
  background-size: 200% 100%;
  animation: dp-skeleton-shimmer 1.5s ease-in-out infinite;
  border-radius: var(--aef-radius-control);
}
```

### 3.7 Scroll-Driven Compact Mode

```typescript
const onScroll = () => {
  const raw = Math.min(Math.max(scrollEl.scrollTop / 60, 0), 1);
  targetEl.style.setProperty('--scroll-progress', String(raw));
  
  // Hysteresis: enter at 0.6, exit at 0.3
  if (!compact && raw > 0.6) targetEl.setAttribute('data-scrolled', '');
  else if (compact && raw < 0.3) targetEl.removeAttribute('data-scrolled');
};
```

---

## 4. Implementation Tasks

| Priority | Task | File |
|----------|------|------|
| P1 | Fix Metric Aggregations (rename or fetch real data) | DashboardPage.tsx |
| P2 | Add loading skeleton states | DashboardPage.tsx, dashboard.css |
| P3 | Responsive two-pane stacking at <768px | dashboard.css |
| P4 | Counter strip responsive at narrow widths | dashboard.css |

---

## 5. Acceptance Criteria

| AC | Criterion |
|----|-----------|
| AC-01 | All 4 counters show correct values |
| AC-02 | Twin cards display health dots, stats, abilities |
| AC-03 | Active card shows selected state |
| AC-04 | Accordion single-expand works |
| AC-05 | Scroll-driven compact mode activates |
| AC-06 | Floating button appears when scrolled |
| AC-07 | Counter detail modal shows filtered twins |
| AC-08 | Empty state shows when no twins |
| AC-09 | Error state shows with retry |
| AC-10 | Metric Aggregations shows real data OR is renamed |
