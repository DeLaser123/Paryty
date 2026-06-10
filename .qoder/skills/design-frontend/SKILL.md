---
name: design-frontend
description: Design and build Paryty frontend UI/UX using the Paryty Design System. Compose DS primitives creatively, eliminate all native browser/OS primitives, interpret visual references for design intent. Use for any UI change — new component, redesign, layout, or visual polish.
---

# Design Frontend — Paryty Design System Enforcement

## Preamble: The Paryty Design Constitution

You are designing for **Paryty** — a network observability platform with a dark, monospace-first, high-density aesthetic. The design system is **not a suggestion**. It is the law.

Three inviolable principles govern every design decision:

1. **Compose, don't invent.** Every UI element is a composition of DS primitives. Before you write a single new CSS class or component, prove that no DS primitive composition can solve the problem.
2. **No native primitives.** Every browser/OS-native widget — scrollbar, context menu, select dropdown, checkbox, radio, tooltip, date picker, focus ring — must be replaced with a Paryty-branded equivalent. The app must look identical on Windows, macOS, Linux, Chrome, Firefox, Safari.
3. **Design intent, not pixel replication.** When given a visual reference (screenshot, Figma, sketch), extract the *intent* — information hierarchy, spatial relationships, interaction model — and re-express it in Paryty DS. Never pixel-copy.

---

## Phase 0: Load the Design System Into Context

Before any design work, read these files to load the complete DS into your working memory:

```
paryty-v1.0/frontend/src/paryty_design_system/tokens.css
paryty-v1.0/frontend/src/styles/globals.css
paryty-v1.0/frontend/src/styles/typography.css
paryty-v1.0/frontend/src/styles/utilities.css
paryty-v1.0/frontend/src/paryty_design_system/design-system-page.css
paryty-v1.0/frontend/src/index.css
```

Then scan the existing component catalog:

```
paryty-v1.0/frontend/src/components/
  ├── common/       ← DS primitives live here (ParytySelect, Tooltip, Toast, etc.)
  ├── layout/       ← App shell, sidebar, header, context menu, status bar
  ├── topology/     ← Topology canvas, controls, panels
  ├── metrics/      ← Metric cards, charts, sparklines
  ├── timeline/     ← Timeline scrubber, diff viewer, speed controls
  ├── intel/        ← Intelligence panels, forecast charts, anomaly cards
  ├── dashboard/    ← Dashboard page layout
  ├── alerts/       ← Alert cards, list, panel
  └── auth/         ← Auth provider, protected routes
```

---

## Phase 1: The Compose-Extend-Create Decision Tree

When asked to build or modify a UI component, follow this strict decision tree:

### Step 1: Compose from existing DS primitives

The Paryty DS provides these **compositional primitives** (all CSS classes prefixed `aef-`):

| Primitive | CSS Class | What It Does | Compose With |
|---|---|---|---|
| **Container Card** | `aef-container-card` | Card with header (icon + title + meta-pill) + scrollable body | `__header`, `__icon`, `__title`, `__body` |
| **Counter Card** | `aef-counter` | Icon + label + value, 4 variants (neutral, active, variant-a, variant-b) | `__icon`, `__body`, `__label`, `__value` |
| **Table Card** | `aef-table-card` | Table in a card with header | `__header`, standard `<table>` with `.aef-table` |
| **Button** | `aef-btn` | Two variants: `aef-btn-active` (filled white), `aef-btn-inactive` (outlined) | — |
| **Progress** | `aef-progress-track` / `aef-progress-fill` | Horizontal progress bar | — |
| **Badge** | `aef-badge` | Small status chip — combine with `.badge-valid`, `.badge-warning`, `.badge-pending` | — |
| **Meta Pill** | `aef-meta-pill` | Metadata label chip for card headers | — |
| **Indicator Dot** | `aef-dot` / `aef-dot-active` | 6px status dot | — |
| **Stat Module** | `aef-stat-module` | Label-value row in a bordered strip | `__label`, `__value` |
| **Viz Well** | `aef-viz-well` | Placeholder area for charts/visualizations | `__icon`, `__label` |

**Composition rule:** If your UI can be expressed as a combination of these primitives arranged with flex/grid and DS spacing tokens (`var(--aef-space-*)`), you MUST compose. Do not create new CSS classes.

