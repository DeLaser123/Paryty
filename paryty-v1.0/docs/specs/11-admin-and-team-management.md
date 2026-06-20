# Step 11: Admin & Team Management — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### What Exists

| Component | File | Status |
|-----------|------|--------|
| SettingsPage | `frontend/src/pages/SettingsPage.tsx` | 4 tabs: Account, Plan, API Keys, Users |
| AccountTab | Lines 47-167 | Email/name display, change password form |
| PlanTab | Lines 163-279 | Shows plan, features, limits. **Upgrade button has NO onClick** |
| ApiKeysTab | Lines 283-488 | Create, list, rotate, delete keys |
| UsersTab | Lines 492-646 | Lists sub-users, create form. **No edit/delete inline** |
| UserListPage | `frontend/src/pages/Users/UserListPage.tsx` | Full CRUD (separate route) |
| planStore | `frontend/src/stores/planStore.ts` | Fetches plans, feature gating |
| RBAC | `cluster/internal/security/rbac.go` | RequireRole, RequirePermission |
| API Keys | `cluster/internal/controlplane/apikey.go` | Generate, validate, revoke, rotate |
| User CRUD | `cluster/internal/api/query/rest.go` | ListUsers, CreateUser, GetUser, UpdateUser, DeleteUser |
| Plan API | rest.go | GET /api/v1/plans, POST /api/v1/tenant/plan/change |

### Critical Gaps

1. **Upgrade button broken** — No onClick handler
2. **No change password backend** — Frontend calls endpoint that doesn't exist
3. **No plan change frontend integration** — Backend endpoint exists but frontend doesn't call it
4. **User management fragmented** — SettingsPage UsersTab vs separate UserListPage
5. **No inline role editing** — Must navigate to separate route
6. **No RBAC feedback** — Disabled buttons show no tooltip
7. **No usage meters** — Plan shows limits but not current usage
8. **Route protection too strict** — `/settings` requires admin, blocks non-admin users

### 1.3 Existing User Management Pages

- **UserListPage** (`frontend/src/pages/Users/UserListPage.tsx`): Full CRUD with search, pagination, delete dialog (493 lines)
- **UserCreatePage** (`frontend/src/pages/Users/UserCreatePage.tsx`): Dedicated create page
- **UserEditPage** (`frontend/src/pages/Users/UserEditPage.tsx`): Dedicated edit page
- **UserDeleteDialog** (`frontend/src/pages/Users/UserDeleteDialog.tsx`): Confirmation dialog
- **fetchUsers API** (`frontend/src/api/users.ts`): Dedicated API function

### 1.4 Existing Plan Store Helpers

- `hasFeature(feature: string)` — check if current plan includes feature (`planStore.ts:84-88`)
- `usageFor(resource, current)` — get usage info with percentage (`planStore.ts:90-100`)
- `isNearLimit(resource, current)` — check if usage is near limit (`planStore.ts:102-119`)

### 1.5 Route Protection Details

- App.tsx wraps `/settings` with `<ProtectedRoute requiredRoles={['admin']}>`
- SettingsPage internally hides Users tab for non-admins (line 661)
- Non-admin users are BLOCKED from entire settings page by route guard
- Fix needed: Remove requiredRoles from route, let SettingsPage handle tab visibility

---

## 2. Target State

### User Journey

```
Settings (/settings)
├── Account Tab (all roles)
│   ├── View profile (email, name, role badge)
│   ├── Change password form
│   └── Session info
│
├── API Keys Tab (all roles, write requires operator+)
│   ├── Key list table
│   ├── Create key modal → one-time key display
│   ├── Rotate key → confirmation → one-time key display
│   └── Revoke key → confirmation
│
├── Users Tab (admin-only)
│   ├── User table (name, email, role badge, status, actions)
│   ├── Create/Edit/Delete user modals
│   └── Inline role change
│
└── Plan & Billing Tab (admin-only for changes)
    ├── Current plan card
    ├── Usage meters (twins, agents, sub-users, alert rules)
    ├── Feature comparison table
    └── Upgrade/Downgrade flow
```

---

## 3. UI Specification

### 3.1 Tab Bar

```tsx
<div className="settings-tab-bar" role="tablist">
  {visibleTabs.map(tab => (
    <button
      key={tab.id}
      role="tab"
      aria-selected={activeTab === tab.id}
      className={clsx('settings-tab-btn', activeTab === tab.id && 'settings-tab-btn--active')}
    >
      {tab.icon} {tab.label}
    </button>
  ))}
</div>
```

