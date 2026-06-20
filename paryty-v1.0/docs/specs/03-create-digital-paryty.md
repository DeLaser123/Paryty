# Step 3: Create a Digital Paryty — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### 1.1 Competing Wizard Implementations

| # | Location | Steps | Abilities Persisted | Quota Check |
|---|----------|-------|---------------------|-------------|
| A | DashboardPage.tsx (CreateParytyWizard modal) | 3: Identity → Abilities → Confirm | No | No |
| B | TwinCreatePage.tsx (standalone) | 4: Identity → Agents → Abilities → Confirm | No | No |
| C | Twins/TwinCreatePage.tsx | Single-page form | No abilities | No |

**Routing:** `/twins/new` redirects to `/?new=true` (wizard A). Wizard B is never routed to. Wizard C is dead code.

### 1.2 Critical Gaps

| Gap | Severity | Description |
|-----|----------|-------------|
| G-01 | Critical | `commitDraft()` sends only name/description, never abilities |
| G-02 | Critical | Backend `CreateTwinRequest` has no `abilities` field |
| G-03 | Critical | `paryty_twins` table has no `abilities` column |
| G-04 | Critical | `backendTwinToDigitalParyty()` hardcodes `abilities: []` |
| G-05 | High | No frontend quota pre-check before creation |
| G-06 | High | Backend `RequireTwinQuota` middleware IS wired but frontend doesn't handle 402 |
| G-07 | Medium | Agent assignment step is placeholder |
| G-08 | Medium | Twin health always returns `unknown` after creation |
| G-09 | Medium | No twin name uniqueness validation |
| G-10 | High | Three competing wizards create maintenance burden |

### 1.3 What Already Works

- `RequireTwinQuota` middleware (quota_middleware.go)
- Plan engine with maxTwins (engine.go)
- Ability catalogue + feature map (ability.ts)
- Dashboard wizard plan-gated abilities
- JWT auth + tenant context
- Agent listing API

### 1.4 Database Migration Required

```sql
-- Migration: Add abilities column to paryty_twins
ALTER TABLE paryty_twins ADD COLUMN IF NOT EXISTS abilities JSONB DEFAULT '[]';
CREATE INDEX IF NOT EXISTS idx_paryty_twins_abilities ON paryty_twins USING GIN (abilities);
```

### 1.5 Twin Status State Machine

**States:** pending → active → degraded → inactive → deleted

**Initial status:** "pending" (twin.go:66)

**Transitions:**
- pending → active (when first agent connects)
- active → degraded (health check failure)
- degraded → active (health restored)
- any → inactive (manual)
- any → deleted (soft delete)

### 1.6 ListTwins Pagination

- Cursor-based pagination using `pageToken` and `pageSize` parameters
- Default pageSize: 20, max: 100
- pageToken is the twin ID of the last item in previous page
- Response includes `nextPageToken` if more results exist

### 1.7 Soft Delete Cascade Behavior

- DeleteTwin sets `deleted_at` timestamp (twin.go:192-220)
- Also removes ALL `agent_assignments` for that twin in same transaction
- Agents are NOT deleted — only unassigned
- agentCount is derived from `agent_assignments` table (twin.go:107-109)

---

## 2. Target State

### 2.1 Flow

```
User clicks "New Digital Paryty"
        │
        ▼
┌─────────────────────┐
│  Quota Pre-Check     │ ← planStore + catalogue.length
│  (>= maxTwins?)      │
└──────┬──────────────┘
       │
  ┌────┴────┐
  │ YES     │ NO
  ▼         ▼
┌────────┐  ┌──────────────────────────────────────┐
│Upgrade │  │  Wizard Modal (4 steps)               │
│Prompt  │  │  Step 1: Identity (name, system)       │
└────────┘  │  Step 2: Abilities (plan-gated grid)   │
            │  Step 3: Agents (multi-select)          │
            │  Step 4: Confirm (summary card)          │
            └──────────┬───────────────────────────────┘
                       │
                       ▼
              POST /api/v1/twins
              { name, description, abilities[], agentIds[] }
                       │
                  ┌────┴────┐
                  │ 201     │ 402
                  ▼         ▼
           ┌──────────┐  ┌────────────┐
           │ Success   │  │ Quota      │
           │ Toast     │  │ Exceeded   │
           │ Navigate  │  │ Upgrade    │
           │ → /twins/ │  │ Prompt     │
           │   :id     │  └────────────┘
           └──────────┘
```