**Example of valid composition:**
```
A "CPU Usage Panel" = aef-container-card
  ├── __header: icon (Cpu) + __title ("CPU Usage") + aef-meta-pill ("12 cores")
  └── __body:
      ├── aef-counter (neutral): Cpu icon + "Current" + "67%"
      ├── aef-progress-track → aef-progress-fill (width: 67%)
      └── aef-viz-well → sparkline canvas
```

### Step 2: Extend with new CSS *only if composition fails*

If no combination of existing primitives can express the required UI:

1. Use existing DS tokens for ALL values — colors (`var(--aef-*-*)`), spacing (`var(--aef-space-*)`), radius (`var(--aef-radius-*)`), motion (`var(--aef-duration-*`), `var(--aef-ease-*`)), fonts (`var(--aef-font-*)`)
2. Prefix new CSS classes with `aef-` to signal DS membership
3. Follow existing naming conventions: `aef-{component}__{element}--{modifier}`
4. Never use raw hex values — always reference a DS token
5. Add new CSS in `index.css` (app-level) or `design-system-page.css` (DS-level)

### Step 3: Create a new React component *only if it wraps new behavior*

A new React component is justified only when it encapsulates:
- State management (open/close, selected, focused, etc.)
- Event handling (click-outside, keyboard navigation, drag)
- Portal rendering (tooltips, modals, context menus)
- Animation orchestration

If it's purely presentational, it should be HTML + CSS classes (no new `.tsx` file).

---

## Phase 2: The Paryty Design Token System

### Color Palette

All colors reference `var(--aef-*)` tokens. NEVER use raw hex.

```
Backgrounds (darkest → lightest):
  --aef-bg:            #000000  ← App root background
  --aef-surface-low:   #080808  ← Card body, input backgrounds
  --aef-surface-mid:   #101010  ← Section backgrounds
  --aef-surface-card:  #141414  ← Card, dropdown, modal surfaces
  --aef-surface-hover: #1a1a1a  ← Hover state

Borders:
  --aef-border:        #212121  ← Default border
  --aef-border-strong: #444444  ← Emphasized border, tooltip border

Text:
  --aef-text-primary:  #ffffff  ← Headings, values, active text
  --aef-text-secondary:#656565  ← Labels, metadata, muted text
  --aef-text-inverse:  #000000  ← Text on white backgrounds

Interactive:
  --aef-selected-bg:   #ffffff  ← Active/selected background
  --aef-selected-text: #000000  ← Text on selected background
  --aef-selected-icon: #000000  ← Icons on selected background

Status:
  --aef-status-live:       #4ade80  ← Green: healthy, live, success
  --aef-status-live-soft:  #4ade8040
  --aef-status-warning:    #fb923c  ← Orange: warning
  --aef-status-warning-soft:#fb923c40

Transport:
  --aef-transport-ws:      #4ade80  ← WebSocket
  --aef-transport-rest:    #f59e0b  ← REST
  --aef-transport-sse:     #f97316  ← SSE
  --aef-transport-offline: #ef4444  ← Offline

Button:
  --aef-btn-active-bg:     #ffffff
  --aef-btn-active-text:   #000000
  --aef-btn-inactive-bg:   #080808
  --aef-btn-inactive-border: 1px solid #212121
  --aef-btn-inactive-text: #ffffff

Counter variants:
  --aef-counter-neutral-bg/border/text  ← Default
  --aef-counter-active-bg/text          ← White fill
  --aef-counter-variant-a: #4719FF      ← Purple accent
  --aef-counter-variant-b: #DC4714      ← Red accent
  --aef-counter-variant-text: #ffffff
```

### Spacing Scale

ALL gaps, padding, margin MUST use these tokens:

```
--aef-space-1:  4px    --aef-space-2:  8px    --aef-space-3:  12px
--aef-space-4:  16px   --aef-space-5:  20px   --aef-space-6:  24px
--aef-space-8:  32px   --aef-space-10: 40px   --aef-space-12: 48px
```

### Radius Scale — Mathematical

```
--aef-radius-control: 10px  ← Buttons, inputs, small controls
--aef-radius-field:   12px  ← Dropdown triggers
--aef-radius-card:    14px  ← Cards
--aef-radius-panel:   20px  ← Panels, modals
--aef-radius-sheet:   24px  ← Context menus, sheets
--aef-radius-full:    9999px ← Pills, badges, full-round elements
--aef-radius-xs:      6px   ← Legacy alias
```

