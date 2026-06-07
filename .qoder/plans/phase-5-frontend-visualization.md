# Phase 5: Frontend Visualization — Implementation Plan

## Context

Phase 5 transforms the Paryty frontend from basic placeholders into a production-grade, GPU-accelerated observability dashboard. The current frontend has functional stores, hooks, and API layers, but the rendering engine uses individual `PIXI.Graphics` per node (scales to ~500), has no viewport controls, no particle system, and no real UI shell. The user has provided a complete design system (`src/paryty_design_system/`) that defines the strict visual language.

**Target:** ~12,000 LOC across ~50 TypeScript/React files.
**Spec:** `paryty-v1.0/docs/development/phase-5-hardened-spec.md` (1,884 lines)

---

## Design System Constraints (HARD RULES)

The user's design system at `src/paryty_design_system/` is the **authoritative** source for all styling. The spec's placeholder CSS values are OVERRIDDEN by these:

| Constraint | Value | Source |
|---|---|---|
| Token prefix | `--aef-*` | `tokens.css` |
| Heading font | `Geist Variable` (variable weight) | `tokens.css:51` |
| Body font | `Geist Mono` (monospace!) | `tokens.css:52` |
| Base font size | 13px | `design-system-page.css:17` |
| Background | `#000000` | `tokens.css:4` |
| Surface layers | `#080808`, `#101010`, `#141414`, `#1a1a1a` | `tokens.css:5-8` |
| Border | `#212121` | `tokens.css:9` |
| Text primary | `#ffffff` | `tokens.css:12` |
| Text secondary | `#656565` | `tokens.css:13` |
| Status live | `#4ade80` (green!) | `tokens.css:21` |
| Status warning | `#fb923c` (orange!) | `tokens.css:23` |
| Radius scale | 10/12/14/20/24px (control/field/card/panel/sheet) | `tokens.css:58-63` |
| Spacing | 4px grid (4,8,12,16,20,24,32,40,48) | `tokens.css:70-78` |
| Sidebar width | 244px expanded, 56px collapsed | `tokens.css:47-48` |
| Motion | 60/120/220/380ms with spring curves | `tokens.css:81-93` |
| Icons | `lucide-react` | `DesignSystemPage.tsx:17-30` |
| Font package | `@fontsource-variable/geist` | `main.tsx:9` |

**IMPORTANT:** The design system uses **monospace body text** (Geist Mono), **colored status indicators** (green/orange, not just gray), and a specific component grammar (counter cards, container cards, table cards, badges, nav items with pill radius). All Phase 5 UI must follow this system exactly.

---

## Pre-Phase Setup

### T0.1 — Install Missing Dependencies
**File:** `frontend/package.json`

Add:
- `@fontsource-variable/geist` — Geist Variable font (used by design system)
- `lucide-react` — Icon library (used extensively in design system)
- `@pixi/particle-emitter@^5.0` — Particle effects for edge visualization
- `framer-motion@^11` — UI animations
- `clsx@^2` — Conditional classnames

Verify clean build after install.

### T0.2 — Create Directory Structure
Create under `frontend/src/`:
- `engine/textures/` — generateTextures.ts, nodeAtlas.ts
- `engine/workers/` — (exists)
- `styles/` — tokens.css, globals.css, typography.css, utilities.css
- `components/layout/` — AppShell, Sidebar, Header, StatusBar
- `components/topology/` — TopologyCanvas, NodeDetailPanel, EdgeDetailPanel, TopologyControls, SearchOverlay, ClusterBreadcrumb
- `components/metrics/` — MetricChart, MetricCards, Sparkline, TimeRangeSelector
- `components/timeline/` — TimelineScrubber, SpeedControls, DiffViewer, SnapshotList
- `components/alerts/` — AlertList, AlertCard, AlertPanel
- `components/common/` — ErrorBoundary

### T0.3 — Test Fixtures & Harness
Create `src/__tests__/fixtures/` with sample topology (100 nodes, 200 edges), metric series, alerts data.
Create `src/__tests__/helpers/renderWithProviders.tsx` for component tests.

---

## Layer 17: GPU Rendering Engine (~5,000 LOC)