### 2.2 Consolidation Strategy

- **Keep:** CreateParytyWizard in DashboardPage.tsx (wizard A) — primary entry point
- **Deprecate:** TwinCreatePage.tsx (wizard B) — replace with redirect to `/?new=true`
- **Remove:** Twins/TwinCreatePage.tsx (wizard C) — dead code
- **Enhance:** Wizard A to include Agents step and ability persistence

---

## 3. UI Specification

### 3.1 Wizard Modal

```tsx
<div className="aef-modal-overlay">
  <div className="aef-modal" style={{ maxWidth: 600 }}>
    <div className="aef-modal-header">
      <span className="aef-modal-title">New Digital Paryty — {stepLabel}</span>
      <button className="aef-modal-close">✕</button>
    </div>
    <div className="aef-modal-body">
      {stepContent}
    </div>
    <div className="aef-modal-footer">
      <StepIndicator total={4} current={step} />
      <div>{Back} {Continue/Create}</div>
    </div>
  </div>
</div>
```

### 3.2 Step 1: Identity

- **Name field:** Required, max 80 chars, async uniqueness check
- **System label field:** Optional, max 80 chars, placeholder "e.g. Payment Gateway v3"
- **Helper text:** "A Digital Paryty is a live-telemetry-driven digital twin..."
- **Validation:** `canAdvance = name.trim().length > 0 && !isNameDuplicate`

### 3.3 Step 2: Abilities

```tsx
<div className="dp-ability-grid">
  {ABILITY_CATALOGUE.map((ability) => {
    const featureName = ABILITY_FEATURE_MAP[ability.id];
    const isAvailable = !featureName || hasFeature(featureName);
    
    return (
      <button
        key={ability.id}
        className={clsx(
          'dp-ability-tile',
          isSelected && 'dp-ability-tile--selected',
          !isAvailable && 'dp-ability-tile--disabled',
        )}
        onClick={() => isAvailable && toggle(ability.id)}
        disabled={!isAvailable}
      >
        <CheckCircle2 size={14} className="dp-ability-tile__check" />
        <span className="dp-ability-tile__icon">{icon}</span>
        <span className="dp-ability-tile__name">
          {ability.name}{!isAvailable && ' (Upgrade to unlock)'}
        </span>
        <span className="dp-ability-tile__desc">{ability.description}</span>
      </button>
    );
  })}
</div>
```

**Ability-to-feature gating:**
```typescript
const ABILITY_FEATURE_MAP: Record<AbilityId, string> = {
  topology_observation: 'topology_monitoring',
  metrics_monitoring:   'metrics',
  alerts:               'alerts',
  timeline_replay:      'timeline_replay',
  forecasting:          'paryty_intel',
  watif_drills:         'paryty_intel',
};
```

### 3.4 Step 3: Agents

- Multi-select from available agents
- Empty state: "No agents available. Agents will be linked after creation."
- Agent row: Name, hostname, OS, status badge
- Skip allowed

### 3.5 Step 4: Confirm

Summary card with: Name, System, Abilities (badges), Agents (names), all in dp-confirm-row format.

### 3.6 Quota Exceeded Modal

```tsx
<div className="aef-modal" style={{ maxWidth: 420 }}>
  <div className="aef-modal-header">
    <span>Twin Limit Reached</span>
  </div>
  <div className="aef-modal-body">
    <p>You've reached the maximum of {maxTwins} Digital Parytys for your {planName} plan.</p>
    <p style={{ color: 'var(--aef-text-secondary)', fontSize: 11 }}>
      Upgrade your plan to create more Digital Parytys.
    </p>
  </div>
  <div className="aef-modal-footer">
    <button className="aef-btn aef-btn-inactive">Cancel</button>
    <button className="aef-btn aef-btn-active">Upgrade Plan</button>
  </div>
</div>
```

