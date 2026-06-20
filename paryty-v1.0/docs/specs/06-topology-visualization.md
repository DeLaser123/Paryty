# Step 6: Topology Visualization — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### What Exists

| Component | File | Status |
|-----------|------|--------|
| TopologyCanvas | `frontend/src/components/topology/TopologyCanvas.tsx` | PixiJS GPU renderer |
| topologyStore | `frontend/src/stores/topologyStore.ts` | Nodes, edges, filters, selection |
| NodeDetailPanel | `frontend/src/components/topology/NodeDetailPanel.tsx` | Slide-in detail |
| EdgeDetailPanel | `frontend/src/components/topology/EdgeDetailPanel.tsx` | Edge info |
| SearchOverlay | `frontend/src/components/topology/SearchOverlay.tsx` | Find nodes |
| ClusterBreadcrumb | `frontend/src/components/topology/ClusterBreadcrumb.tsx` | Navigation |
| TopologyControls | `frontend/src/components/topology/TopologyControls.tsx` | Zoom/fit/layout |
| WebSocket | `frontend/src/api/websocket.ts` | topology channel (critical priority) |

### Major Gaps

1. **Edge-flow particles** — Current particles are event-driven, not continuous data flow
2. **Edge click interaction** — No hit-test zones for edges
3. **Loading skeleton graph** — Text-only loading state
4. **Filter controls UI** — Store has `filterTypes` but no health/protocol filters
5. **Health history sparklines** — Not in detail panel
6. **Metric badges on nodes** — Not rendered
7. **Edge protocol labels** — Not displayed
8. **Directional arrows** — Not on edges

---

## 2. Target State

**User Journey:** Load → See system map → Nodes with health colors → Edges with data flow particles → Zoom/pan → Click node → Detail panel → Search → Filter by type → Breadcrumb navigation

---

## 3. Engine Architecture

| Component | File | Lines | Description |
|-----------|------|-------|-------------|
| PixiJS Application | `frontend/src/engine/pixiApp.ts` | 1038 | Full GPU rendering pipeline |
| TopologyRenderer | `frontend/src/engine/renderer.ts` | 526 | Wraps PixiJS app |
| Worker Bridge | `frontend/src/engine/renderBridge.ts` | 351 | Offloaded rendering + renderWorker.ts |
| Memory Budget | `frontend/src/engine/memoryBudget.ts` | 160 | Degrades rendering when memory pressure high |
| Layout Engine | `frontend/src/engine/processing/layout.ts` | 576 | 3 modes: force, hierarchical, radial |
| Clustering | `frontend/src/engine/clustering.ts` | 398 | Node clustering at zoom levels |
| Instanced Rendering | `frontend/src/engine/instancing.ts` | 491 | Efficient node rendering |
| Effects | `frontend/src/engine/effects.ts` | 526 | Glow, pulse effects |
| Viewport | `frontend/src/engine/viewport.ts` | 485 | Zoom/pan with constraints |

---

## 4. Backend Filtering

- **Process node filtering:** `filterOutProcessNodes()` at rest.go:293-339 removes process-type nodes from global view
- **Edge metrics in DTOs:** `FrontendTopologyEdge` includes LatencyMs, BytesPerSec, ErrorRate

---

## 5. Store UI State

- **Timeline drawer:** `timelineDrawerOpen`, `toggleTimelineDrawer()`
- **Metrics animations:** `metricsAnimationsEnabled`, `toggleMetricsAnimations()`
- **Node labels:** `nodeLabelsVisible`, `toggleNodeLabels()`
- **StrictMode guard:** Double-init guard via `rendererCreatedRef` prevents two renderers in dev

---

## 6. UI Specification

### 6.1 Canvas Area
- Full viewport, `--aef-canvas-bg: #000000` background
- PixiJS Stage with Container hierarchy: edges layer → nodes layer → particles layer → UI overlay

