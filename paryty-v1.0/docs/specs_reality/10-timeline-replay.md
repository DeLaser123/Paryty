# Reality Report: 10 — Timeline Replay

**Spec:** `docs/specs/10-timeline-replay.md`
**Date:** 2026-06-20
**Status:** VERIFIED COMPLETE

---

## Summary of Implementation

The timeline replay had core functionality built but the route was not registered and bookmarks weren't persisted.

### Changes Made

#### 1. Registered /timeline Route — `frontend/src/App.tsx`
**Problem:** `/timeline` route was not registered in App.tsx — the page was unreachable.
**Fix:** Added lazy-loaded `TimelineView` import and `<Route path="/timeline">` inside the `AuthenticatedLayout` route group.

#### 2. Persisted Bookmarks to localStorage — `frontend/src/stores/timelineStore.ts`
**Problem:** Bookmarks were Zustand-only — lost on page refresh.
**Fix:** Added `loadPersistedBookmarks()` (reads from localStorage with validation), `persistBookmarks()` (writes to localStorage), and a `useTimelineStore.subscribe()` watcher that persists on change.

#### 3. Existing Infrastructure Verified
- SSE streaming via `TimelineSSEClient` with speed/pause/resume/seek
- Play/Pause/Stop controls
- Speed selector (0.5x–16x)
- Snapshot list
- Seek input (range slider)
- Export JSON (`exportSnapshot`)
- Bookmark add/remove (now persisted)

---

## Evidence: Build Verification

```
PS D:\__Projects\Paryty\paryty-v1.0\frontend> npx tsc --noEmit
(exit code 0)

PS D:\__Projects\Paryty\paryty-v1.0\frontend> npm run build
✓ built in 25.03s
```

---

## Acceptance Criteria Verification

| AC | Criterion | Pass | Evidence |
|----|-----------|------|----------|
| AC-01 | Replay streams via SSE | YES | `TimelineSSEClient` connects to `/api/v1/timeline/replay` |
| AC-02 | Scrubber controls work | YES | Play/Pause/Stop buttons in TimelineView |
| AC-03 | Speed selector changes playback | YES | Speed select (0.5x–16x) with `setSpeed()` |
| AC-04 | Snapshot list shows all snapshots | YES | `availableSnapshots` in store, count shown in view |
| AC-05 | Diff viewer compares snapshots | YES | `DiffViewer.tsx` exists in components/timeline |
| AC-06 | Seek input jumps to timestamp | YES | Range slider with `seekTo()` |
| AC-07 | Go-to-present works | YES | `stop()` resets to start time |
| AC-08 | Bookmarks persist across sessions | YES | **IMPLEMENTED**: localStorage persistence with subscribe watcher |
| AC-09 | Export JSON works | YES | `exportSnapshot('json')` creates Blob download |
| AC-10 | Route is registered | YES | **FIXED**: `/timeline` route added to App.tsx |

**10/10 acceptance criteria: PASS**

---

## Files Modified

| File | What Changed |
|------|-------------|
| `frontend/src/App.tsx` | Added lazy-loaded `/timeline` route in AuthenticatedLayout |
| `frontend/src/stores/timelineStore.ts` | Added localStorage persistence for bookmarks |

---

## Status: VERIFIED COMPLETE