**Nested radius rule:** When a card contains a child card, inner radius = `max(parent_radius - inset, radius-control)`.

### Motion Tokens

```
Durations:
  --aef-duration-instant:  60ms   ← Micro-interactions, button press
  --aef-duration-fast:     120ms  ← Hover transitions, dropdown open
  --aef-duration-standard: 220ms  ← Default transitions
  --aef-duration-slow:     380ms  ← Page transitions, complex animations

Easing:
  --aef-ease-emphasized: cubic-bezier(0.2, 0, 0, 1)    ← Entrances, emphasis
  --aef-ease-settle:     cubic-bezier(0.34, 1.56, 0.64, 1) ← Overshoot settle
  --aef-ease-exit:       cubic-bezier(0.4, 0, 1, 1)    ← Exits, fades
  --aef-ease-linear:     linear

Spring variants (alias for settle with varying overshoot):
  --aef-spring-subtle:   cubic-bezier(0.34, 1.20, 0.64, 1)
  --aef-spring-normal:   cubic-bezier(0.34, 1.56, 0.64, 1)
  --aef-spring-obvious:  cubic-bezier(0.25, 1.80, 0.64, 1)
```

**Animation keyframes available:** `aef-scale-in`, `aef-scale-settle`, `aef-fade-in`, `aef-panel-enter`, `aef-dropdown-in`, `aef-modal-enter`, `aef-warn-pulse`, `spin`.

### Typography

```
--aef-font-heading: 'Geist Variable', 'Geist', system-ui, sans-serif
--aef-font-body:    'Geist Mono', 'IBM Plex Mono', ui-monospace, 'Courier New', monospace

Default sizing:
  Headings: 10-12px, weight 500-600
  Body:     9-11px, weight 400
  Labels:   8px, weight 500
  Metadata: 8px, secondary color
```

---

## Phase 3: Native Primitive Elimination Checklist

**EVERY** native browser/OS widget must be replaced. This is non-negotiable. When building UI, verify each item:

### Already Eliminated ✅

| Native Primitive | Paryty Replacement | Implementation |
|---|---|---|
| Right-click context menu | `aef-context-menu` | `ContextMenuProvider` — intercepts `contextmenu` event, `e.preventDefault()`, renders fixed-position menu |
| `<select>` dropdown | `aef-dropdown` | `ParytySelect` — custom animated dropdown with click-outside, Escape, keyboard nav, check-mark |
| Browser tooltip (`title` attr) | `aef-tooltip` | `Tooltip` — `createPortal` to `document.body`, cursor-following, 400ms delay, edge-flip |
| Native scrollbar | `aef-scroll` / `aef-scroll-thin` | CSS `scrollbar-width: none` + `::-webkit-scrollbar { display: none }` for invisible; 3px thin track for visible |
| Native focus ring | Custom `:focus-visible` | White outline with offset, hidden on mouse focus |
| Text selection color | `::selection` | White background, black text |
| Alert/confirm dialogs | `aef-modal` | Modal overlay + panel with DS styling |
| Toast notifications | `.toast-container` / `.toast-item` | Custom fixed-position toast stack with slide-in animation |

### Still Need Building ❌

| Native Primitive | What Must Be Created | Priority |
|---|---|---|
| `<input type="checkbox">` | `ParytyCheckbox` component using `aef-` CSS + custom SVG check | HIGH |
| `<input type="radio">` | `ParytyRadio` component using `aef-` CSS + custom dot indicator | HIGH |
| `<input type="range">` | `ParytySlider` — custom range slider with aef-progress-track styling | MEDIUM |
| Toggle switch | `ParytyToggle` — pill-shaped on/off toggle (not native, but replaces checkbox pattern) | MEDIUM |
| `<input type="date">` / date picker | `ParytyDatePicker` — custom calendar dropdown | LOW |
| `<input type="number">` spin buttons | Hide native spinners via CSS, style consistently | LOW |
| `<details>/<summary>` | Accordion/collapse using `aef-container-card` + animated height | LOW |
| File input | Custom styled file upload trigger | LOW |
| Native form validation tooltips | Suppress with `novalidate`, use Paryty Toast for validation messages | LOW |

### CSS-Only Eliminations (no React component needed)

These are suppressed purely via CSS in `globals.css`:

