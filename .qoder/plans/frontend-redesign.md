# Frontend Redesign Plan — Paryty DS Enforcement

## Context

The Paryty frontend has inconsistent typography scaling and several pages that don't fully utilize the Paryty Design System (DS) primitives. The user wants:

1. **Global typography scaling**: Non-title texts at 12px, title/header fonts at 14px or 16px
2. **Dashboard page redesign**: Enterprise-grade dashboard summarizing client's account
3. **Loading animation redesign**: Beautiful stress-relieving animation instead of "Loading..." text
4. **Agents page redesign**: Use `aef-table-card` + `aef-container-card` modal for agent details
5. **Twin list redesign**: Make it more DS-like
6. **Active twin highlighting fix**: Current implementation violates DS rules (uses hardcoded hex)
7. **API key management redesign**: Use `aef-table-card` for key listing

## Implementation Tasks

### Task 1: Global Typography Scaling

**Files to modify:**
- `frontend/src/styles/typography.css`
- `frontend/src/styles/utilities.css`
- `frontend/src/paryty_design_system/design-system-page.css`

**Changes:**

#### typography.css
- Update heading font sizes:
  - h1: 21px (keep)
  - h2: 11px → 16px
  - h3: 12px → 14px
  - h4: 12px → 14px
  - h5: 11px → 12px
  - h6: 9px → 10px
- Update body text:
  - p: 12px (keep)
  - small: 9px → 10px
  - code/kbd/samp/pre: 11px → 12px
  - code (inline): 9px → 10px

#### utilities.css
- Update text size utility classes:
  - .aef-text-xs: 9px → 10px
  - .aef-text-sm: 11px → 12px
  - .aef-text-base: 12px (keep)
  - .aef-text-lg: 14px (keep)
  - .aef-text-xl: 15px → 16px

#### design-system-page.css
- Update DS component font sizes:
  - .aef-btn: 10px → 12px
  - .aef-counter__label: 8px → 10px
  - .aef-counter__value: 10px → 12px
  - .aef-table th: 8px → 10px
  - .aef-table td: 10px → 12px
  - .aef-badge: 8px → 10px
  - .aef-meta-pill: 8px → 10px
  - .aef-stat-module__label: 8px → 10px
  - .aef-stat-module__value: 11px → 12px
  - .aef-container-card__title: 11px → 12px
  - .aef-table-card__header: 11px → 12px
  - .ds-nav-item__label: 10px → 12px
  - .aef-modal-title: 11px → 12px

### Task 2: Dashboard Page Redesign

**Files to modify:**
- `frontend/src/components/dashboard/DashboardPage.tsx`
- `frontend/src/components/dashboard/dashboard.css`

**Design Approach:**
The dashboard should be an enterprise-grade summary with:

1. **Header Section**: Page title + "New Digital Paryty" button
2. **Counter Strip**: 4 counters showing Total Twins, Healthy, Degraded, Abilities
3. **Twin Catalogue Grid**: Card grid of Digital Parytys using `aef-container-card`
4. **Empty State**: When no twins exist

**Key Changes:**
- Keep the existing structure but ensure all elements use DS primitives
- Update font sizes to match new typography scale
- Fix active twin highlighting (see Task 5)

### Task 3: Loading Animation Redesign

**Files to modify:**
- `frontend/src/App.tsx`
- `frontend/src/index.css`

**Design Approach:**
Create a beautiful, stress-relieving loading animation using DS tokens:

1. **Animated Logo/Brand**: Pulsing Paryty logo with subtle glow
2. **Progress Indicator**: Animated progress bar using `aef-progress-track`
3. **Breathing Animation**: Subtle scale/opacity animation for calm effect

**Implementation:**
```tsx
function LoadingFallback() {
  return (
    <div className="loading-fallback">
      <div className="loading-fallback__brand">
        <div className="loading-fallback__logo">P</div>
        <div className="loading-fallback__text">Paryty</div>
      </div>
      <div className="loading-fallback__progress">
        <div className="aef-progress-track">
          <div className="aef-progress-fill loading-fallback__progress-fill" />
        </div>
      </div>
      <div className="loading-fallback__message">Initializing workspace…</div>
    </div>
  );
}
```