### T17.1 — Texture Generation (NEW, ~200 LOC)
**File:** `src/engine/textures/generateTextures.ts`
- Generate PIXI textures for each (type, status) combination at runtime
- Node types: host (circle), container (square), service (hexagon), process (diamond)
- Status colors: use design system status tokens (green=live, orange=warning, white=normal, gray=unknown)
- Pre-generate all combinations into a Map<string, PIXI.Texture>
- Agent: `frontend-engineer`

### T17.2 — Texture Atlas Manager (NEW, ~200 LOC)
**File:** `src/engine/textures/nodeAtlas.ts`
- Manage texture atlas for instanced rendering
- `getTexture(type, status)` → PIXI.Texture
- Populate from generateTextures at init
- Agent: `frontend-engineer`

### T17.3 — Instanced Node Renderer (NEW, ~500 LOC)
**File:** `src/engine/instancing.ts`
- Replace individual Graphics with `PIXI.ParticleContainer`
- `InstancedNodeRenderer` class with sprite pool
- `SpatialIndex` — grid-based spatial hash for O(1) hit testing
- `update(nodes)` — batch position/texture updates
- `getNodeAtPoint(x, y)` — hit testing
- **Target: 15K nodes at ≥55fps**
- Agent: `frontend-engineer`

### T17.4 — Viewport Controller (NEW, ~300 LOC)
**File:** `src/engine/viewport.ts`
- `ViewportController` — zoom/pan/fit with smooth animations
- Wheel zoom, drag pan, pinch zoom
- `screenToWorld()` / `worldToScreen()` coordinate transforms
- Zoom limits: 0.1x to 5.0x
- Agent: `frontend-engineer`

### T17.5 — Hierarchical Clustering (NEW, ~400 LOC)
**File:** `src/engine/clustering.ts`
- `HierarchicalLayout` — group nodes by type, arrange clusters in grid
- d3-force within each cluster
- `drillDown(clusterId)` / `drillUp()` navigation
- `Cluster` interface with bounding box, label, aggregate health
- Agent: `frontend-engineer`

### T17.6 — Edge Particle System (NEW, ~600 LOC)
**File:** `src/engine/particles.ts`
- `ParticleSystem` — ParticleContainer for edge data flow
- Particles spawn at source, travel along edge, despawn at target
- Speed = latency, color = throughput, density = volume
- **Target: 10K particles at ≥30fps**
- Agent: `frontend-engineer`

### T17.7 — Visual Effects (NEW, ~400 LOC)
**File:** `src/engine/effects.ts`
- `HealthGlow` — alpha pulse for degraded/unhealthy nodes
- `EdgeAnimation` — line width/alpha based on throughput
- `AlertPulse` — expanding ring on alerting nodes
- All monochromatic with status color accents from design system
- Agent: `frontend-engineer`

### T17.8 — REWRITE pixiApp.ts (~300 LOC)
**File:** `src/engine/pixiApp.ts`
- Replace individual Graphics with InstancedNodeRenderer
- Integrate ViewportController, ParticleSystem, EffectManager
- Container hierarchy: viewport > [edges, particles, nodes, effects, labels]
- FPS counter, WebGL context loss handling
- Agent: `frontend-engineer`

### T17.9 — REWRITE renderer.ts (~400 LOC)
**File:** `src/engine/renderer.ts`
- Bridge Zustand stores → PixiTopologyApp
- Integrate HierarchicalLayout for cluster-aware rendering
- Wire viewport controls to store state
- Fix metadata loss in current applyPositions()
- Agent: `frontend-engineer`

### T17.10 — ENHANCE topologyLayout.worker.ts (+200 LOC)
**File:** `src/engine/workers/topologyLayout.worker.ts`
- Add clustered layout message type
- Replace O(n²) repulsion with Barnes-Hut for large graphs
- Incremental layout for changed nodes only
- Agent: `frontend-engineer`

### T17.11 — ENHANCE dataParser.worker.ts (+100 LOC)
**File:** `src/engine/workers/dataParser.worker.ts`
- Implement protobuf decoding
- Add batch parsing for metric batches
- Add compression detection
- Agent: `frontend-engineer`

---

## Layer 18: Real-Time Data Pipeline (+700 LOC)

