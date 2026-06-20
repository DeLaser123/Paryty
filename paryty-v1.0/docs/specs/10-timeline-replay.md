# Step 10: Timeline Replay — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### What Exists

| Component | File | Status |
|-----------|------|--------|
| TimelineScrubber | `frontend/src/components/timeline/TimelineScrubber.tsx` | Playback scrubber |
| SpeedControls | `frontend/src/components/timeline/SpeedControls.tsx` | Speed selector (0.25x-16x) |
| DiffViewer | `frontend/src/components/timeline/DiffViewer.tsx` | Snapshot comparison |
| SeekInput | `frontend/src/components/timeline/SeekInput.tsx` | Time-based seek |
| SnapshotList | `frontend/src/components/timeline/SnapshotList.tsx` | Snapshot browser |
| TimelineDrawerBar | `frontend/src/components/layout/TimelineDrawerBar.tsx` | Drawer bar in topology |
| timelineStore | `frontend/src/stores/timelineStore.ts` | Play/pause/stop/seek, diff, bookmarks |
| SSE Client | `frontend/src/api/sse.ts` | EventSource with speed control |
| Backend replay engine | `cluster/internal/timeline/replay_engine.go` | Go-native replay with speed control |
| Backend snapshot mgr | `cluster/internal/timeline/snapshot_manager.go` | Snapshot management |
| Backend export | `cluster/internal/timeline/export_manager.go` | Report export |
| Proto | `proto/paryty/v1/timeline.proto` | Full gRPC service |

### Critical Gaps

1. **PDF export** uses HTML print-to-PDF, no proper PDF library
2. **Timeline page UX** is basic, needs dedicated page with full topology integration
3. **Bookmark persistence** is Zustand-only (no localStorage)

### 1.3 Existing Implementation Details

- **TimelineSSEClient** (`sse.ts:154-301`): Full class with speed control, pause/resume, seek, ring buffer, reset
- **DiffCalculator** (`diff_calculator.go`): Calculates diffs between time ranges
- **ExportManager** (`export_manager.go`): JSON/HTML/diff export capabilities
- **SeekInput** has relative time parsing (e.g., "-5m", "+1h") at lines 56-88
- **TimelineDrawerBar** has 300+ lines with drag-to-scrub, speed drop-up menu, RAF-locked pointer events
- **Snapshot tagging:** TagSnapshot RPC (`timeline.proto:32`)
- **ReplayFrame structure:** topology_data, metrics, alerts, progress (`timeline.proto:170-181`)
- **MetricDelta type:** from/to/change/change_pct (`timeline.proto:148-153`)
- **AlertSnapshot type:** alert snapshots in timeline (`timeline.proto:108-113`)

---

## 2. Target State

**User Journey:** Open timeline → See snapshot markers → Scrub to past time → Play replay → See system state change → Pause → Compare snapshots → Export report

---

## 3. UI Specification

### 3.1 Timeline Scrubber Bar

```tsx
<div className="timeline-scrubber">
  <div className="timeline-track">
    <div className="timeline-progress" style={{ width: `${progress}%` }} />
    {snapshots.map(s => (
      <div key={s.id} className="timeline-marker" style={{ left: `${s.position}%` }} />
    ))}
    <div className="timeline-pointer" style={{ left: `${progress}%` }} />
  </div>
  <div className="timeline-controls">
    <button onClick={togglePlay}>{isPlaying ? <Pause /> : <Play />}</button>
    <button onClick={stop}><Square /></button>
    <SpeedControls value={speed} onChange={setSpeed} />
    <span className="timeline-time">{formatTime(currentTime)}</span>
    <button onClick={goToPresent}>Go to Present</button>
  </div>
</div>
```

**Tokens:** `--aef-progress-active`, `--aef-progress-track`, `--aef-btn-*`

### 3.2 Playback Controls