---

## 4. API Contract

### POST /api/v1/twins (Enhanced)

**Request:**
```json
{
  "name": "Payment Gateway Twin",
  "description": "Payment Gateway v3",
  "abilities": ["topology_observation", "metrics_monitoring", "alerts"],
  "agent_ids": ["agent-uuid-1", "agent-uuid-2"]
}
```

**Success (201):**
```json
{
  "data": {
    "id": "twin-uuid",
    "name": "Payment Gateway Twin",
    "status": "active",
    "abilities": ["topology_observation", "metrics_monitoring", "alerts"],
    "agentCount": 0,
    "createdAt": "2026-06-18T12:00:00Z"
  }
}
```

**Quota Exceeded (402):**
```json
{ "error": "TWIN_LIMIT_REACHED", "message": "Twin quota exceeded.", "details": { "current": 2, "limit": 2 } }
```

**Name Conflict (409):**
```json
{ "error": "CONFLICT", "message": "A Digital Paryty with this name already exists." }
```

---

## 5. Implementation Tasks

### Phase 1: Backend — Abilities Persistence

| Task | File | Description |
|------|------|-------------|
| T-01 | proto/paryty/v1/auth.proto | Add `repeated string abilities` to CreateTwinRequest |
| T-02 | cluster/internal/twin/twin.go | Add Abilities to TwinConfig, store in JSON column |
| T-03 | cluster/internal/auth/twin_rest_adapter.go | Accept and forward abilities |
| T-04 | cluster/internal/api/query/rest.go | Parse abilities from request body |
| T-05 | cluster/internal/twin/twin.go | Add name uniqueness check |

### Phase 2: Frontend — API & Types

| Task | File | Description |
|------|------|-------------|
| T-06 | frontend/src/api/twins.ts | Add abilities and agentIds to CreateTwinPayload |
| T-07 | frontend/src/types/digitalParyty.ts | Add abilities to TwinDetails, update mapper |

### Phase 3: Frontend — Consolidated Wizard

| Task | File | Description |
|------|------|-------------|
| T-08 | DashboardPage.tsx | Add Agents step, update step count to 4 |
| T-09 | dashboardStore.ts | Update commitDraft to include abilities and agentIds |
| T-10 | dashboardStore.ts | Handle 402 response with quota error |
| T-11 | DashboardPage.tsx | Add name uniqueness check |

### Phase 4: Frontend — Quota Enforcement

| Task | File | Description |
|------|------|-------------|
| T-12 | DashboardPage.tsx | Add quota pre-check before opening wizard |
| T-13 | DashboardPage.tsx | Add quota exceeded modal |

### Phase 5: Cleanup

| Task | File | Description |
|------|------|-------------|
| T-14 | TwinCreatePage.tsx | Replace with redirect to /?new=true |
| T-15 | Twins/TwinCreatePage.tsx | Remove dead code |

---

## 6. Acceptance Criteria

| AC | Criterion |
|----|-----------|
| AC-01 | Selected abilities persist to backend and returned on GET |
| AC-02 | Abilities displayed on dashboard twin cards |
| AC-03 | Quota enforcement blocks creation at maxTwins |
| AC-04 | Frontend shows upgrade prompt when quota reached |
| AC-05 | Single wizard is the only creation path |
| AC-06 | Name uniqueness validation prevents duplicates |
| AC-07 | Plan-gated abilities disabled with upgrade prompt |
| AC-08 | Agent assignment step shows available agents |
| AC-09 | Created twin appears in dashboard catalogue |
| AC-10 | Success toast and redirect to twin detail |

---

## 7. Testing Strategy

- Unit tests: CreateTwin with abilities, name uniqueness, quota enforcement
- Integration: Create twin → fetch → verify abilities persisted
- E2E: Full wizard flow, quota reached modal, name duplicate error, plan gating