```css
/* Already in globals.css — verify they're complete */
input[type="checkbox"],
input[type="radio"] {
  appearance: none;
  -webkit-appearance: none;
  /* Must be replaced by ParytyCheckbox / ParytyRadio */
}

/* Hide number input spinners */
input[type="number"]::-webkit-inner-spin-button,
input[type="number"]::-webkit-outer-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
input[type="number"] {
  -moz-appearance: textfield;
}
```

---

## Phase 4: The "Creative Palette" Mindset

The DS is not a cage — it's a palette. Constraints enable creativity by eliminating trivial decisions and focusing energy on composition, proportion, and motion.

### How to be creative within the DS:

1. **Contrast density.** Compose tight clusters of counters against open canvas space. The DS gives you `--aef-space-*`; the creativity is in choosing *which* spacing creates the most effective information hierarchy.

2. **Stagger entrance animations.** Use `aef-panel-enter` with incremental `animation-delay` (40-80ms per child) to create cascade effects. See `forecast-cards .aef-container-card:nth-child(N)` in globals.css.

3. **Hover reveals.** Use `aef-container-card:hover` to trigger child transformations — scale, opacity, color transitions. The DS provides tokens; you choreograph them.

4. **Counter card variants as semantic color.** `aef-counter-variant-a` (#4719FF purple) and `aef-counter-variant-b` (#DC4714 red) are not just colors — they're semantic signals. Use them to encode meaning (e.g., variant-a for ingress, variant-b for egress).

5. **Nested radii.** When nesting cards within cards, apply the mathematical radius scale: outer card at `--aef-radius-card` (14px), inner element at `--aef-radius-control` (10px). The hierarchy is expressed through geometry.

6. **Spring motion for delight.** Use `--aef-spring-obvious` for celebratory moments (error resolved, connection established). Use `--aef-spring-subtle` for everyday interactions. Use `--aef-ease-emphasized` for entrances that demand attention.

7. **The status bar is sacred.** The 28px status bar at the bottom is the only place exempt from transport-mode saturation effects (`filter: saturate(1) !important`). Use it for system-critical information only.

---

## Phase 5: Design Intent Extraction

When given a visual reference (screenshot, design mockup, Figma link):

### Step 1: Identify the information architecture
- What is the PRIMARY action or data the user needs?
- What is SECONDARY (supporting context)?
- What is TERTIARY (nice to have, can be behind hover/click)?

### Step 2: Map to DS primitives
- Primary → `aef-counter-active` (white fill, highest contrast)
- Secondary → `aef-counter-neutral` (bordered, medium contrast)
- Tertiary → `aef-stat-module` (compact row) or `aef-meta-pill`

### Step 3: Extract spatial relationships
- Is it a grid? → CSS Grid with `--aef-space-*` gaps
- Is it a list? → Flexbox column with `--aef-space-*` gaps
- Is it overlapping? → `position: absolute` within a relative container
- Is it layered (z-stack)? → `z-index` with `--aef-z-*` tokens

### Step 4: Identify the motion intent
- Does the reference show a sequence? → Staggered entrance animations
- Does it imply state change? → `aef-ease-emphasized` transition
- Does it suggest playfulness? → `aef-spring-normal` or `aef-spring-obvious`

### Step 5: Re-express in Paryty
- Replace ALL colors with `--aef-*` token equivalents
- Replace ALL fonts with Geist Variable (headings) or Geist Mono (body)
- Replace ALL border radii with the mathematical scale
- Replace ALL spacing with the `--aef-space-*` scale
- Add DS-standard hover/active/focus states

### What NOT to do:
- Do NOT copy hex values from the reference
- Do NOT match pixel font sizes — use the DS typographic scale
- Do NOT replicate layout pixel-perfectly — re-express the spatial logic
- Do NOT copy animations from the reference — use DS motion tokens

---

## Phase 6: Existing DS Component Reference

### ParytySelect (`components/common/ParytySelect.tsx`)
Custom dropdown replacing native `<select>`.  
**CSS:** `aef-dropdown`, `aef-dropdown-trigger`, `aef-dropdown-menu`, `aef-dropdown-item`, `aef-dropdown-item--selected`, `aef-dropdown-check`, `aef-dropdown-chevron`, `aef-dropdown-chevron--open`  
**Props:** `options: {label, value}[]`, `value: string`, `onChange: (value) => void`, `placeholder?: string`, `className?: string`, `testId?: string`  
**Features:** Click-outside close, Escape close, edge-flip via `useDropdownEdge`, tooltip on truncated labels

### Tooltip (`components/common/Tooltip.tsx`)
Portal-based tooltip following cursor.  
**CSS:** `aef-tooltip-anchor`, `aef-tooltip`  
**Props:** `label: string`, `children: ReactNode`  
**Features:** 400ms delay, cursor tracking, edge-flip (above/below), `createPortal` to `document.body`, arrow indicator

### ContextMenu (`components/layout/ContextMenu.tsx`)
Page-aware right-click context menu system.  
**CSS:** `aef-context-menu`, `aef-context-item`, `aef-context-item__label`, `aef-context-divider`, `aef-context-kbd`  
**Features:** `contextmenu` event interception with `e.preventDefault()`, viewport-edge detection, click-outside close, Escape close, route-aware menu items

### Toast (`components/common/Toast.tsx`)
Fixed-position toast notifications.  
**CSS:** `toast-container`, `toast-item`, `toast-item--success/warning/error/info`, `toast-item--expandable`, `toast-item--expanded`  
**Store:** `useToastStore` (Zustand) — `toasts[]`, `addToast()`, `removeToast()`  
**Usage:** `addToast({ type: 'success'|'warning'|'error'|'info', message: string, duration?: number })`

### UserMenu (`components/layout/UserMenu.tsx`)
User dropdown with profile, settings, sign-out.

### SearchOverlay (`components/topology/SearchOverlay.tsx`)
Command-palette-style search overlay for topology.

### ErrorBoundary (`components/common/ErrorBoundary.tsx`)
React error boundary with DS styling.

### AuthHint (`components/common/AuthHint.tsx`)
Floating contextual help card for auth flows.

### UpgradePrompt (`components/common/UpgradePrompt.tsx`)
Plan upgrade prompt card.

### StepIndicator (`components/common/StepIndicator.tsx`)
Multi-step progress indicator.

---

## Phase 7: Building New DS Components

When you must create a new DS primitive (e.g., ParytyCheckbox), follow this template:

### 1. CSS First (add to `design-system-page.css`)

```css
/* === {Component Name} === */

.aef-{component} {
  /* Container styles — use DS tokens for ALL values */
  display: inline-flex;
  align-items: center;
  gap: var(--aef-space-2);
  cursor: pointer;
  /* ... */
}

.aef-{component}__{element} {
  /* Child element styles */
}

.aef-{component}--{modifier} {
  /* Variant styles */
}

/* States: hover, active, focus-visible, disabled */
.aef-{component}:hover { }
.aef-{component}:active { }
.aef-{component}:focus-visible { }
.aef-{component}[aria-disabled="true"] { opacity: 0.4; cursor: not-allowed; }

/* Animation if applicable */
.aef-{component}[data-state="open"] {
  animation: aef-scale-in var(--aef-duration-fast) var(--aef-ease-settle) both;
}
```

### 2. React Component (add to `components/common/`)

```tsx
/**
 * {ComponentName} — {What it replaces and what it does}.
 *
 * @module components/common/{ComponentName}
 */

import { useState, useCallback, type ReactNode } from 'react';
import clsx from 'clsx';

export interface {ComponentName}Props {
  /** ... */
}

export function {ComponentName}({ ... }: {ComponentName}Props) {
  // State, handlers, accessibility attributes
  return (
    <div className="aef-{component}" role="..." aria-...>
      {/* Composition of DS primitives */}
    </div>
  );
}
```

### 3. Accessibility Requirements

Every component MUST have:
- Appropriate ARIA roles (`role`, `aria-selected`, `aria-expanded`, `aria-haspopup`, etc.)
- Keyboard navigation (Tab, Enter, Escape, Arrow keys as applicable)
- Focus management (`focus-visible` styles, `tabIndex`)
- Screen reader labels (`aria-label` or visible label + `aria-labelledby`)
- `data-testid` attributes for testing

---

## Phase 8: Layout Architecture

### App Shell Structure

```
#root
└── .app-shell
    ├── .app-sidebar (244px / 56px collapsed, z: 100)
    │   ├── Brand block (logo, name)
    │   ├── Nav items with sliding indicator
    │   └── User profile + dropdown
    ├── .app-topbar (44px height, z: 90)
    │   ├── Breadcrumb / page title
    │   └── Global actions (time range, alerts, user)
    ├── .app-shell__content
    │   └── <Routed page view>
    └── .app-statusbar (28px height)
        ├── Status items (connection, latency)
        ├── FPS counter
        └── Version / environment
```

### Page View Template

```tsx
<div className="view-container">
  <div className="view-header">
    <h2>Page Title</h2>
    <div className="view-controls">
      {/* Filters, time selectors, actions */}
    </div>
  </div>
  {/* Page-specific content */}
</div>
```

### Panel Overlay Template

```tsx
<div className="detail-panel aef-container-card">
  <div className="aef-container-card__header">
    <span className="aef-container-card__icon"><PanelIcon size={14} /></span>
    <span className="aef-container-card__title">Panel Title</span>
    <button className="aef-container-card__close" onClick={onClose}>
      <X size={12} />
    </button>
  </div>
  <div className="aef-container-card__body aef-scroll">
    {/* Panel content */}
  </div>
</div>
```

---

## Phase 9: Quality Gates

Before declaring any design work complete, pass these gates:

### Gate 1: Token Compliance
- [ ] Zero raw hex values in new code
- [ ] All spacing uses `--aef-space-*` tokens
- [ ] All radii use `--aef-radius-*` tokens
- [ ] All durations use `--aef-duration-*` tokens
- [ ] All easing uses `--aef-ease-*` or `--aef-spring-*` tokens
- [ ] All fonts use `--aef-font-heading` or `--aef-font-body`

### Gate 2: Native Primitive Audit
- [ ] No native `<select>` elements — use `ParytySelect`
- [ ] No `title` attributes for tooltips — use `Tooltip` component
- [ ] No native scrollbars visible — `.aef-scroll` or `.aef-scroll-thin` applied
- [ ] No native context menu — intercepted by `ContextMenuProvider`
- [ ] No native checkboxes/radios visible — use `ParytyCheckbox`/`ParytyRadio` (build if missing)
- [ ] No native focus rings — `:focus-visible` styling applied
- [ ] No browser form validation tooltips — `novalidate` + Toast

### Gate 3: Composition Audit
- [ ] New UI expressed as a composition of existing `aef-*` primitives wherever possible
- [ ] Any new CSS class uses `aef-` prefix and BEM naming
- [ ] Any new React component justified by behavior encapsulation (not just styling)

### Gate 4: Accessibility
- [ ] All interactive elements have `data-testid`
- [ ] All interactive elements are keyboard-navigable
- [ ] Appropriate ARIA roles and states
- [ ] Focus visible on all interactive elements

### Gate 5: Visual Verification
- [ ] Run `npm run dev` and visually verify in browser
- [ ] Check dark theme consistency
- [ ] Verify hover/active/focus states
- [ ] Verify entrance animations play correctly

### Gate 6: Cross-Browser Consistency
- [ ] No browser-specific CSS without fallback
- [ ] Scrollbar styling works in Firefox (`scrollbar-width`) AND Chrome (`::-webkit-scrollbar`)
- [ ] Font renders in Geist Variable/Geist Mono across platforms
- [ ] Flexbox/grid layout consistent across browsers

---

## Phase 10: Build & Verify

After making changes, run the verification pipeline:

```bash
cd paryty-v1.0/frontend && npx tsc --noEmit 2>&1
```

```bash
cd paryty-v1.0/frontend && npm run build 2>&1
```

```bash
cd paryty-v1.0/frontend && npx vitest run 2>&1
```

```bash
cd paryty-v1.0/frontend && npx eslint src/ 2>&1
```

For visual verification:
```bash
cd paryty-v1.0/frontend && npm run dev
```

---

## Summary: The Design Frontend Workflow

```
1. RECEIVE task (design/redesign/build UI)
       │
2. LOAD DS into context (tokens.css, design-system-page.css, globals.css, component catalog)
       │
3. EXTRACT intent (if reference provided — information hierarchy, spatial logic, motion intent)
       │
4. COMPOSE from primitives (can existing aef-* primitives express this?)
       │
   ┌──YES──▶ Build with HTML + aef-* CSS classes + existing React components
   │
   NO
   │
   ▼
5. EXTEND with new aef-* CSS (using DS tokens exclusively)
       │
   Still need behavior?
       │
   YES──▶ 6. CREATE new React component (wrapping behavior + new CSS)
       │
7. AUDIT natives (checklist — every native widget eliminated?)
       │
8. VERIFY (TypeScript, build, tests, lint, visual check)
       │
9. REPORT what was composed, extended, or created
       │
10. DONE
```