- Play/Pause toggle
- Stop (reset to start)
- Speed selector: 0.25x, 0.5x, 1x, 2x, 4x, 8x, 16x
- Time display: current time, elapsed, total range
- Go-to-present button

### 3.3 Snapshot List Panel

```tsx
<div className="snapshot-list">
  <input placeholder="Search snapshots..." onChange={handleSearch} />
  {filteredSnapshots.map(snapshot => (
    <div key={snapshot.id} className="snapshot-row" onClick={() => seekTo(snapshot.timestamp)}>
      <span className="snapshot-time">{formatTime(snapshot.timestamp)}</span>
      <span className="snapshot-event-count">{snapshot.eventCount} events</span>
      <button onClick={() => bookmark(snapshot)}>Bookmark</button>
    </div>
  ))}
</div>
```

### 3.4 Diff Viewer

Side-by-side or inline comparison of two snapshots:
- Left: Snapshot A (topology state at time A)
- Right: Snapshot B (topology state at time B)
- Highlighted differences: added nodes (green), removed nodes (red), changed metrics (yellow)

### 3.5 Seek Input

```tsx
<div className="seek-input">
  <input type="datetime-local" onChange={handleSeek} />
  <span>or</span>
  <input placeholder="-30m, -1h, -2d" onChange={handleRelativeSeek} />
</div>
```

### 3.6 Bookmark System

- Save interesting moments with name
- List of bookmarks with click-to-seek
- Zustand-only storage (no localStorage)

### 3.7 Export Options

- **JSON:** Full snapshot data
- **HTML:** Formatted report
- **PDF:** (Future — requires jsPDF or similar)

---

## 4. API Contract

### GET /api/v1/timeline/snapshots

**Query:** `?startTime=...&endTime=...&limit=50`

**Response:** `[{ "id": "uuid", "timestamp": "...", "eventCount": 42, "nodeCount": 12, "edgeCount": 18 }]`

### GET /api/v1/timeline/snapshots/:id

**Response:** Full snapshot with nodes, edges, metrics, events.

### GET /api/v1/timeline/snapshots/diff

**Query:** `?fromId=...&toId=...`

**Response:** Diff with added/removed/changed nodes and edges.

### SSE /api/v1/timeline/replay

**Query:** `?start=...&end=...&speed=1`

**Events:** `frame` events with topology state at each timestamp.

---

## 5. SSE Replay Protocol

1. Client connects with `?start=...&end=...&speed=1`
2. Server streams `frame` events at configured speed
3. Speed changes → reconnect with new speed
4. Pause → close connection, save position
5. Resume → reconnect at saved position
6. Seek → reconnect at new timestamp

---

## 6. Implementation Tasks

| Task | File | Description |
|------|------|-------------|
| T-01 | rest.go | Fix snapshot list to return all snapshots with counts |
| T-02 | rest.go | Fix snapshot data completeness |
| T-03 | TimelineDrawerBar.tsx | Enhance with full scrubber + controls |
| T-04 | New: TimelinePage.tsx | Dedicated timeline page |
| T-05 | timelineStore.ts | Add bookmark persistence |
| T-06 | DiffViewer.tsx | Enhance with highlighted differences |
| T-07 | SeekInput.tsx | Add relative time input |
| T-08 | export_manager.go | Implement proper PDF export |
| T-09 | timelineStore.ts | Add export actions |
| T-10 | TopologyCanvas.tsx | Integrate replay data with topology |

---

## 7. Acceptance Criteria

| AC | Criterion |
|----|-----------|
| AC-01 | Replay streams via SSE |
| AC-02 | Scrubber controls work (play/pause/stop) |
| AC-03 | Speed selector changes playback speed |
| AC-04 | Snapshot list shows all snapshots |
| AC-05 | Diff viewer compares two snapshots |
| AC-06 | Seek input jumps to timestamp |
| AC-07 | Go-to-present works |
| AC-08 | Bookmarks persist across sessions |
| AC-09 | Export JSON works |
| AC-10 | Topology updates during replay |