### 6.2 Node Rendering
- **Shape:** Rounded rectangle or circle based on type (service, host, container, process)
- **Health dot:** `--aef-status-live` (healthy), `--aef-status-warning` (degraded), `--aef-counter-variant-b` (unhealthy)
- **Label:** `--aef-text-primary`, `--aef-font-body`, 11px
- **Metric badges:** CPU %, Memory % in tiny pills below label
- **Hover:** Scale 1.05x, glow effect
- **Selected:** `--aef-selected-bg` border

### 6.3 Edge Rendering
- **Line:** 1px, `--aef-border` color, dashed or solid based on protocol
- **Protocol label:** Tiny text at midpoint (HTTP, gRPC, TCP)
- **Directional arrow:** Triangle at target end
- **Error state:** `--aef-counter-variant-b` color when error_rate > 0

### 6.4 Particle System
- **Data flow particles:** Small circles moving along edges
- **Color by protocol:** HTTP→`--aef-transport-rest`, gRPC→`--aef-transport-ws`, TCP→`--aef-transport-sse`
- **Speed:** Proportional to bytes_per_sec
- **Density:** Proportional to request_rate
- **Continuous flow** (not event-driven)

### 6.5 Node Detail Panel
- Slide-in from right, 320px wide
- **Info:** Name, type, health, CPU sparkline, Memory sparkline, request rate, error rate, p99 latency
- **Connections:** List of connected nodes with edge stats
- **Health history:** 24h sparkline chart
- **Close:** X button or click outside

### 6.6 Search Overlay
- Cmd+K shortcut
- Search by node name, label, type
- Results list with node type icon + name + health dot
- Click result → select and center node

### 6.7 Filter Controls
- Filter by: node type (service/host/container/process), health (healthy/degraded/unhealthy), protocol
- Toggle buttons using `--aef-btn-*` tokens
- Active filters shown as pills with X to remove

### 6.8 Breadcrumb Navigation
```
Cluster > Service > Container
```
- Each level clickable to navigate back
- `--aef-text-secondary` for non-current, `--aef-text-primary` for current

---

## 7. WebSocket Integration

**Channel:** `topology` (critical priority), `topology.diff` (incremental updates)

**Message types:**
- `topology.snapshot` — Full graph (on connect)
- `topology.diff` — Incremental updates (node add/remove/update, edge add/remove/update)
- `topology.health` — Health status changes

**Reconnection:** 5-second grace window, exponential backoff

---

## 8. Performance

- **Node limit:** 500 visible nodes (LOD beyond)
- **Edge limit:** 1000 visible edges
- **LOD:** At zoom < 0.5x, hide labels and badges; at < 0.25x, simplify to dots
- **Culling:** Only render nodes/edges in viewport
- **SharedArrayBuffer:** For topology layout worker

---

## 9. Acceptance Criteria

| AC | Criterion |
|----|-----------|
| AC-01 | Nodes render with health colors |
| AC-02 | Edges connect correct nodes |
| AC-03 | Particles flow along edges |
| AC-04 | Click node opens detail panel |
| AC-05 | Search finds nodes by name |
| AC-06 | Filter by type/health/protocol |
| AC-07 | Zoom/pan works smoothly |
| AC-08 | Real-time updates via WebSocket |
| AC-09 | Breadcrumb navigation works |
| AC-10 | Loading skeleton shows during fetch |

---

## 10. Implementation Tasks

| Task | File | Description |
|------|------|-------------|
| T-01 | TopologyCanvas.tsx | Add metric badges to node rendering |
| T-02 | TopologyCanvas.tsx | Add protocol labels to edges |
| T-03 | TopologyCanvas.tsx | Implement continuous edge-flow particles |
| T-04 | TopologyCanvas.tsx | Add edge click hit-test zones |
| T-05 | New: FilterControls.tsx | Health/protocol/type filter UI |
| T-06 | TopologyCanvas.tsx | Add loading skeleton graph |
| T-07 | TopologyCanvas.tsx | Add empty state |
| T-08 | TopologyCanvas.tsx | Add health change flash animation |
| T-09 | ClusterBreadcrumb.tsx | Add direct navigation on click |
| T-10 | NodeDetailPanel.tsx | Add health history sparklines |
| T-11 | topologyStore.ts | Add health/protocol filter state |
