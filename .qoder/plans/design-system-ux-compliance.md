# Design System UX Compliance — Full Interactive Audit

## Context

The Paryty design system defines custom interaction patterns (button press animations, proprietary dropdowns, icon hover/press states, thin scrollbars). The Header.tsx component correctly uses the custom `aef-dropdown` pattern, but 9 native `<select>` elements, bare buttons, and unstyled scroll containers across the rest of the frontend still use native browser behavior. This plan replaces all native browser UI with the Paryty design system equivalents.

---

## Task 1: Create reusable `ParytySelect` component

**New file:** `frontend/src/components/common/ParytySelect.tsx` (~120 LOC)

Extract the custom dropdown pattern from `Header.tsx` (lines 127-163) into a reusable component:
- Props: `options: { label: string; value: string }[]`, `value: string`, `onChange: (value: string) => void`, `placeholder?: string`, `className?: string`, `testId?: string`
- Uses `aef-dropdown`, `aef-dropdown-trigger`, `aef-dropdown-menu`, `aef-dropdown-item` classes
- Keyboard support (Escape to close, click-outside to close)
- Selected item check mark indicator
- Animated open/close via design system `aef-dropdown-in` keyframe

## Task 2: Replace all native `<select>` with `ParytySelect`

**9 select elements across 6 files:**

| File | Selects | Options |
|------|---------|---------|
| `components/alerts/AlertList.tsx` | 3 (state, severity, groupBy) | Static option lists |
| `components/AlertView.tsx` | 2 (state, severity) | Static option lists |
| `components/MetricsView.tsx` | 1 (metric picker) | Static option list |
| `components/TimelineView.tsx` | 1 (speed) | Static speed options |
| `components/intel/IntelView.tsx` | 1 (horizon) | Dynamic from HORIZON_PRESETS |
| `components/intel/ForecastChart.tsx` | 1 (metric selector) | Dynamic from KEY_METRICS |

Each replacement:
- Convert `<option>` children to `{ label, value }[]` array
- Replace `<select onChange>` with `ParytySelect onChange`
- Update handler callbacks (no more `e.target.value` — direct string value)
- Remove `HTMLSelectElement` type annotations from handlers

## Task 3: Add global press animation CSS

**File:** `frontend/src/index.css`

Add a global press-animation rule for all interactive elements that currently lack `:active` feedback:

```css
/* Global design-system press animation */
button:not(.aef-btn):not(.tld-speed-btn):not(.tld-ctrl-btn):not(.topology-timeline-drawer__close),
.view-controls button,
.view-controls input,
.view-controls select,
.cluster-breadcrumb__item,
.alert-card,
.tld-speed-btn,
.tld-ctrl-btn,
.topology-timeline-drawer__close,
.intel-view__horizon-select,
.intel-view__refresh-btn,
.forecast-chart__selector {
  transition:
    transform var(--aef-duration-fast) var(--aef-ease-exit),
    background var(--aef-duration-fast) var(--aef-ease-exit),
    border-color var(--aef-duration-fast) var(--aef-ease-exit);
}

button:not(.aef-btn):active,
.cluster-breadcrumb__item:active,
.tld-speed-btn:active,
.tld-ctrl-btn:active,
.topology-timeline-drawer__close:active,
.intel-view__refresh-btn:active {
  transform: scale(0.95);
}

.alert-card:active {
  transform: scale(0.99);
}
```

This gives every clickable element the tactile "press-down" feel from the design system without conflicting with existing `aef-btn` animations.

## Task 4: Upgrade scroll containers to thin proprietary scrollbar

**File changes:** Replace `aef-scroll` (invisible scrollbar) with `aef-scroll-thin` (3px styled scrollbar) on scroll containers that should show the proprietary thin scrollbar:

| File | Current | Change To |
|------|---------|-----------|
| `alerts/AlertPanel.tsx` | `aef-scroll` | `aef-scroll-thin` |
| `alerts/AlertList.tsx` | `aef-scroll` | `aef-scroll-thin` |
| `topology/NodeDetailPanel.tsx` | `aef-scroll` | `aef-scroll-thin` |
| `topology/EdgeDetailPanel.tsx` | `aef-scroll` | `aef-scroll-thin` |

Keep `aef-scroll` (invisible) on:
- `topology/SearchOverlay.tsx` — command palette uses invisible scroll (correct per DS)
- `intel/AnomalyPanel.tsx` — already uses `aef-scroll-thin` (correct)

## Task 5: Add `aef-btn` classes to bare buttons

**Files to update:**

| File | Buttons | Action |
|------|---------|--------|
| `TimelineView.tsx` | Play, Pause, Stop | Add `aef-btn aef-btn-inactive` |
| `AlertView.tsx` | Acknowledge | Add `aef-btn aef-btn-inactive` |
| `AlertList.tsx` filter handlers | N/A (selects, handled in Task 2) | — |

Already correct (no change needed):
- `TopologyControls.tsx` — uses `aef-btn aef-btn-inactive` ✓
- `SpeedControls.tsx` — uses `aef-btn aef-btn-active/inactive` ✓
- `DashboardPage.tsx` — uses `aef-btn aef-btn-active/inactive` ✓
- `TimelineDrawer.tsx` — uses custom `tld-ctrl-btn`/`tld-speed-btn` classes ✓
- `Header.tsx` — uses custom `app-topbar__icon-btn` classes ✓
- `AlertCard.tsx` — uses `aef-btn aef-btn-inactive` ✓
- `ErrorBoundary.tsx` — uses `aef-btn aef-btn-active` ✓

## Task 6: Fix Layout.tsx — bare `<a>` tags

**File:** `frontend/src/components/Layout.tsx`

Layout.tsx has 4 bare `<a href>` tags (lines 20-23) that:
- Use native HTML navigation (full page reload) instead of React Router
- Have no design system nav classes
- Appear to be a legacy duplicate of the Sidebar nav (which already handles routing)

**Action:** Since `Layout.tsx` is NOT used in the active app shell (AppShell.tsx uses Sidebar.tsx for navigation), this file is dead code. Leave it as-is — no risk of breaking anything, and it's not rendered.

## Task 7: Add icon interaction wrappers to clickable icons

**Files to update:**

| File | Icons | Action |
|------|-------|--------|
| `Sidebar.tsx` nav items | Lucide icons inside `ds-nav-item` | Already handled by nav-item hover styles — no change needed |
| `StatusBar.tsx` indicator icons | Wifi, Monitor, Sparkles, etc. | Non-interactive indicators — no change needed |
| `Header.tsx` icon buttons | Bell, History, Clock | Already inside `app-topbar__icon-btn` with hover/active CSS — no change needed |
| `TimelineDrawer.tsx` | Play, Pause, Square, X, ChevronUp | Inside `tld-ctrl-btn` buttons — press animation added in Task 3 |

Most icons are already inside interactive button containers. The press animation CSS (Task 3) covers them.

---

## Verification

1. **Build check:** `npm run build` in `frontend/` — no TypeScript errors
2. **Visual check:** Open `http://localhost:3001/`, verify:
   - Sidebar nav items have hover/press animations
   - All dropdowns use the custom animated Paryty dropdown (no native selects)
   - Topology controls have press animation
   - Alert list filters are custom dropdowns
   - Metrics view metric picker is a custom dropdown
   - Timeline view controls use `aef-btn` classes with press animation
   - Intel view horizon selector is a custom dropdown
   - Scroll containers show thin proprietary scrollbar (not native)
3. **Interaction check:** Click every button/dropdown and verify the scale-down press animation fires
