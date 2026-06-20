# Step 2: Login & Session Persistence — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### 1.1 What Exists

| Component | File | Status |
|-----------|------|--------|
| LoginPage | `frontend/src/pages/LoginPage.tsx` | Clean login form with email/password |
| authStore | `frontend/src/stores/authStore.ts` | login(), refreshAuth(), initAuth() |
| AuthProvider | `frontend/src/components/auth/AuthProvider.tsx` | Wraps app with auth context |
| AuthRESTAdapter | `cluster/internal/auth/auth_rest_adapter.go` | Login, Refresh, Logout handlers |
| AuthHandler | `cluster/internal/auth/handler.go` | Login with brute-force protection |
| JWT Manager | `cluster/internal/auth/jwt.go` | HS256 JWT, token rotation, reuse detection |
| Middleware | `cluster/internal/auth/middleware.go` | GinJWTAuth, GinJWTAuthFlexible, **GinOptionalAuth** (public endpoints with optional personalization, lines 139-173) |
| gRPC Interceptor | `cluster/internal/auth/middleware.go` | GrpcJWTAuth unary interceptor for gRPC services (lines 182-214) |

### 1.2 What Works

1. Login form with email/password, password visibility toggle
2. Error handling for 401 (invalid credentials) and 429 (rate limiting)
3. Brute-force protection: 5 failed attempts per 15 min, 15 min block
4. Timing-attack prevention: Dummy bcrypt hash for non-existent users
5. Token rotation: One-time use refresh tokens, reuse detection
6. Session persistence: Recently fixed — cookie Secure flag conditional on PARYTY_DEV_MODE
7. Silent refresh on app mount via initAuth() → refreshAuth()
8. Flexible auth middleware for WebSocket/SSE (header → cookie → query param)
9. **Account deactivation check:** Deactivated accounts (`isActive` field) get "account is deactivated" error (PermissionDenied) at handler.go:293-295
10. **Dual httpOnly cookies:** Both `paryty_access_token` (Path=`/`) and `paryty_refresh_token` (Path=`/api/v1/auth`) set as httpOnly cookies. Access token cookie enables WebSocket/SSE auth.
11. **refreshToken removed from response body:** Response initially contains refreshToken but it's deleted at auth_rest_adapter.go:223 — only cookie carries it.
12. **SSE token getter wiring:** AuthProvider.tsx:62-63 calls `setSseTokenGetter()` for EventSource authentication (EventSource can't set headers, uses module-level getter pattern)
13. **Opportunistic token cleanup:** Expired/revoked tokens cleaned up on each refresh (handler.go:566-573)
14. **Token reuse detection:** If a revoked token is reused, ALL user sessions are revoked. Warning logged, specific error "refresh token has been returned" (handler.go:381-393)

### 1.3 What's Missing

| Issue | Severity | Description |
|-------|----------|-------------|
| No "Remember Me" option | Low | Refresh token always 7 days |
| No session timeout warning | Medium | Users not warned before expiry |
| No multi-factor authentication | Low | Only password-based |
| No password reset flow | High | Users cannot recover forgotten passwords |
| No account lockout notification | Low | No notification of failed attempts |
| Stale tests | Medium | Tests reference localStorage but code uses httpOnly cookies |
| No Concurrent Refresh Handling | Medium | Multiple tabs refreshing simultaneously not addressed — race condition possible |
| No Network Error Retry Logic | Medium | Frontend retry strategy for failed refresh attempts not specified |
| No Disaster Recovery | Low | Backup strategies for auth data, token rotation during recovery not documented |

---

## 2. Target State Specification

### 2.1 User Journey

1. User navigates to `/login`
2. PublicRoute checks → if authenticated, redirect to `/`
3. LoginPage renders: Email field, Password field with toggle, Login button, Error banner
4. User fills form → clicks Login
5. POST /api/v1/auth/login → Backend validates → returns JWT pair
6. Frontend stores access token in memory → sets httpOnly cookie for refresh
7. Redirect to `/` → Dashboard loads
8. **Session persistence:** F5 refresh → initAuth() → refreshAuth() → cookie sent → new token pair → stay logged in
9. **Token refresh:** Access token expires (15min) → silent refresh → new pair
10. **Session warning:** 2 minutes before expiry → modal warns user
11. **Logout:** POST /api/v1/auth/logout → revoke refresh token → clear memory → redirect to `/login`

### 2.2 Session Lifecycle

```
ACCESS TOKEN (JWT)
- Location: In-memory (Zustand authStore)
- Lifetime: 15 minutes
- Payload: { sub, tenant_id, plan, role, permissions, exp, iat }
- Usage: Authorization header (Bearer <token>)
- Refresh: Silent refresh via httpOnly cookie

REFRESH TOKEN
- Location: httpOnly cookie (paryty_refresh_token)
- Lifetime: 7 days
- Storage: Hashed in refresh_tokens table
- Rotation: One-time use (old token revoked on refresh)
- Reuse detection: If revoked token reused, revoke all user sessions
```

### 2.3 Proactive Token Refresh

```typescript
// In AuthProvider or authStore
const scheduleRefresh = (expiresAt: number) => {
  const refreshAt = expiresAt - 120_000; // 2 minutes before expiry
  const delay = Math.max(0, refreshAt - Date.now());
  setTimeout(() => refreshAuth(), delay);
};
```

### 2.4 Session Timeout Warning

When access token is about to expire (2 minutes):
- Show modal: "Your session is about to expire. Would you like to stay logged in?"
- "Stay Logged In" button → refreshAuth()
- "Log Out" button → logout()
- Auto-logout if no action within 2 minutes

---

## 3. UI Component Specification

### 3.1 Login Page Layout

```tsx
<div className="auth-page">
  <div className="aef-container-card auth-card">
    <div className="aef-container-card__header">
      <Shield size={14} />
      <span>Paryty</span>
    </div>
    <div className="aef-container-card__body">
      <h1 className="auth-card__title">Welcome back</h1>
      <p className="auth-card__subtitle">Sign in to your workspace</p>
      
      {error && (
        <div className="auth-form__error" role="alert">{error}</div>
      )}
      
      <form className="auth-form">
        <div className="dp-field">
          <label className="dp-field__label">Email</label>
          <input className="dp-field__input" type="email" />
        </div>
        <div className="dp-field">
          <label className="dp-field__label">Password</label>
          <input className="dp-field__input" type="password" />
          {/* Visibility toggle */}
        </div>
        <button className="aef-btn aef-btn-active" disabled={isLoading}>
          {isLoading ? 'Signing in…' : 'Sign In'}
        </button>
      </form>
    </div>
    <div className="aef-container-card__body">
      <p>Don't have an account? <Link to="/register">Create one</Link></p>
    </div>
  </div>
  <AuthHint /> {/* Contextual help */}
</div>
```

### 3.2 Token Usage

- Page background: `var(--aef-bg)`
- Card: `var(--aef-surface-card)`, `var(--aef-radius-card)`
- Input: `var(--aef-surface-low)`, `var(--aef-border)`, `var(--aef-radius-field)`
- Focus: `var(--aef-border-strong)`
- Text: `var(--aef-text-primary)`, `var(--aef-text-secondary)`
- Error: `var(--aef-counter-variant-b)` or `var(--aef-status-error)`
- Button: `var(--aef-btn-active-bg)`, `var(--aef-btn-active-text)`
- Font: `var(--aef-font-heading)` for titles, `var(--aef-font-body)` for body

### 3.3 Error States

| Error | Display |
|-------|---------|
| Invalid credentials | "Invalid email or password." (red banner) |
| Rate limited | "Too many attempts. Please try again in X minutes." |
| Network error | "Unable to connect. Please check your connection." |
| Server error | "Something went wrong. Please try again." |

### 3.4 Session Timeout Warning Modal

```tsx
<div className="aef-modal-overlay">
  <div className="aef-modal" style={{ maxWidth: 420 }}>
    <div className="aef-modal-header">
      <span>Session Expiring</span>
    </div>
    <div className="aef-modal-body">
      <p>Your session will expire in {countdown} seconds.</p>
      <p>Would you like to stay logged in?</p>
    </div>
    <div className="aef-modal-footer">
      <button className="aef-btn aef-btn-inactive" onClick={handleLogout}>
        Log Out
      </button>
      <button className="aef-btn aef-btn-active" onClick={handleRefresh}>
        Stay Logged In
      </button>
    </div>
  </div>
</div>
```

---

## 4. API Contract

### 4.1 POST /api/v1/auth/login

**Request:**
```json
{ "email": "user@example.com", "password": "SecureP@ss123" }
```

**Success (200):**
```json
{
  "accessToken": "eyJ...",
  "expiresAt": "2026-06-18T12:15:00Z",
  "user": { "id": "uuid", "email": "...", "name": "...", "role": "admin", "permissions": {...} },
  "tenant": { "id": "uuid", "name": "...", "planName": "pro" },
  "plan": { "planName": "pro", "features": {...}, "limits": {...} }
}
```

**Set-Cookie:** `paryty_refresh_token=<token>; Path=/api/v1/auth; HttpOnly; Secure; SameSite=Strict`

### 4.2 POST /api/v1/auth/refresh

**Request:** Cookie: `paryty_refresh_token=<token>`

**Success (200):** Same shape as login response with new tokens.

**Set-Cookie:** New refresh token cookie (rotation).

### 4.3 POST /api/v1/auth/logout

**Request:** Cookie: `paryty_refresh_token=<token>`

**Success (200):** `{ "message": "logged out" }`

**Set-Cookie:** `paryty_refresh_token=; Max-Age=0` (clear cookie)

---

## 5. Security

- **Brute-force:** 5 failed attempts/15min → 15min block per email
- **Timing attack:** Dummy bcrypt hash for non-existent users
- **Cookie:** HttpOnly, SameSite=Strict, conditional Secure (PARYTY_DEV_MODE)
- **Token rotation:** One-time use refresh tokens
- **Reuse detection:** Revoked token reuse → revoke all user sessions
- **Password hashing:** bcrypt cost=12
- **JWT:** HS256, 15min access, 7d refresh

---

## 6. Acceptance Criteria

| ID | Requirement | Pass Criteria |
|----|-------------|---------------|
| FR-01 | Login with valid credentials | Redirect to `/`, token in memory |
| FR-02 | Login with invalid credentials | Error message shown |
| FR-03 | Session persists on F5 refresh | Stay logged in after refresh |
| FR-04 | Session survives tab close/reopen | Stay logged in within 7 days |
| FR-05 | Token refresh works | New token pair generated silently |
| FR-06 | Rate limiting blocks brute force | 5 failures → 15min block |
| FR-07 | Logout clears session | Redirect to `/login` |
| FR-08 | Session timeout warning | Modal appears 2min before expiry |

---

## 7. Implementation Tasks

### Backend Tasks

| Task | File | Description |
|------|------|-------------|
| B-01 | auth_rest_adapter.go | Add ChangePassword endpoint |
| B-02 | main.go | Register change-password route |
| B-03 | handler.go | Add password reset token generation |
| B-04 | handler.go | Add password reset verification |

### Frontend Tasks

| Task | File | Description |
|------|------|-------------|
| F-01 | authStore.ts | Add proactive token refresh scheduling |
| F-02 | AuthProvider.tsx | Add session timeout warning modal |
| F-03 | LoginPage.tsx | Add "Forgot password?" link |
| F-04 | authStore.ts | Add changePassword action |
| F-05 | SettingsPage.tsx | Wire change password form to API |
| F-06 | authStore.test.ts | Fix stale localStorage references |