### 3.2 Account Tab

**Profile rows:** Email, Name, Role (with RoleBadge), all in dp-confirm-row format.

**RoleBadge:**
```tsx
<span className="aef-badge" style={{
  background: role === 'admin' ? 'var(--aef-accent)' : role === 'operator' ? 'var(--aef-warning)' : 'var(--aef-surface-low)',
  color: role === 'admin' ? '#fff' : role === 'operator' ? '#000' : 'var(--aef-text-secondary)',
  borderRadius: 'var(--aef-radius-full)',
  padding: '2px 8px',
  fontSize: 11,
  textTransform: 'capitalize',
}}>
  <Shield size={10} /> {role}
</span>
```

**Change Password Form:**
```tsx
<div className="dp-field">
  <label className="dp-field__label">Current password</label>
  <input className="dp-field__input" type="password" />
</div>
<div className="dp-field">
  <label className="dp-field__label">New password</label>
  <input className="dp-field__input" type="password" />
</div>
<div className="dp-field">
  <label className="dp-field__label">Confirm new password</label>
  <input className="dp-field__input" type="password" />
</div>
<button className="aef-btn aef-btn-active" onClick={handleChangePassword}>
  Change Password
</button>
```

### 3.3 API Keys Tab

**Key List Table:**
```tsx
<table className="aef-table">
  <thead>
    <tr><th>Name</th><th>Prefix</th><th>Created</th><th>Actions</th></tr>
  </thead>
  <tbody>
    {keys.map(key => (
      <tr key={key.id}>
        <td>{key.name}</td>
        <td><code>{key.prefix}…</code></td>
        <td>{new Date(key.createdAt).toLocaleDateString()}</td>
        <td>
          <RbacButton permission="api_keys:write" onClick={() => handleRotate(key)}>
            <ArrowUpRight size={12} />
          </RbacButton>
          <RbacButton permission="api_keys:write" onClick={() => handleDelete(key)}>
            <Trash2 size={12} />
          </RbacButton>
        </td>
      </tr>
    ))}
  </tbody>
</table>
```

**One-Time Key Display:**
```tsx
<div className="aef-container-card" style={{ borderColor: 'var(--aef-accent)', borderWidth: 2 }}>
  <div className="aef-container-card__body">
    <p style={{ color: 'var(--aef-accent)', fontWeight: 600 }}>
      Key created! Copy it now — it won't be shown again.
    </p>
    <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
      <code style={{ flex: 1 }}>{newKey.key}</code>
      <button className="aef-btn aef-btn-active" onClick={handleCopy}>
        {copied ? 'Copied' : 'Copy'}
      </button>
    </div>
  </div>
</div>
```

### 3.4 Users Tab (Admin-Only)

**User Table:**
```tsx
<table className="aef-table">
  <thead>
    <tr><th>User</th><th>Email</th><th>Role</th><th>Status</th><th>Actions</th></tr>
  </thead>
  <tbody>
    {users.map(u => (
      <tr key={u.id}>
        <td><Users size={14} /> {u.name}</td>
        <td>{u.email}</td>
        <td><RoleBadge role={u.role} /></td>
        <td><StatusBadge active={u.isActive} /></td>
        <td>
          <button onClick={() => handleEdit(u)}><Edit size={12} /></button>
          <button onClick={() => setDeleteUser(u)}><Trash2 size={12} /></button>
        </td>
      </tr>
    ))}
  </tbody>
</table>
```

**Create User Modal:** Email, Name, Password, Role selector (Admin/Operator/Viewer)

**Edit User Modal:** Name (editable), Role (dropdown), Email (read-only)

### 3.5 Plan & Billing Tab

**Current Plan Card:** Plan name, started date, expires date.

**Usage Meters:**
```tsx
function UsageMeter({ label, current, max }) {
  const pct = max > 0 ? Math.min(100, Math.round((current / max) * 100)) : 0;
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <span>{label}</span>
        <span>{current} / {max}</span>
      </div>
      <div style={{ height: 6, background: 'var(--aef-border)', borderRadius: 'var(--aef-radius-full)' }}>
        <div style={{
          height: '100%',
          width: `${pct}%`,
          background: pct >= 100 ? 'var(--aef-error)' : pct >= 80 ? 'var(--aef-warning)' : 'var(--aef-accent)',
          borderRadius: 'var(--aef-radius-full)',
        }} />
      </div>
    </div>
  );
}
```

