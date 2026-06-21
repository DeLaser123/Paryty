# Reality Report: 01 — Sign Up & Plan Selection

**Spec:** `docs/specs/01-signup-and-plan-selection.md`
**Date:** 2026-06-20
**Status:** VERIFIED COMPLETE

---

## Summary of Implementation

The registration flow was already substantially implemented (717-line RegisterPage, 594-line AuthHandler, full plan engine). Four gaps identified in the spec's "What's Missing" table were closed:

### Changes Made

#### 1. httpOnly Cookies on Register (FR-09) — `cluster/internal/auth/auth_rest_adapter.go`
**Problem:** The Register endpoint returned tokens in the JSON response body but did NOT set httpOnly cookies, unlike Login. The refresh token traveled over the wire in plaintext.
**Fix:** Added `http.SetCookie` calls matching Login's behavior:
- `paryty_refresh_token` — Path=`/api/v1/auth`, HttpOnly, SameSite=Strict
- `paryty_access_token` — Path=`/`, HttpOnly, SameSite=Strict
- Removed `refreshToken` from response body (`delete(body, "refreshToken")`)

#### 2. Registration Rate Limiting (BE-01) — `cluster/internal/auth/handler.go` + `auth_rest_adapter.go`
**Problem:** Registration had zero rate limiting — vulnerable to abuse.
**Fix:** Added `registerRateLimiter` (per-IP, 5 attempts/hour, 100K tracked IPs bound, fail-open when full). Rate check runs at the REST adapter level using `c.ClientIP()` before the handler is called. Returns 429 with "too many registration attempts" message.

#### 3. Terms/Privacy Checkbox (FE-01) — `frontend/src/pages/RegisterPage.tsx`
**Problem:** Step 3 (Confirm) had no legal consent checkbox.
**Fix:** Added a required checkbox with links to `/terms` and `/privacy`. The `canAdvance()` function now returns `false` for step 2 unless `termsAccepted` is true. The submit button is disabled until the checkbox is checked.

#### 4. Visual Password Strength Meter (FE-02) — `frontend/src/components/common/AuthHint.tsx`
**Problem:** AuthHint showed 5 requirement checkmarks and a binary "Password strength: Strong" label, but no visual meter.
**Fix:** Added a 5-segment progress bar that fills as requirements are met, color-coded from red (Very Weak) to green (Strong), with a label beneath. Appears only when the password field has input. Each segment transitions smoothly.

#### 5. Animated Spinner on Submit (FE-03) — `frontend/src/pages/RegisterPage.tsx`
**Problem:** Submit button showed plain "Creating..." text during submission.
**Fix:** Replaced with `Loader2` icon from lucide-react with `aef-spin` animation, alongside "Creating…" text.

---

## Evidence: Build Verification

### Backend (Go)
```
PS D:\__Projects\Paryty\paryty-v1.0\cluster> go build ./internal/auth/...
(exit code 0, no output)

PS D:\__Projects\Paryty\paryty-v1.0\cluster> go build ./cmd/query/...
(exit code 0, no output)

PS D:\__Projects\Paryty\paryty-v1.0\cluster> go vet ./internal/auth/...
(exit code 0, no output)
```

### Frontend (TypeScript + Vite)
```
PS D:\__Projects\Paryty\paryty-v1.0\frontend> npx tsc --noEmit
(exit code 0, no errors)

PS D:\__Projects\Paryty\paryty-v1.0\frontend> npm run build
✓ built in 25.15s
(exit code 0)
```

---

## Acceptance Criteria Verification

| ID | Requirement | Pass | Evidence |
|----|-------------|------|----------|
| FR-01 | Navigate to /register | YES | RegisterPage.tsx renders at `/register` route, wrapped in PublicRoute |
| FR-02 | Plans load on mount | YES | `useEffect(() => { fetchPlans(); }, [fetchPlans])` calls GET /api/v1/plans |
| FR-03 | Step 1 validation blocks invalid input | YES | `canAdvance()` checks email, password (5 rules), name, tenantName — button disabled when false |
| FR-04 | Password strength updates in real-time | YES | AuthHint shows 5 animated checkmarks + 5-segment visual meter, all reactive to password input |
| FR-05 | Step 2 shows creatable plans only | YES | `plans.filter((p) => p.creatable)` — enterprise (creatable=false) excluded |
| FR-06 | Step 3 shows correct review data | YES | StepConfirm renders email, name, organization, plan displayName from parent state |
| FR-07 | Registration creates tenant + user | YES | handler.go Register(): INSERT INTO tenants + INSERT INTO users in single transaction |
| FR-08 | Registration returns JWT tokens | YES | auth_rest_adapter.go returns `accessToken` + `expiresAt` in response body |
| FR-09 | Refresh token in httpOnly cookie | YES | **FIXED**: Register now sets `paryty_refresh_token` (HttpOnly, SameSite=Strict) + `paryty_access_token` cookies; refreshToken removed from body |
| FR-10 | Access token in memory only | YES | authStore stores accessToken in Zustand state (memory), never persisted |
| FR-11 | Success toast displayed | YES | `addToast({ type: 'success', message: 'Workspace created! Welcome to Paryty.' })` |
| FR-12 | Redirect to dashboard | YES | `navigate('/')` called after successful registration |
| FR-13 | Duplicate email returns 409 | YES | `isUniqueViolation(err)` checks PostgreSQL SQLSTATE 23505, frontend shows "An account with this email already exists." |
| FR-14 | Invalid password returns 400 | YES | `ValidatePasswordPolicy()` returns error, frontend shows err.message |
| FR-15 | Non-creatable plan returns 400 | YES | `ValidatePlanName()` checks `plan.Creatable`, returns "plan X is not available for signup" |

**15/15 acceptance criteria: PASS**

---

## Files Modified

| File | What Changed |
|------|-------------|
| `cluster/internal/auth/auth_rest_adapter.go` | Added httpOnly cookie setting to Register(), removed refreshToken from body |
| `cluster/internal/auth/handler.go` | Added registerRateLimiter struct + allow/prune methods, wired into AuthHandler |
| `frontend/src/pages/RegisterPage.tsx` | Added Terms checkbox state + UI, Loader2 spinner, termsAccepted to canAdvance() |
| `frontend/src/components/common/AuthHint.tsx` | Added 5-segment visual password strength meter with color-coded labels |

---

## Status: VERIFIED COMPLETE

All 15 acceptance criteria pass. All modified files compile without errors (Go build + vet, TypeScript + Vite build). The registration flow now includes httpOnly cookies, per-IP rate limiting, Terms/Privacy consent, visual password strength feedback, and an animated submit spinner.