**CSS Animation:**
```css
.loading-fallback {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--aef-space-4);
  height: 100%;
  background: var(--aef-bg);
}

.loading-fallback__logo {
  width: 48px;
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--aef-font-heading);
  font-size: 24px;
  font-weight: 700;
  color: var(--aef-text-primary);
  background: var(--aef-surface-card);
  border: var(--aef-border-width) solid var(--aef-border);
  border-radius: var(--aef-radius-card);
  animation: loading-breathe 2s ease-in-out infinite;
}

@keyframes loading-breathe {
  0%, 100% { transform: scale(1); opacity: 1; }
  50% { transform: scale(1.05); opacity: 0.8; }
}

.loading-fallback__progress-fill {
  animation: loading-progress 2s ease-in-out infinite;
}

@keyframes loading-progress {
  0% { width: 0%; }
  50% { width: 70%; }
  100% { width: 100%; }
}
```

### Task 4: Agents Page Redesign

**Files to modify:**
- `frontend/src/pages/AgentsPage.tsx`

**Design Approach:**
Convert from raw HTML table to DS `aef-table-card` + `aef-container-card` modal:

1. **Page Header**: Title + "Create Agent" button
2. **Agent Table Card**: Use `aef-table-card` with proper DS styling
3. **Agent Detail Modal**: Use `aef-container-card` for agent details when row is clicked

**Key Changes:**
- Replace `<table>` with `aef-table-card` structure
- Add row selection with `aef-card-active` class
- Create agent detail modal using `aef-container-card`
- Update font sizes to match new typography scale

### Task 5: Active Twin Highlighting Fix

**Files to modify:**
- `frontend/src/components/dashboard/dashboard.css`

**Problem:**
Current active state uses hardcoded hex values:
```css
.dp-card--active {
  background: #ffffff;
  color: #000000;
}
```

**Solution:**
Use DS tokens for selected state:
```css
.dp-card--active {
  border-color: var(--aef-text-primary);
  background: var(--aef-selected-bg);
  color: var(--aef-selected-text);
}

.dp-card--active .dp-card__name {
  color: var(--aef-selected-text);
}

.dp-card--active .dp-card__system {
  color: var(--aef-text-secondary);
}
```

### Task 6: API Key Management Redesign

**Files to modify:**
- `frontend/src/pages/SettingsPage.tsx`

**Design Approach:**
Convert API keys tab to use DS primitives:

1. **Create Form**: Use `aef-container-card` with proper form layout
2. **Key List**: Use `aef-table-card` to display keys
3. **New Key Display**: Use `aef-container-card` with accent border

**Key Changes:**
- Replace div-based key list with `aef-table-card`
- Add proper table headers: Name, Prefix, Created, Actions
- Use DS button styles for Rotate/Delete actions
- Update font sizes to match new typography scale

## Verification Steps

1. **TypeScript Compilation**:
   ```bash
   cd paryty-v1.0/frontend && npx tsc --noEmit
   ```

2. **Build**:
   ```bash
   cd paryty-v1.0/frontend && npm run build
   ```

3. **Visual Verification**:
   ```bash
   cd paryty-v1.0/frontend && npm run dev
   ```
   - Check typography scaling across all pages
   - Verify loading animation appears
   - Test agents page table-card and modal
   - Verify active twin highlighting uses DS tokens
   - Check API keys table-card layout

4. **Lint**:
   ```bash
   cd paryty-v1.0/frontend && npx eslint src/
   ```

## Summary

This plan addresses all 7 user requirements:

1. ✅ Global typography scaling (12px body, 14-16px headers)
2. ✅ Dashboard page redesign (enterprise-grade layout)
3. ✅ Loading animation redesign (breathing animation with progress)
4. ✅ Agents page redesign (table-card + modal)
5. ✅ Twin list redesign (DS-compliant styling)
6. ✅ Active twin highlighting fix (use DS tokens)
7. ✅ API key management redesign (table-card layout)

All changes strictly adhere to Paryty DS rules using `--aef-*` tokens exclusively.