**Available Plans Grid:**
```tsx
<div className="dp-ability-grid">
  {plans.map(plan => (
    <div key={plan.name} className={clsx('dp-ability-tile', plan.name === currentPlan && 'dp-ability-tile--selected')}>
      <span className="dp-ability-tile__name">{plan.displayName}</span>
      <span className="dp-ability-tile__desc">Up to {plan.maxTwins} Parytys</span>
      {plan.name !== currentPlan && (
        <RbacButton permission="tenants:write" className="aef-btn aef-btn-active" onClick={() => handleUpgrade(plan)}>
          <ArrowUpRight size={12} /> {isUpgrade(plan.name) ? 'Upgrade' : 'Downgrade'}
        </RbacButton>
      )}
    </div>
  ))}
</div>
```

### 3.6 RbacButton Component

```tsx
function RbacButton({ permission, tooltip, children, ...props }) {
  const user = useAuthStore((s) => s.user);
  const hasPermission = user?.permissions?.[permission] === true;
  const isDisabled = props.disabled || !hasPermission;
  
  return (
    <Tooltip label={isDisabled ? `Permission required: ${permission}` : tooltip}>
      <button {...props} disabled={isDisabled} style={{ ...props.style, opacity: isDisabled ? 0.4 : 1 }}>
        {children}
      </button>
    </Tooltip>
  );
}
```

---

## 4. API Contract

### POST /api/v1/auth/change-password (NEW)

```json
{ "currentPassword": "...", "newPassword": "..." }
```

**Response (200):** `{ "message": "Password changed successfully" }`

### POST /api/v1/tenant/plan/change

```json
{ "planName": "pro_plus" }
```

**Response (200):** `{ "data": { "planName": "pro_plus", "message": "plan updated" } }`

### User CRUD

- GET /api/v1/users — List users
- POST /api/v1/users — Create user
- PUT /api/v1/users/:id — Update user (name, role)
- DELETE /api/v1/users/:id — Delete user

---

## 5. Implementation Tasks

### Phase 1: Backend

| Task | File | Description |
|------|------|-------------|
| B-01 | auth_rest_adapter.go | Add ChangePassword endpoint |
| B-02 | main.go | Register change-password route |

### Phase 2: Frontend — Fix Upgrade Button

| Task | File | Description |
|------|------|-------------|
| F-01 | SettingsPage.tsx | Add upgrade handler to PlanTab |
| F-02 | SettingsPage.tsx | Add upgrade confirmation modal |
| F-03 | planStore.ts | Add changePlan action |

### Phase 3: Frontend — Enhanced Users Tab

| Task | File | Description |
|------|------|-------------|
| F-04 | SettingsPage.tsx | Replace UsersTab with full CRUD table |
| F-05 | SettingsPage.tsx | Add create/edit/delete user modals |
| F-06 | New: RoleBadge.tsx | Reusable role badge component |

### Phase 4: Frontend — RBAC Feedback

| Task | File | Description |
|------|------|-------------|
| F-07 | New: RbacButton.tsx | Permission-aware button component |
| F-08 | SettingsPage.tsx | Apply RbacButton to all action buttons |

### Phase 5: Frontend — Usage Meters

| Task | File | Description |
|------|------|-------------|
| F-09 | New: UsageMeter.tsx | Progress bar with current/max |
| F-10 | SettingsPage.tsx | Add usage meters to PlanTab |

### Phase 6: Frontend — Confirmation Modals

| Task | File | Description |
|------|------|-------------|
| F-11 | New: ConfirmationModal.tsx | Reusable confirmation modal |
| F-12 | SettingsPage.tsx | Replace window.confirm() with modals |

### Phase 7: Route Protection Fix

| Task | File | Description |
|------|------|-------------|
| F-13 | App.tsx | Remove requiredRoles from /settings route |

---

## 6. Acceptance Criteria

| AC | Criterion |
|----|-----------|
| AC-01 | Upgrade button works — calls API, shows success |
| AC-02 | User CRUD complete — create, edit, delete |
| AC-03 | API key rotation works with confirmation modal |
| AC-04 | RBAC enforced — disabled buttons with tooltips |
| AC-05 | Change password works |
| AC-06 | Usage meters show current/max |
| AC-07 | Confirmation modals replace window.confirm() |
| AC-08 | Non-admin users can access settings |
| AC-09 | Users tab hidden for non-admins |
| AC-10 | Empty states show for each tab |
