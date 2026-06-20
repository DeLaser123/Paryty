# Step 1: Sign Up & Plan Selection — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### 1.1 What Exists

| Component | File | Status |
|-----------|------|--------|
| RegisterPage | `frontend/src/pages/RegisterPage.tsx` | 3-step wizard (Account → Plan → Confirm) |
| StepIndicator | `frontend/src/components/common/StepIndicator.tsx` | Dot-based step indicator |
| AuthHint | `frontend/src/components/common/AuthHint.tsx` | Contextual help with password strength |
| authStore | `frontend/src/stores/authStore.ts` | Register action with JWT handling |
| planStore | `frontend/src/stores/planStore.ts` | Plan fetching and feature flags |
| AuthRESTAdapter | `cluster/internal/auth/auth_rest_adapter.go` | Register handler |
| AuthHandler | `cluster/internal/auth/handler.go` | Registration with tenant+user creation |
| Password Utils | `cluster/internal/auth/password.go` | bcrypt hashing, policy validation |
| PlanEngine | `cluster/internal/plan/engine.go` | Plan management, assignment, validation |
| Plans Config | `configs/cluster/plans.yaml` | 4 plans (basic, pro, pro_plus, enterprise) |
| Design Tokens | `frontend/src/paryty_design_system/tokens.css` | All --aef-* tokens defined |

### 1.2 What Works

1. Complete registration flow: Frontend wizard → Backend API → Database → JWT tokens → Redirect
2. Password policy enforcement: Client-side and server-side (8+ chars, uppercase, lowercase, digit, special)
3. Plan selection: Fetches from `/api/v1/plans`, filters `creatable: true`
4. JWT authentication: Access token (15min) in memory, refresh token (7d) in httpOnly cookie
5. **Dual httpOnly cookies:** Both `paryty_access_token` (Path=`/`) and `paryty_refresh_token` (Path=`/api/v1/auth`) are set as httpOnly cookies (auth_rest_adapter.go:212-220). The access token cookie enables WebSocket/SSE authentication without query parameters.
6. **refreshToken removed from response body:** The response initially contains `refreshToken` but it is deleted at line 223 of auth_rest_adapter.go before sending — only the cookie carries the refresh token.
7. Tenant isolation: Row-Level Security (RLS) on all tenant-scoped tables
8. Audit logging: Registration events logged to `audit_log` table
9. Duplicate email detection: Unique constraint with proper 409 response
10. **Plan assignment graceful degradation:** If plan assignment fails post-commit (handler.go:218-223), the error is logged but registration continues — the user gets a default plan.

### 1.3 What's Missing

| Issue | Severity | Description |
|-------|----------|-------------|
| No Terms/Privacy Checkbox | Medium | Missing required legal consent |
| No Email Verification | Low | Registration doesn't require verification |
| No Rate Limiting on Register | Medium | Only login has rate limiting (5 attempts/15min). Registration has NO rate limiting — vulnerable to abuse. |
| Missing Password Strength Meter | Low | AuthHint shows rules but no visual meter |
| Plan Cards Lack Pricing | Medium | Plans show limits but no pricing |
| No Confirmation Email | Low | No email sent after registration |
| No CSRF Protection | Medium | SameSite=Strict cookies provide some protection, but no CSRF token mechanism exists |
| No Concurrent Registration Handling | Medium | Two simultaneous registrations with same email rely on DB unique constraint (23505 error code) — no application-level locking |
| No Input Sanitization Spec | Medium | XSS prevention, HTML escaping, SQL injection prevention not specified |

---

## 2. Target State Specification

### 2.1 User Journey