### T18.1 — ENHANCE websocket.ts (+300 LOC)
**File:** `src/api/websocket.ts`
- Message deduplication (sequence number tracking)
- Batch processing (accumulate 16ms, flush on rAF)
- Backpressure detection (monitor send buffer)
- Priority channels: topology > alerts > metrics
- Agent: `frontend-engineer`

### T18.2 — ENHANCE sse.ts (+200 LOC)
**File:** `src/api/sse.ts`
- Speed control (adjustable replay speed)
- Pause/resume (close/reconnect with offset)
- Seek (disconnect, reconnect at new position)
- Buffer management (ring buffer for recent events)
- Agent: `frontend-engineer`

### T18.3 — ENHANCE rest.ts (+200 LOC)
**File:** `src/api/rest.ts`
- Timeline snapshot endpoints
- Cluster topology endpoints
- Request cancellation (AbortController)
- Response caching (LRU for topology, metric names)
- Agent: `frontend-engineer`

---

## Layer 19: State Management (+850 LOC)

### T19.1 — ENHANCE topologyStore.ts (+200 LOC)
**File:** `src/stores/topologyStore.ts`
- Viewport state: zoom, panX, panY, selectedClusterId
- Cluster navigation: clusterPath (breadcrumb), expandedClusters
- Layout state: layoutMode, layoutRunning
- Actions: zoomToNode, enterCluster, exitCluster
- Agent: `frontend-engineer`

### T19.2 — ENHANCE metricsStore.ts (+200 LOC)
**File:** `src/stores/metricsStore.ts`
- Streaming mode: isStreaming, streamBuffer
- Downsample: LTTB algorithm for display
- Metric comparison: compareMode, baselineRange
- Increase point cap from 1000 to 10000 with downsampling
- Agent: `frontend-engineer`

### T19.3 — ENHANCE alertsStore.ts (+150 LOC)
**File:** `src/stores/alertsStore.ts`
- Alert grouping by severity/rule/node
- acknowledgeAlert(id) with API call
- silenceAlert(id, duration)
- Alert history tracking
- Agent: `frontend-engineer`

### T19.4 — ENHANCE timelineStore.ts (+200 LOC)
**File:** `src/stores/timelineStore.ts`
- Snapshot management: loadSnapshot, compareSnapshots
- Diff state: currentDiff, diffMode
- Bookmarks: addBookmark, removeBookmark
- Wire speed control to SSE client
- Agent: `frontend-engineer`

### T19.5 — ENHANCE settingsStore.ts (+100 LOC)
**File:** `src/stores/settingsStore.ts`
- Theme preferences (from design system tokens)
- Layout preferences: nodeSize, edgeStyle, labelVisibility
- localStorage persistence
- Agent: `frontend-engineer`

---

## Layer 20: UI Components (~2,250 LOC)

### T20.0 — Style Integration (~280 LOC)
**Files:**
- `src/styles/tokens.css` — Import and re-export design system tokens (`--aef-*`)
- `src/styles/globals.css` — Reset, body styles, scrollbar utilities
- `src/styles/typography.css` — @fontsource-variable/geist import, font-face declarations
- `src/styles/utilities.css` — Common utility classes

**MUST use the design system's `--aef-*` tokens exclusively. No custom color values.**

### T20.1 — Application Shell (~340 LOC)
**Files:**
- `src/components/layout/AppShell.tsx` — Full-height flex with sidebar + content
- `src/components/layout/Sidebar.tsx` — Nav items with pill radius, active state (white bg), collapse toggle (244px/56px)
- `src/components/layout/Header.tsx` — Brand, breadcrumb, search, connection indicator
- `src/components/layout/StatusBar.tsx` — Alert count, connection state, FPS, last update

**Must follow design system sidebar/nav patterns exactly (see `design-system-page.css:504-600`).**

### T20.2 — Topology Components (~700 LOC)
**Files:**
- `src/components/topology/TopologyCanvas.tsx` — Mount PixiJS, wire store
- `src/components/topology/NodeDetailPanel.tsx` — Slide-out panel (container card pattern)
- `src/components/topology/EdgeDetailPanel.tsx` — Edge details
- `src/components/topology/TopologyControls.tsx` — Zoom/fit/layout toggles
- `src/components/topology/SearchOverlay.tsx` — Fuzzy search with Ctrl+K
- `src/components/topology/ClusterBreadcrumb.tsx` — Cluster drill-down path