1. User navigates to `/register`
2. PublicRoute checks auth state → if authenticated, redirect to `/`
3. RegisterPage mounts → useEffect calls fetchPlans() → GET /api/v1/plans (skipAuth: true)
4. **Step 1: Account Details** — Email, Password, Name, Organization fields with AuthHint panel
5. **Step 2: Plan Selection** — Radio-style tile buttons for creatable plans, auto-selects first
6. **Step 3: Confirmation** — Review all details, Terms/Privacy checkbox
7. Submit → POST /api/v1/auth/register → Backend: Validate → Hash password → Create tenant → Create user → Assign plan → Generate JWT → Set httpOnly cookie
8. Success → Store access token in memory → Populate plan store → Show toast → Navigate to `/`
9. Post-registration → AuthProvider wires RestClient → WebSocket connects → Dashboard renders

### 2.2 Step-by-Step Experience

#### Step 1: Account Details
- Email field: Auto-focused, email icon, placeholder "you@example.com", live format validation
- Password field: Lock icon, visibility toggle, placeholder "MyP@ssw0rd"
- Name field: User icon, placeholder "Jane Smith"
- Organization field: Building2 icon, placeholder "Acme Corp"
- AuthHint panel: 260px wide, floats right on desktop, stacks on mobile
  - Default: "Create your Paryty workspace in three easy steps..."
  - Email focused: Live validation indicator (✓/✗)
  - Password focused: 5 animated requirement checkmarks
  - Name focused: "Your display name..."
  - Organization focused: "Your company or team name..."

#### Step 2: Plan Selection
- Grid layout with responsive tile buttons
- Each card: Shield icon, display name, max twins, sub-users limit, check badge when selected
- Auto-selection of first creatable plan
- Loading state: Centered spinner with "Loading available plans…"
- Error state: AlertTriangle icon + error message + Retry button
- Empty state: "No plans are currently available."

#### Step 3: Confirmation
- Review rows: Email, Name, Organization, Plan
- Each row: Icon (12px) + Label + Value
- Staggered animation: Rows appear with 40ms delay
- Terms/Privacy checkbox (required)

### 2.3 Navigation

- Back button: Visible on Steps 2-3, `aef-btn aef-btn-inactive` style
- Continue button: `aef-btn aef-btn-active`, disabled until validation passes
  - Steps 1-2: "Continue →" with ArrowRight icon
  - Step 3: "Create Workspace" with UserPlus icon
- Step Indicator: 3 dots at bottom-left, active dot highlighted
- Footer link: "Already have an account? Sign in" → `/login`

---

## 3. UI Component Specification

### 3.1 Design Tokens

```css
--aef-bg: #000000                    /* Page background */
--aef-surface-card: #141414          /* Card background */
--aef-border: #212121                /* Default border */
--aef-border-strong: #444444         /* Hover/focus border */
--aef-text-primary: #ffffff          /* Primary text */
--aef-text-secondary: #656565        /* Secondary text */
--aef-btn-active-bg: #ffffff         /* Active button */
--aef-btn-active-text: #000000       /* Active button text */
--aef-btn-inactive-bg: #080808       /* Inactive button */
--aef-btn-inactive-border: 1px solid #212121
--aef-status-live: #4ade80           /* Success color */
--aef-counter-variant-b: #DC4714     /* Error color */
--aef-font-heading: 'Geist Variable', system-ui, sans-serif
--aef-font-body: 'Geist Mono', 'IBM Plex Mono', monospace
--aef-radius-control: 10px
--aef-radius-field: 12px
--aef-radius-card: 14px
--aef-space-1 through --aef-space-12
--aef-duration-fast: 120ms
--aef-duration-standard: 220ms
--aef-ease-settle: cubic-bezier(0.34, 1.56, 0.64, 1)
```

### 3.2 Component Tree

```
RegisterPage (div.auth-page)
├── auth-card (div.aef-container-card)
│   ├── aef-container-card__header
│   │   ├── Shield icon (14px)
│   │   └── "Paryty" title
│   ├── aef-container-card__body
│   │   ├── auth-card__header
│   │   │   ├── h1.auth-card__title "Create your workspace"
│   │   │   └── p.auth-card__subtitle "Step {n} of 3: {stepName}"
│   │   ├── form.auth-form
│   │   │   ├── auth-form__error (conditional)
│   │   │   ├── StepContent (keyed by step)
│   │   │   └── Navigation Row
│   │   │       ├── StepIndicator (3 dots)
│   │   │       └── Button Group (Back + Continue/Submit)
│   │   └── auth-card__footer
│   │       └── Link to /login
│   └── AuthHint (conditional, Step 0 only)
```

### 3.3 Visual Specifications

#### Auth Page Layout
```css
.auth-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background-color: var(--aef-bg);
  padding: var(--aef-space-4);
  gap: var(--aef-space-6);
}
```

#### Form Fields
```css
.dp-field__input {
  width: 100%;
  padding: var(--aef-space-2) var(--aef-space-3);
  padding-left: var(--aef-space-8);
  background: var(--aef-surface-low);
  border: var(--aef-border-width) solid var(--aef-border);
  border-radius: var(--aef-radius-field);
  color: var(--aef-text-primary);
  font-family: var(--aef-font-body);
  font-size: 12px;
  transition: border-color var(--aef-duration-fast) var(--aef-ease-exit);
}
.dp-field__input:focus {
  border-color: var(--aef-border-strong);
  outline: none;
}
```

#### Password Strength Indicator
```css
.auth-hint__req-row {
  display: flex;
  align-items: center;
  gap: var(--aef-space-2);
  font-family: var(--aef-font-body);
  font-size: 10px;
  color: var(--aef-text-secondary);
  transition: color var(--aef-duration-fast) var(--aef-ease-exit);
}
.auth-hint__req-row--met {
  color: var(--aef-status-live);
}
```

#### Plan Selection Cards
```css
.dp-ability-tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--aef-space-2);
  padding: var(--aef-space-4);
  background: var(--aef-surface-low);
  border: var(--aef-border-width) solid var(--aef-border);
  border-radius: var(--aef-radius-card);
  cursor: pointer;
  transition: border-color var(--aef-duration-fast), background var(--aef-duration-fast);
}
.dp-ability-tile--selected {
  border-color: var(--aef-text-primary);
  background: var(--aef-surface-hover);
}
```

### 3.4 Responsive Behavior

- **Desktop (>759px):** Auth card and AuthHint side by side, gap: var(--aef-space-6)
- **Mobile (≤759px):** Stacked vertically, gap: var(--aef-space-4)

---

## 4. API Contract

### 4.1 POST /api/v1/auth/register

**Request:**
```json
{
  "email": "user@example.com",
  "password": "SecureP@ss123",
  "name": "John Doe",
  "tenantName": "Acme Corp",
  "planName": "pro"
}
```

**Success Response (201 Created):**
```json
{
  "accessToken": "eyJhbGciOiJIUzI1NiIs...",
  "expiresAt": "2026-06-18T12:15:00Z",
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "name": "John Doe",
    "role": "admin",
    "tenantId": "uuid",
    "permissions": { "tenants:read": true, "twins:read": true, "twins:write": true }
  },
  "tenant": { "id": "uuid", "name": "Acme Corp", "planName": "pro" },
  "plan": { "planName": "pro", "features": {...}, "limits": {...}, "quotas": {...} }
}
```

**Set-Cookie Header:**
```
paryty_refresh_token=<token>; Path=/api/v1/auth; HttpOnly; Secure; SameSite=Strict
```

**Error Responses:**

| Status | Message | Condition |
|--------|---------|-----------|
| 400 | "email is not a valid address" | Invalid email |
| 400 | "password policy: ..." | Password too weak |
| 400 | "plan \"enterprise\" is not available for signup" | Non-creatable plan |
| 409 | "a user with this email already exists" | Duplicate email |
| 429 | "too many registration attempts" | Rate limit |

### 4.2 GET /api/v1/plans (Public)

Returns array of plans with name, displayName, maxTwins, creatable, features, limits, quotas.

---

## 5. Backend Implementation

### 5.1 Registration Flow (handler.go)