### T20.3 — Metrics Components (~440 LOC)
**Files:**
- `src/components/metrics/MetricChart.tsx` — Recharts time-series (already installed)
- `src/components/metrics/MetricCards.tsx` — Summary cards (counter card pattern from design system)
- `src/components/metrics/Sparkline.tsx` — Inline mini charts
- `src/components/metrics/TimeRangeSelector.tsx` — Time range picker

### T20.4 — Timeline Components (~410 LOC)
**Files:**
- `src/components/timeline/TimelineScrubber.tsx` — Horizontal scrubber with markers
- `src/components/timeline/SpeedControls.tsx` — Speed selector (button group pattern)
- `src/components/timeline/DiffViewer.tsx` — Side-by-side snapshot comparison
- `src/components/timeline/SnapshotList.tsx` — Bookmarkable snapshot list

### T20.5 — Alert Components (~350 LOC)
**Files:**
- `src/components/alerts/AlertList.tsx` — Alert list with filtering
- `src/components/alerts/AlertCard.tsx` — Individual alert (badge pattern from design system)
- `src/components/alerts/AlertPanel.tsx` — Slide-out detail panel

### T20.6 — Hooks (~200 LOC)
**Files:**
- `src/hooks/useViewport.ts` — Expose zoom/pan/fit from ViewportController
- `src/hooks/useKeyboard.ts` — Keyboard shortcuts (Ctrl+K search, Escape close, +/- zoom, Space play/pause)
- Enhance `useTopology.ts` (+30 LOC) — cluster navigation
- Enhance `useMetrics.ts` (+20 LOC) — streaming toggle
- Enhance `useAlerts.ts` (+20 LOC) — acknowledge/silence
- Enhance `useTimeline.ts` (+30 LOC) — speed control, bookmarks

### T20.7 — App.tsx REWRITE (~80 LOC)
**File:** `src/App.tsx`
- Wire AppShell as root layout
- Add ErrorBoundary component
- Add Suspense boundaries for lazy-loaded views
- Nested routes under AppShell
- Import design system font (`@fontsource-variable/geist`)
- Import new style files

---

## Verification Gates

| Gate | Criteria | Method | After |
|---|---|---|---|
| V1 | 15K nodes at ≥55fps | Automated benchmark | T17.8 |
| V2 | Non-overlapping clusters, drill-down works | Automated + manual | T17.10 |
| V3 | 10K particles at ≥30fps | Automated benchmark | T17.7 |
| V4 | WebSocket dedup, batch, reconnect | Automated tests | T18.1 |
| V5 | Timeline play/pause/stop/seek/speed | Automated tests | T20.4 |
| V6 | Design system compliance | Manual visual inspection | T20.0+T20.1 |

After all gates pass: run `/verify-frontend`, `/code-review`, `/test-integration`.

---

## Execution Order

```
Wave 1 (parallel): T0.1, T0.2, T0.3
Wave 2 (parallel): T17.1, T17.4, T17.11, T18.1, T18.2, T18.3
Wave 3: T17.2 (after T17.1)
Wave 4 (parallel): T17.3, T17.5, T17.6 (after T17.2+T17.4)
Wave 5: T17.7 (after T17.6)
Wave 6 (parallel): T17.8, T17.10 (after T17.3+T17.5+T17.7)
Wave 7: T17.9 (after T17.8)
Wave 8 (parallel): T19.1-T19.5 (after T17.9+T18.x)
Wave 9: T20.0 (style integration — can start after T0.1)
Wave 10: T20.1 (AppShell — after T20.0)
Wave 11 (parallel): T20.2-T20.6 (after T19.x+T20.1)
Wave 12: T20.7 (App.tsx rewrite — after T20.1)
Wave 13: Verification gates V1-V6
Wave 14: /verify-frontend, /code-review, /test-integration
```

**Agent mapping:** All tasks → `frontend-engineer` (single agent for consistency).
**Critical path:** T0.1 → T17.1 → T17.2 → T17.3 → T17.8 → T17.9 → T19.1 → T20.2