1. Validate email (RFC 5322)
2. Validate password policy (8-72 chars, upper, lower, digit, special)
3. Validate plan name (must be creatable)
4. Hash password (bcrypt, cost=12)
5. Begin transaction → Create tenant → Create admin user → Commit
6. Assign plan (post-commit, idempotent)
7. Generate JWT pair (15min access + 7d refresh)
8. Store refresh token hash
9. Audit log

### 5.2 Password Policy (password.go)

```go
func ValidatePasswordPolicy(password string) error {
  if len(password) < 8 { return fmt.Errorf("at least 8 characters") }
  if len(password) > 72 { return fmt.Errorf("at most 72 characters") }
  // Check uppercase, lowercase, digit, special
}
```

### 5.3 Plan Validation (engine.go)

```go
func (e *PlanEngine) ValidatePlanName(planName string) bool {
  plan, ok := e.config.Plans[planName]
  return ok && plan.Creatable  // Must exist AND be creatable
}
```

---

## 6. Security Requirements

- **Input sanitization:** Email validation, password length enforcement
- **Password hashing:** bcrypt cost=12 (~250ms per hash)
- **Email enumeration prevention:** Timing-attack protection with dummy bcrypt hash
- **Cookie security:** HttpOnly, SameSite=Strict, conditional Secure flag
- **Token security:** HMAC-signed JWT, SHA-256 hashed refresh tokens
- **Rate limiting:** Add to register endpoint (5 per hour per IP)
- **RLS:** Row-Level Security on tenant-scoped tables

---

## 7. Acceptance Criteria

| ID | Requirement | Pass Criteria |
|----|-------------|---------------|
| FR-01 | Navigate to /register | Page loads without errors |
| FR-02 | Plans load on mount | GET /api/v1/plans returns 200 |
| FR-03 | Step 1 validation blocks invalid input | Continue disabled until valid |
| FR-04 | Password strength updates in real-time | 5 checkmarks animate |
| FR-05 | Step 2 shows creatable plans only | Enterprise not displayed |
| FR-06 | Step 3 shows correct review data | All fields match input |
| FR-07 | Registration creates tenant + user | Database has new rows |
| FR-08 | Registration returns JWT tokens | Response contains accessToken |
| FR-09 | Refresh token in httpOnly cookie | Cookie present with flags |
| FR-10 | Access token in memory only | authStore.accessToken set |
| FR-11 | Success toast displayed | "Workspace created!" shown |
| FR-12 | Redirect to dashboard | navigate('/') called |
| FR-13 | Duplicate email returns 409 | Error message shown |
| FR-14 | Invalid password returns 400 | Error message shown |
| FR-15 | Non-creatable plan returns 400 | Error message shown |

---

## 8. Implementation Tasks

### Frontend Tasks

| Task | File | Description |
|------|------|-------------|
| FE-01 | RegisterPage.tsx | Add Terms/Privacy checkbox to Step 3 |
| FE-02 | AuthHint.tsx | Add visual password strength meter |
| FE-03 | RegisterPage.tsx | Add animated spinner to submit button |
| FE-04 | rest.ts | Add CSRF token to requests |

### Backend Tasks

| Task | File | Description |
|------|------|-------------|
| BE-01 | main.go | Add rate limiting to register endpoint |
| BE-02 | middleware.go | Add CSRF token generation/validation |
| BE-03 | handler.go | Add email verification flow (future) |

---

## 9. Testing Strategy

### Unit Tests (Vitest)
- RegisterPage renders step 1 by default
- Validates email format
- Validates password policy
- Shows error on duplicate email (409)
- Navigates to / on success

### Unit Tests (Go)
- TestRegister_Success: Creates tenant + user
- TestRegister_DuplicateEmail: Returns 409
- TestRegister_InvalidPassword: Returns 400
- TestRegister_NonCreatablePlan: Returns 400
- TestValidatePasswordPolicy: All edge cases

### Integration Tests
- Full HTTP registration flow with database verification
- httpOnly cookie verification
- Plan assignment verification

### E2E Tests (Playwright)
- Complete registration flow (3 steps → dashboard)
- Duplicate email error handling
- Password strength real-time feedback
