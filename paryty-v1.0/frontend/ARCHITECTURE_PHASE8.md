# Paryty Phase 8 — Frontend Architecture

## Table of Contents
1. [Route Architecture](#1-route-architecture)
2. [Auth Flow](#2-auth-flow)
3. [New Stores](#3-new-stores)
4. [New Types](#4-new-types)
5. [RestClient Auth Hardening](#5-restclient-auth-hardening)
6. [WebSocket Auth Hardening](#6-websocket-auth-hardening)
7. [Component Changes](#7-component-changes)
8. [New Pages — Detailed Specs](#8-new-pages--detailed-specs)
9. [Plan-Aware Feature Gating](#9-plan-aware-feature-gating)
10. [UX Specifications](#10-ux-specifications)
11. [File Tree — Phase 8 Deliverables](#11-file-tree--phase-8-deliverables)
12. [Implementation Order](#12-implementation-order)

---

## 1. Route Architecture

### Updated `App.tsx` Route Table

| Path | Component | Auth | Plan Gate | Notes |
|---|---|---|---|---|
| `/login` | `LoginPage` | **No** (public) | — | Redirects to `/` if already authed |
| `/register` | `RegisterPage` | **No** (public) | — | Multi-step: Account → Plan → Confirm |
| `/` | `DashboardPage` | **Yes** | Basic+ | Existing; enhanced with plan limits |
| `/topology` | `TopologyCanvas` | **Yes** | Basic+ | Existing lazy route |
| `/metrics` | `MetricsView` | **Yes** | Basic+ | Existing lazy route |
| `/alerts` | `AlertView` | **Yes** | Basic+ | Existing lazy route |
| `/timeline` | `TimelineView` | **Yes** | Pro+ | NEW route (was 404) |
| `/intel` | `IntelView` | **Yes** | Pro+ | Existing lazy route; gated |
| `/settings` | `SettingsPage` | **Yes** | Basic+ | NEW route (was 404) |
| `/twins/new` | `TwinCreatePage` | **Yes** | Basic+ | NEW; enhanced wizard |
| `/twins/:id` | `TwinDetailPage` | **Yes** | Basic+ | NEW; twin dashboard |
| `/twins/:id/settings` | `TwinSettingsPage` | **Yes** | Basic+ | NEW; ability/per-agent config |

### Route Configuration (App.tsx)

```tsx
// App.tsx — Phase 8
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AuthProvider } from './components/auth/AuthProvider';
import { ProtectedRoute } from './components/auth/ProtectedRoute';
import { PlanGate } from './components/auth/PlanGate';
import { AppShell } from './components/layout/AppShell';

// Public pages (no AppShell chrome)
const LoginPage = lazy(() => import('./pages/LoginPage'));
const RegisterPage = lazy(() => import('./pages/RegisterPage'));

// Existing lazy routes
const TopologyCanvas = lazy(/* ... */);
const MetricsView = lazy(/* ... */);
const AlertView = lazy(/* ... */);
const TimelineView = lazy(/* ... */);   // was missing
const IntelView = lazy(/* ... */);

// New lazy routes
const SettingsPage = lazy(() => import('./pages/SettingsPage'));
const TwinCreatePage = lazy(() => import('./pages/TwinCreatePage'));
const TwinDetailPage = lazy(() => import('./pages/TwinDetailPage'));
const TwinSettingsPage = lazy(() => import('./pages/TwinSettingsPage'));

function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          {/* Public */}
          <Route path="/login"    element={<PublicRoute><LoginPage /></PublicRoute>} />
          <Route path="/register" element={<PublicRoute><RegisterPage /></PublicRoute>} />

          {/* Protected — wrapped in AppShell */}
          <Route element={<ProtectedRoute><AppShell><Outlet /></AppShell></ProtectedRoute>}>
            <Route index                          element={<DashboardPage />} />
            <Route path="topology"                element={<TopologyCanvas />} />
            <Route path="metrics"                 element={<MetricsView />} />
            <Route path="alerts"                  element={<AlertView />} />
            <Route path="timeline"                element={<PlanGate required="pro"><TimelineView /></PlanGate>} />
            <Route path="intel"                   element={<PlanGate required="pro_plus"><IntelView /></PlanGate>} />
            <Route path="settings"                element={<SettingsPage />} />
            <Route path="twins/new"               element={<TwinCreatePage />} />
            <Route path="twins/:twinId"           element={<TwinDetailPage />} />
            <Route path="twins/:twinId/settings"  element={<TwinSettingsPage />} />
          </Route>

          {/* Catch-all */}
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  );
}
```

---

## 2. Auth Flow

### 2.1 Token Storage Strategy

| Token | Storage | Rationale |
|---|---|---|
| `access_token` (JWT) | **In-memory** (Zustand `authStore`) | Prevents XSS extraction; survives SPA navigation but not full reload. |
| `refresh_token` | **httpOnly cookie** (set by backend) OR **localStorage** (fallback if backend can't set cookies) | HttpOnly cookie is preferred; localStorage fallback with strict CSP. |
| `user` object | In-memory `authStore` | Never serialized to storage. |
| `tenant` object | In-memory `authStore` | Never serialized to storage. |

**Refresh flow on page reload:**
1. `AuthProvider` mounts → call `POST /api/v1/auth/refresh` with the httpOnly cookie
2. If cookie is valid → receive new `access_token` + user/tenant → hydrate store
3. If cookie is invalid/missing → redirect to `/login`

### 2.2 AuthContext / AuthProvider

**File:** `src/components/auth/AuthProvider.tsx`

```
AuthProvider
├── On mount: calls refreshToken()
├── Renders <AuthGuard> which handles three states:
│   ├── "loading"   → Full-screen skeleton (brand logo + spinner)
│   ├── "anonymous" → <LoginPage> or <RegisterPage> (public routes)
│   └── "authenticated" → children (AppShell + protected routes)
└── Sets up 401 interceptor on RestClient
```

**Three render states:**

```
┌──────────────────────────────────────────────────┐
│                                                  │
│              [Paryty Logo]                       │
│                                                  │
│              ═══════════════                     │  ← Loading state
│              Authenticating…                     │     (skeleton pulse)
│                                                  │
└──────────────────────────────────────────────────┘
```

### 2.3 Auth Store Shape

**File:** `src/stores/authStore.ts`

```typescript
interface AuthState {
  // ── State ──
  status: 'loading' | 'anonymous' | 'authenticated';
  user: User | null;
  tenant: Tenant | null;
  accessToken: string | null;
  refreshToken: string | null;   // only if localStorage fallback

  // ── Computed (via getters) ──
  isAuthenticated: () => boolean;
  isAdmin: () => boolean;

  // ── Actions ──
  login: (email: string, password: string) => Promise<void>;
  register: (params: RegisterParams) => Promise<void>;
  logout: () => Promise<void>;
  refreshToken: () => Promise<void>;
  setAuth: (user: User, tenant: Tenant, accessToken: string, refreshToken?: string) => void;
  clearAuth: () => void;

  // ── Token timer ──
  _scheduleRefresh: (expiresInSeconds: number) => void;
  _cancelRefresh: () => void;
}
```

### 2.4 JWT Refresh Timing

```
Access token lifetime: 15 minutes
Refresh trigger: 2 minutes before expiry (at 13 min mark)
Implementation: setTimeout in authStore._scheduleRefresh()

Token is NEVER decoded client-side (no jwt-decode dependency).
Expiry is passed by the backend in the login/refresh response body:
  { access_token, expires_in: 900, refresh_token }
```

### 2.5 ProtectedRoute Component

**File:** `src/components/auth/ProtectedRoute.tsx`

```typescript
function ProtectedRoute({ children }: { children: ReactNode }) {
  const status = useAuthStore((s) => s.status);

  if (status === 'loading') {
    return <AuthLoadingSkeleton />;
  }

  if (status === 'anonymous') {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }

  return <>{children}</>;
}
```

The `state={{ from }}` preserves the intended destination so LoginPage can redirect back after successful login.

### 2.6 PublicRoute Component (Redirects if Already Authed)

**File:** `src/components/auth/PublicRoute.tsx`

```typescript
function PublicRoute({ children }: { children: ReactNode }) {
  const status = useAuthStore((s) => s.status);
  const location = useLocation();

  if (status === 'authenticated') {
    const from = (location.state as { from?: string })?.from ?? '/';
    return <Navigate to={from} replace />;
  }

  return <>{children}</>;
}
```

### 2.7 PlanGate Component

**File:** `src/components/auth/PlanGate.tsx`

```typescript
type PlanTier = 'basic' | 'pro' | 'pro_plus';

function PlanGate({ required, children, fallback }: {
  required: PlanTier;
  children: ReactNode;
  fallback?: ReactNode;
}) {
  const plan = usePlanStore((s) => s.currentPlan);

  if (!plan) return null; // still loading

  const tiers: PlanTier[] = ['basic', 'pro', 'pro_plus'];
  const currentIdx = tiers.indexOf(plan.tier);
  const requiredIdx = tiers.indexOf(required);

  if (currentIdx < requiredIdx) {
    return fallback ?? <UpgradePrompt required={required} current={plan.tier} />;
  }

  return <>{children}</>;
}
```

### 2.8 401 Interceptor on RestClient

The existing `RestClient.request()` method will be extended with a 401 handler:

```typescript
// In rest.ts — after the !response.ok check:
if (response.status === 401 && !path.includes('/auth/')) {
  // Attempt silent refresh
  const refreshed = await this.authRefreshCallback?.();
  if (refreshed) {
    // Retry the original request with new token
    return this.request<T>(method, path, body, params, options);
  }
  // Refresh failed — redirect to login
  useAuthStore.getState().clearAuth();
  throw new ApiClientError('Session expired', 401, 'UNAUTHORIZED');
}

// Add Authorization header:
headers: {
  'Content-Type': 'application/json',
  'Accept': 'application/json',
  ...(this.getAuthHeader() ? { 'Authorization': this.getAuthHeader()! } : {}),
}
```

The `authRefreshCallback` is a function injected by `AuthProvider` at mount time, maintaining separation of concerns (RestClient doesn't import authStore directly — it receives a callback).

---

## 3. New Stores

### 3.1 authStore

**File:** `src/stores/authStore.ts`  
**Persistence:** None (security requirement — tokens never touch localStorage except for refresh_token fallback)  
**Pattern:** Standard Zustand (no `persist` middleware)

See §2.3 for the full interface.

### 3.2 planStore

**File:** `src/stores/planStore.ts`

```typescript
interface PlanState {
  // ── State ──
  availablePlans: Plan[];
  currentPlan: CurrentPlan | null;
  isLoading: boolean;
  error: string | null;

  // ── Actions ──
  fetchPlans: () => Promise<void>;
  fetchCurrentPlan: () => Promise<void>;

  // ── Computed helpers (callable selectors) ──
  hasFeature: (feature: FeatureFlag) => boolean;
  usageFor: (resource: ResourceType) => UsageInfo | undefined;
  isNearLimit: (resource: ResourceType) => boolean;
}

// ── Types ──

interface Plan {
  id: string;
  name: string;               // "Basic", "Pro", "Pro+"
  tier: 'basic' | 'pro' | 'pro_plus';
  priceMonthly: number;
  limits: Record<ResourceType, number>;
  features: FeatureFlag[];
}

type ResourceType = 'twins' | 'agents' | 'users' | 'dataRetentionDays';

type FeatureFlag =
  | 'topology'
  | 'metrics'
  | 'alerts'
  | 'timeline_replay'
  | 'forecasting'
  | 'watif_drills'
  | 'custom_dashboards'
  | 'api_keys'
  | 'sso';

interface CurrentPlan {
  planId: string;
  tier: 'basic' | 'pro' | 'pro_plus';
  features: FeatureFlag[];
  usage: Record<ResourceType, { used: number; limit: number }>;
  billingPeriodStart: string;
  billingPeriodEnd: string;
}

interface UsageInfo {
  used: number;
  limit: number;
}
```

**Fetch strategy:** `fetchCurrentPlan()` is called once after login and cached until logout. Plan limits and feature flags are the source of truth for all gating decisions.

---

## 4. New Types

**File:** `src/types/auth.ts`

```typescript
export interface User {
  id: string;
  email: string;
  name: string;
  role: 'admin' | 'member' | 'viewer';
  avatarUrl?: string;
  createdAt: string;
}

export interface Tenant {
  id: string;
  name: string;           // e.g. "gai-tech"
  displayName: string;    // e.g. "Gai Tech Inc."
  createdAt: string;
}

export interface RegisterParams {
  email: string;
  password: string;
  name: string;
  planId: string;          // selected plan from plan selection step
}

export interface LoginResponse {
  access_token: string;
  expires_in: number;      // seconds
  refresh_token?: string;  // only if localStorage fallback
  user: User;
  tenant: Tenant;
}

export interface AuthTokens {
  accessToken: string;
  refreshToken?: string;
}

// ── User Management ──

export interface SubUser {
  id: string;
  email: string;
  name: string;
  role: 'admin' | 'member' | 'viewer';
  status: 'active' | 'invited' | 'disabled';
  createdAt: string;
  lastLoginAt?: string;
}

export interface CreateSubUserParams {
  email: string;
  name: string;
  role: 'admin' | 'member' | 'viewer';
}
```

**File:** `src/types/apiKeys.ts`

```typescript
export interface ApiKey {
  id: string;
  name: string;
  prefix: string;          // First 8 chars for display (e.g. "pk_live_AbCd...")
  createdAt: string;
  lastUsedAt?: string;
  expiresAt?: string;
}

export interface CreateApiKeyResponse {
  id: string;
  name: string;
  key: string;             // Full key — shown ONCE
  prefix: string;
  createdAt: string;
}
```

Add exports to `src/types/index.ts`:
```typescript
export * from './auth';
export * from './apiKeys';
```

---

## 5. RestClient Auth Hardening

### Changes to `src/api/rest.ts`

**Add auth token management:**

```typescript
export class RestClient {
  // ... existing fields
  private authRefreshCallback: (() => Promise<boolean>) | null = null;

  setAuthRefreshCallback(cb: () => Promise<boolean>): void {
    this.authRefreshCallback = cb;
  }

  private getAuthHeader(): string | null {
    // Read token from authStore without importing it directly
    // Use a getter function injected by AuthProvider
    return this._tokenGetter?.() ?? null;
  }

  private _tokenGetter: (() => string | null) | null = null;

  setTokenGetter(getter: () => string | null): void {
    this._tokenGetter = getter;
  }
}

// In request() method — add Authorization header:
headers: {
  'Content-Type': 'application/json',
  'Accept': 'application/json',
  ...(this.getAuthHeader()
    ? { 'Authorization': `Bearer ${this.getAuthHeader()}` }
    : {}),
},

// In request() — 401 handling (after !response.ok):
if (response.status === 401 && !path.startsWith('/api/v1/auth/')) {
  const refreshed = await this.authRefreshCallback?.();
  if (refreshed) {
    return this.request<T>(method, path, body, params, options);
  }
  throw new ApiClientError('Session expired', 401, 'UNAUTHORIZED');
}
```

**Cache clearance on logout:** Call `restClient.clearCache()` when `authStore.clearAuth()` is invoked.

---

## 6. WebSocket Auth Hardening

### Changes to `src/api/websocket.ts`

**Add token passing:**

```typescript
export class WebSocketClient {
  private _tokenGetter: (() => string | null) | null = null;

  setTokenGetter(getter: () => string | null): void {
    this._tokenGetter = getter;
  }

  connect(): void {
    // Build URL with token query param
    const token = this._tokenGetter?.();
    let url = this.url;
    if (token) {
      const sep = url.includes('?') ? '&' : '?';
      url = `${url}${sep}token=${encodeURIComponent(token)}`;
    }
    // ... rest of connect logic
  }

  // On reconnect — re-read token (it may have been refreshed)
  private scheduleReconnect(): void {
    // Before calling connect, the token will be re-read
    super.scheduleReconnect();
  }
}
```

**Disconnect on logout:** The `useWebSocket` hook (or `authStore.clearAuth`) must call `wsClient.disconnect()` on logout.

---

## 7. Component Changes

### 7.1 AppShell → Add UserMenu

**Changes to:** `src/components/layout/AppShell.tsx`

The AppShell wraps differently: public pages (login/register) render WITHOUT AppShell chrome; protected pages render WITH it. This is handled by the route structure (public routes are outside the `<AppShell>` wrapper in App.tsx).

**Changes to:** `src/components/layout/Header.tsx`

Replace the hardcoded tenant badge:
```tsx
// BEFORE:
<span className="app-topbar__tenant-badge">gai-tech</span>

// AFTER:
import { useAuthStore } from '../../stores/authStore';
import { usePlanStore } from '../../stores/planStore';

// In Header():
const tenant = useAuthStore((s) => s.tenant);
const planTier = usePlanStore((s) => s.currentPlan?.tier);

// Tenant badge + plan pill
<span className="app-topbar__tenant-badge">
  {tenant?.displayName ?? tenant?.name ?? '—'}
</span>
{planTier && (
  <span className={`app-topbar__plan-pill app-topbar__plan-pill--${planTier}`}>
    {planTier === 'pro_plus' ? 'Pro+' : planTier === 'pro' ? 'Pro' : 'Basic'}
  </span>
)}
```

Add UserMenu (right side of header, before alert bell):
```tsx
<div className="app-topbar__user-menu" ref={userMenuRef}>
  <button
    className="app-topbar__user-trigger"
    onClick={() => setUserMenuOpen(o => !o)}
    data-testid="topbar-user-menu"
  >
    <div className="app-topbar__user-avatar">
      {user?.name?.charAt(0).toUpperCase() ?? '?'}
    </div>
  </button>
  {userMenuOpen && (
    <div className="aef-dropdown-menu" style={{ right: 0, minWidth: 200 }}>
      <div className="aef-dropdown-section">
        <span className="aef-dropdown-section__label">{user?.name}</span>
        <span className="aef-dropdown-section__sub">{user?.email}</span>
      </div>
      <div className="aef-dropdown-divider" />
      <Link to="/settings" className="aef-dropdown-item" onClick={close}>
        <Settings size={14} /> Settings
      </Link>
      <button className="aef-dropdown-item" onClick={handleLogout}>
        <LogOut size={14} /> Sign out
      </button>
    </div>
  )}
</div>
```

### 7.2 Sidebar → Plan-Aware Items

**Changes to:** `src/components/layout/Sidebar.tsx`

Replace static `NAV_ITEMS` with a hook-derived list:

```typescript
function useNavItems(): NavItem[] {
  const hasFeature = usePlanStore((s) => s.hasFeature);

  const allItems: (NavItem & { feature?: FeatureFlag })[] = [
    { label: 'Dashboard', icon: <LayoutDashboard size={16} />, path: '/' },
    { label: 'Topology',  icon: <Globe size={16} />, path: '/topology', feature: 'topology' },
    { label: 'Metrics',   icon: <BarChart2 size={16} />, path: '/metrics', feature: 'metrics' },
    { label: 'Timeline',  icon: <Activity size={16} />, path: '/timeline', feature: 'timeline_replay' },
    { label: 'Alerts',    icon: <Bell size={16} />, path: '/alerts', feature: 'alerts' },
    { label: 'Paryty-Intel', icon: <Brain size={16} />, path: '/intel', feature: 'forecasting' },
  ];

  return allItems.filter(item => !item.feature || hasFeature(item.feature));
}
```

Update the bottom section — profile is now fed from authStore:
```tsx
const user = useAuthStore((s) => s.user);
const tenant = useAuthStore((s) => s.tenant);

<div className="ds-sidebar__profile-info">
  <span className="ds-sidebar__profile-name">{user?.name ?? 'Operator'}</span>
  <span className="ds-sidebar__profile-role">
    {user?.role === 'admin' ? 'Tenant Admin' : user?.role ?? 'Member'}
  </span>
</div>
```

### 7.3 DashboardPage → Plan Limits + Upgrade CTA

**Changes to:** `src/components/dashboard/DashboardPage.tsx`

Add a plan usage banner between the counter row and the catalogue:

```tsx
function PlanUsageBanner() {
  const usage = usePlanStore((s) => s.currentPlan?.usage);
  const isNearLimit = usePlanStore((s) => s.isNearLimit);

  if (!usage) return null;

  const twinUsage = usage.twins;

  return (
    <div className="dp-usage-banner">
      <div className="dp-usage-banner__item">
        <span className="dp-usage-banner__label">Twins</span>
        <span className={clsx(
          'dp-usage-banner__value',
          isNearLimit('twins') && 'dp-usage-banner__value--near-limit',
        )}>
          {twinUsage.used}/{twinUsage.limit}
        </span>
      </div>
      {/* Agents, Users similarly */}
      {isNearLimit('twins') && (
        <Link to="/settings?tab=plan" className="dp-usage-banner__upgrade">
          Upgrade <ArrowUpRight size={12} />
        </Link>
      )}
    </div>
  );
}
```

Disable "New Digital Paryty" button when at twin limit:
```tsx
const atLimit = (usage?.twins?.used ?? 0) >= (usage?.twins?.limit ?? Infinity);
<button className="aef-btn aef-btn-active" onClick={openWizard} disabled={atLimit}>
  <Plus size={14} /> New Digital Paryty
</button>
```

---

## 8. New Pages — Detailed Specs

### 8.1 LoginPage

**File:** `src/pages/LoginPage.tsx`  
**Route:** `/login`  
**Auth:** Public (redirects if already authenticated)

```
┌────────────────────────────────────────────────────┐
│                                                    │
│                    [Paryty Logo]                    │
│                 Observability SaaS                  │
│                                                    │
│         ┌──────────────────────────────┐           │
│         │  Email                        │           │
│         │  ┌──────────────────────────┐ │           │
│         │  │ ops@gai-tech.com         │ │           │
│         │  └──────────────────────────┘ │           │
│         │                              │           │
│         │  Password                     │           │
│         │  ┌──────────────────────────┐ │           │
│         │  │ ••••••••••         [👁]  │ │           │
│         │  └──────────────────────────┘ │           │
│         │                              │           │
│         │  [✓] Remember me             │           │
│         │                              │           │
│         │  ┌──────────────────────────┐ │           │
│         │  │      Sign in             │ │           │
│         │  └──────────────────────────┘ │           │
│         │                              │           │
│         │  Don't have an account?       │           │
│         │  [Create one →]              │           │
│         └──────────────────────────────┘           │
│                                                    │
└────────────────────────────────────────────────────┘
```

**Component tree:**
```
LoginPage
├── BrandBlock (logo + tagline)
├── LoginForm
│   ├── EmailField (text input, email validation)
│   ├── PasswordField (password input, show/hide toggle)
│   ├── RememberMeCheckbox
│   ├── ErrorAlert (conditionally shown — wrong credentials, rate limited, etc.)
│   └── SubmitButton (with loading spinner state)
└── FooterLink ("Don't have an account? Create one")
```

**States:**
- **Idle:** Clean form
- **Loading:** Submit button shows spinner, fields disabled
- **Error:** Error banner below password field ("Invalid email or password", "Too many attempts. Try again in 30s", "Network error")
- **Success:** Brief flash + redirect to intended destination (or `/`)

**Form behavior:**
- Email validation: `/.+@.+\..+/` (basic, server validates thoroughly)
- Password: minimum 8 characters client-side warning
- "Remember me" → if checked, persist refresh token in localStorage (fallback)
- Enter key submits
- Escape clears error

### 8.2 RegisterPage

**File:** `src/pages/RegisterPage.tsx`  
**Route:** `/register`  
**Auth:** Public

**Multi-step wizard: Account → Plan Selection → Confirm**

```
Step 0: Account
┌──────────────────────────────────────┐
│  Create your account                 │
│                                      │
│  Full name                           │
│  ┌────────────────────────────────┐  │
│  │ Alex Chen                      │  │
│  └────────────────────────────────┘  │
│                                      │
│  Work email                          │
│  ┌────────────────────────────────┐  │
│  │ alex@gai-tech.com              │  │
│  └────────────────────────────────┘  │
│                                      │
│  Password                            │
│  ┌────────────────────────────────┐  │
│  │ ••••••••••              [👁]  │  │
│  └────────────────────────────────┘  │
│  ████████░░  Strong                 │
│                                      │
│              [Continue →]            │
└──────────────────────────────────────┘

Step 1: Plan Selection
┌──────────────────────────────────────┐
│  Choose your plan                    │
│                                      │
│  ┌─────────┐ ┌─────────┐ ┌────────┐ │
│  │  Basic  │ │   Pro   │ │  Pro+  │ │
│  │         │ │  ◄───►  │ │        │ │
│  │  $0/mo  │ │ $99/mo  │ │$299/mo │ │
│  │         │ │         │ │        │ │
│  │ 1 Twin  │ │ 4 Twins │ │Unlimit.│ │
│  │ 5 Agent │ │25 Agent │ │100 Agt │ │
│  │ 1 User  │ │ 5 Users │ │20 Users│ │
│  │ 7d data │ │30d data │ │90d data│ │
│  │         │ │         │ │        │ │
│  │[Select] │ │[Select] │ │[Select]│ │
│  └─────────┘ └─────────┘ └────────┘ │
│                                      │
│         [← Back]    [Continue →]     │
└──────────────────────────────────────┘

Step 2: Confirm
┌──────────────────────────────────────┐
│  Review & create                     │
│                                      │
│  Account                             │
│  Name:  Alex Chen                    │
│  Email: alex@gai-tech.com            │
│                                      │
│  Plan                                │
│  Pro — $99/month                     │
│  4 Twins, 25 Agents, 5 Users         │
│                                      │
│  By creating an account you agree    │
│  to the [Terms] and [Privacy Policy] │
│                                      │
│         [← Back]  [Create Account]   │
└──────────────────────────────────────┘
```

**Component tree:**
```
RegisterPage
├── StepIndicator (3 dots, animated transitions)
├── StepRouter
│   ├── StepAccount
│   │   ├── NameField
│   │   ├── EmailField
│   │   ├── PasswordField (with strength meter)
│   │   └── ErrorAlert
│   ├── StepPlan
│   │   ├── PlanCard × N (fetched from GET /api/v1/plans)
│   │   └── PlanComparisonTable
│   └── StepConfirm
│       ├── ReviewSection (account)
│       ├── ReviewSection (plan)
│       ├── TermsCheckbox
│       └── ErrorAlert
└── WizardFooter (Back/Continue buttons)
```

**On submit:** Calls `authStore.register(params)` → on success, authStore is hydrated and user is redirected to `/`.

### 8.3 SettingsPage

**File:** `src/pages/SettingsPage.tsx`  
**Route:** `/settings`  
**Tabs:** Account | Plan & Billing | API Keys | Users (admin only)

```
┌──────────────────────────────────────────────────────┐
│  Settings                                            │
│                                                      │
│  [Account] [Plan & Billing] [API Keys] [Users]       │
│  ─────────────────────────────────────────────────── │
│                                                      │
│  Tab content based on active tab                     │
│                                                      │
└──────────────────────────────────────────────────────┘
```

**Tab: Account**
```
┌──────────────────────────────────────┐
│  Profile                             │
│  ┌────────────────────────────────┐  │
│  │ Avatar    [AL]  Change         │  │
│  │ Name      Alex Chen  [Edit]    │  │
│  │ Email     alex@gai-tech.com    │  │
│  │ Role      Tenant Admin         │  │
│  └────────────────────────────────┘  │
│                                      │
│  Security                            │
│  ┌────────────────────────────────┐  │
│  │ Password  ••••••••  [Change]   │  │
│  │ Sessions  3 active  [Manage]   │  │
│  └────────────────────────────────┘  │
│                                      │
│  Preferences (existing settings)     │
│  ┌────────────────────────────────┐  │
│  │ Theme, language, font scale    │  │
│  │ (migrate from settingsStore)   │  │
│  └────────────────────────────────┘  │
└──────────────────────────────────────┘
```

**Tab: Plan & Billing**
```
┌──────────────────────────────────────┐
│  Current Plan: Pro                   │
│  ┌────────────────────────────────┐  │
│  │  Pro — $99/month               │  │
│  │  Billing period: Jun 1–Jul 1   │  │
│  │                                │  │
│  │  Usage:                        │  │
│  │  Twins    2 / 4   ██████░░     │  │
│  │  Agents   12 / 25  █████░░░    │  │
│  │  Users    3 / 5    ████████░   │  │
│  │  Data     18 / 30d ████████░░  │  │
│  │                                │  │
│  │  [Upgrade to Pro+ →]           │  │
│  └────────────────────────────────┘  │
└──────────────────────────────────────┘
```

**Tab: API Keys**
```
┌──────────────────────────────────────┐
│  API Keys                            │
│                                      │
│  [+ Create API Key]                  │
│                                      │
│  ┌────────────────────────────────┐  │
│  │ Production Agent    pk_live_AbCd│  │
│  │ Created Jun 1       Last: now  │  │
│  │                          [···] │  │
│  ├────────────────────────────────┤  │
│  │ Staging Agent       pk_live_EfGh│  │
│  │ Created May 28      Last: Jun 7│  │
│  │                          [···] │  │
│  └────────────────────────────────┘  │
│                                      │
│  Create modal:                       │
│  ┌────────────────────────────────┐  │
│  │  Create API Key                │  │
│  │  Name: [Production Agent    ]  │  │
│  │  Expires: [Never ▾]           │  │
│  │                                │  │
│  │  ┌──────────────────────────┐  │  │
│  │  │ pk_live_xxxxxxxxxxxxxxx  │  │  │
│  │  │ [Copy]  ← shown ONCE     │  │  │
│  │  └──────────────────────────┘  │  │
│  └────────────────────────────────┘  │
└──────────────────────────────────────┘
```

**Tab: Users (Admin only)**
```
┌──────────────────────────────────────┐
│  Team Members                        │
│                                      │
│  [+ Invite User]                     │
│                                      │
│  ┌────────────────────────────────┐  │
│  │ AL  Alex Chen                  │  │
│  │     alex@gai-tech.com          │  │
│  │     Admin · Active      [···]  │  │
│  ├────────────────────────────────┤  │
│  │ MJ  Maria Jones                │  │
│  │     maria@gai-tech.com         │  │
│  │     Member · Active    [···]   │  │
│  ├────────────────────────────────┤  │
│  │ TS  Tom Smith                  │  │
│  │     tom@gai-tech.com           │  │
│  │     Viewer · Invited   [···]   │  │
│  └────────────────────────────────┘  │
└──────────────────────────────────────┘
```

Actions on [···] dropdown: Edit role, Disable/Enable, Remove.

**Invite modal:**
```
┌────────────────────────────────────┐
│  Invite Team Member                │
│                                    │
│  Email: [____________________]     │
│  Name:  [____________________]     │
│  Role:  [Admin ▾]                  │
│          Admin / Member / Viewer   │
│                                    │
│  [Cancel]          [Send Invite]   │
└────────────────────────────────────┘
```

### 8.4 TwinCreatePage

**File:** `src/pages/TwinCreatePage.tsx`  
**Route:** `/twins/new`

Enhanced version of the existing `CreateParytyWizard`. The existing wizard in `DashboardPage` is extracted into this page but also kept as a modal shortcut on the dashboard (which navigates to `/twins/new`).

**Enhanced steps (4 steps vs. 3):**

```
Step 0: Identity (same as existing)
Step 1: Abilities (same as existing)
Step 2: Agent Discovery (NEW)
Step 3: Confirm
```

**Step 2: Agent Discovery**

```
┌──────────────────────────────────────┐
│  Connect agents                      │
│                                      │
│  Your agents will auto-discover      │
│  services once connected.            │
│                                      │
│  ┌────────────────────────────────┐  │
│  │  Option A: Guided Setup        │  │
│  │                                │  │
│  │  Run this on your server:      │  │
│  │  ┌──────────────────────────┐  │  │
│  │  │ curl -sSL https://...    │  │  │
│  │  │ | bash -s -- --token     │  │  │
│  │  │ pk_live_xxx              │  │  │
│  │  └──────────────────────────┘  │  │
│  │  [📋 Copy]                     │  │
│  │                                │  │
│  │  ── or ──                      │  │
│  │                                │  │
│  │  Option B: Create API Key      │  │
│  │  [+ Create in Settings →]      │  │
│  └────────────────────────────────┘  │
│                                      │
│  Once agents connect, they appear:   │
│  ┌────────────────────────────────┐  │
│  │ ● agent-01  Connected  [✕]    │  │
│  │ ○ Waiting for agents...       │  │
│  └────────────────────────────────┘  │
│                                      │
│  (You can skip this and add later)   │
└──────────────────────────────────────┘
```

**Component tree:**
```
TwinCreatePage
├── PageHeader ("Create Digital Paryty")
├── StepIndicator (4 dots)
├── WizardContainer (framer-motion AnimatePresence for step transitions)
│   ├── StepIdentity (extracted from DashboardPage)
│   ├── StepAbilities (extracted from DashboardPage)
│   ├── StepAgents (NEW — agent onboarding)
│   └── StepConfirm (extracted from DashboardPage)
└── WizardFooter (Back / Continue / Create)
```

**On create:** POST `/api/v1/twins` → on success, navigate to `/twins/:id`.

### 8.5 TwinDetailPage

**File:** `src/pages/TwinDetailPage.tsx`  
**Route:** `/twins/:twinId`

A focused dashboard for a single Digital Paryty. This is the primary workspace.

```
┌──────────────────────────────────────────────────────┐
│  Payment Gateway Twin                        [Settings]│
│  System: Payment Gateway v3 · Healthy                 │
│                                                      │
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐        │
│  │ 142    │ │ 3      │ │ 0      │ │ 7d     │        │
│  │ Nodes  │ │ Alerts │ │ Crit   │ │Forecast│        │
│  └────────┘ └────────┘ └────────┘ └────────┘        │
│                                                      │
│  ┌────────────────────────────────────────────────┐  │
│  │  Ability cards — click to navigate             │  │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐       │  │
│  │  │Topology  │ │ Metrics  │ │ Timeline │       │  │
│  │  │ 142      │ │ 12       │ │ 34       │       │  │
│  │  │ nodes    │ │ series   │ │ snaps    │       │  │
│  │  │ [Open →] │ │ [Open →] │ │ [Open →] │       │  │
│  │  └──────────┘ └──────────┘ └──────────┘       │  │
│  └────────────────────────────────────────────────┘  │
│                                                      │
│  ┌────────────────────────────────────────────────┐  │
│  │  Recent Activity                                │  │
│  │  · Agent payment-svc-01 connected (2m ago)      │  │
│  │  · Alert: CPU > 80% on payment-svc-02 (15m)     │  │
│  │  · Forecast updated: 7-day horizon (1h ago)     │  │
│  └────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────┘
```

### 8.6 TwinSettingsPage

**File:** `src/pages/TwinSettingsPage.tsx`  
**Route:** `/twins/:twinId/settings`

Manage abilities, agents, and configuration for a single twin.

```
┌──────────────────────────────────────────────────────┐
│  ← Back to Twin                                      │
│                                                      │
│  [General] [Abilities] [Agents] [Danger Zone]        │
│  ─────────────────────────────────────────────────── │
│                                                      │
│  Tab: General                                        │
│  Name: [Payment Gateway Twin          ]              │
│  System: [Payment Gateway v3          ]              │
│                                                      │
│  Tab: Abilities                                      │
│  Toggle on/off abilities (same grid as wizard)       │
│                                                      │
│  Tab: Agents                                         │
│  List connected agents, add agent command, remove    │
│                                                      │
│  Tab: Danger Zone                                    │
│  [Delete this Digital Paryty] (red, confirmation)    │
└──────────────────────────────────────────────────────┘
```

---

## 9. Plan-Aware Feature Gating

### Feature → Plan Matrix

| Feature | Basic | Pro | Pro+ |
|---|---|---|---|
| Topology (live graph) | ✓ | ✓ | ✓ |
| Metrics & Monitoring | ✓ | ✓ | ✓ |
| Alerts | ✓ | ✓ | ✓ |
| Timeline Replay | ✗ | ✓ | ✓ |
| Paryty-Intel (Forecasting) | ✗ | ✗ | ✓ |
| Watif Drills | ✗ | ✗ | ✓ |
| API Keys | ✗ | ✓ | ✓ |
| Custom Dashboards | ✗ | ✗ | ✓ |
| SSO | ✗ | ✗ | ✓ |

### Gating Points

| Location | Gate | What Happens |
|---|---|---|
| Sidebar nav items | `hasFeature(feature)` | Items hidden if feature not in plan |
| Route `/timeline` | `PlanGate required="pro"` | Redirected to upgrade prompt |
| Route `/intel` | `PlanGate required="pro_plus"` | Redirected to upgrade prompt |
| Settings → API Keys tab | `hasFeature('api_keys')` | Tab hidden if not available |
| "New Twin" button | `usage.twins.used < usage.twins.limit` | Button disabled with tooltip |
| "Invite User" button | `usage.users.used < usage.users.limit` | Button disabled with tooltip |
| Dashboard upgrade CTA | `plan.tier === 'basic'` | Shown for basic users |

### UpgradePrompt Component

**File:** `src/components/common/UpgradePrompt.tsx`

A reusable upgrade prompt shown when a user navigates to a gated feature:

```tsx
function UpgradePrompt({ required, current }: { required: PlanTier; current: PlanTier }) {
  return (
    <div className="aef-container-card" style={{ maxWidth: 480, margin: 'var(--aef-space-8) auto' }}>
      <div className="aef-container-card__header">
        <span className="aef-container-card__title">Upgrade Required</span>
      </div>
      <div className="aef-container-card__body">
        <p>This feature requires the <strong>{required === 'pro_plus' ? 'Pro+' : 'Pro'}</strong> plan.</p>
        <p>You're currently on <strong>{current}</strong>.</p>
        <Link to="/settings?tab=plan" className="aef-btn aef-btn-active">
          View plans <ArrowUpRight size={14} />
        </Link>
      </div>
    </div>
  );
}
```

---

## 10. UX Specifications

### 10.1 Loading States

| Scenario | UX |
|---|---|
| Initial auth check | Full-screen skeleton: logo + pulsing progress bar |
| Page transition (lazy route) | Inline skeleton matching page shape |
| Login/Register submit | Button spinner + disabled fields |
| Plan fetch | Skeleton cards (3 placeholder cards) |
| Twin list fetch | Skeleton cards (same shape as DigitalParytyCard) |
| Settings tabs | Tab content skeleton |

### 10.2 Error States

| Scenario | UX |
|---|---|
| Login failed (401) | Inline error banner below password field |
| Register failed (409 — email taken) | Inline error on email field |
| Network error | Toast: "Connection lost. Retrying…" + auto-retry |
| Rate limited (429) | Toast: "Too many attempts. Try again in X seconds." |
| Session expired (401 on protected route) | Silent refresh → if fails, redirect to login with toast "Session expired. Please sign in again." |
| Server error (500) | Error boundary card with retry button |
| Plan fetch failed | Degraded mode: sidebar shows all items (no gating), upgrade prompts hidden |

### 10.3 Empty States

| Scenario | UX |
|---|---|
| No twins (DashboardPage) | Existing EmptyState component (already built) |
| No API keys | Illustration + "Create your first API key" CTA |
| No team members (admin) | "Invite your first team member" CTA |
| Feature not available (plan gate) | UpgradePrompt (see §9) |

### 10.4 Toast Notifications

**Implementation:** Lightweight toast system using Zustand store + React portal.

**File:** `src/components/common/Toast.tsx`

```typescript
interface ToastState {
  toasts: Toast[];
  addToast: (toast: Omit<Toast, 'id'>) => void;
  removeToast: (id: string) => void;
}

type ToastType = 'success' | 'error' | 'info' | 'warning';
```

**Triggers:**
- "Account created successfully" — after register
- "Signed in as Alex Chen" — after login
- "Digital Paryty created" — after twin creation
- "API key created — copy it now" — after key generation
- "User invited" — after inviting team member
- "Plan upgraded to Pro+" — after plan change
- "Session expired. Please sign in again." — after 401 cascade

### 10.5 Animations (framer-motion)

| Element | Animation |
|---|---|
| Page transitions | `AnimatePresence` — fade + slide-up (200ms, `--aef-ease-emphasized`) |
| Wizard step transitions | Slide left/right (220ms, `--aef-ease-settle`) |
| Modal open/close | Scale + fade (120ms) |
| Dropdown menus | Scale-y from top (120ms) |
| Toast enter/exit | Slide in from right + fade (200ms spring) |
| Plan cards hover | Slight scale-up (1.02) + border glow |
| Upgrade prompt | Fade in + slide up (300ms) |

### 10.6 Responsive Behavior

Target minimum: **1280px width** (enterprise desktop).

| Breakpoint | Behavior |
|---|---|
| ≥1440px | Full layout: sidebar (244px) + content + optional panel |
| 1280–1439px | Sidebar collapses to icon mode (56px) by default |
| <1280px | Sidebar auto-collapses; mobile warning shown ("Paryty is optimized for larger screens") |

### 10.7 Keyboard & Accessibility

- Tab order: Skip link → Sidebar nav → Main content → Header actions
- Login/Register forms: All inputs labeled, error messages linked via `aria-describedby`
- Modals: Focus trap, Escape to close, `aria-modal="true"`
- Toast: `role="alert"`, `aria-live="polite"`
- Plan cards: `aria-pressed` for selection state

---

## 11. File Tree — Phase 8 Deliverables

```
frontend/src/
├── App.tsx                          # MODIFIED — new routes, AuthProvider wrapper
├── main.tsx                         # UNCHANGED
├── index.css                        # MODIFIED — add auth page styles, toast styles
│
├── api/
│   ├── rest.ts                      # MODIFIED — auth headers, 401 interceptor, token getter
│   └── websocket.ts                 # MODIFIED — token passing, disconnect on logout
│
├── stores/
│   ├── authStore.ts                 # NEW
│   ├── planStore.ts                 # NEW
│   ├── toastStore.ts                # NEW
│   ├── dashboardStore.ts            # MODIFIED — twin creation calls API
│   └── settingsStore.ts             # UNCHANGED
│
├── types/
│   ├── auth.ts                      # NEW
│   ├── apiKeys.ts                   # NEW
│   ├── plan.ts                      # NEW
│   └── index.ts                     # MODIFIED — add exports
│
├── components/
│   ├── auth/
│   │   ├── AuthProvider.tsx         # NEW — auth context, token refresh, loading skeleton
│   │   ├── ProtectedRoute.tsx       # NEW
│   │   ├── PublicRoute.tsx          # NEW
│   │   └── PlanGate.tsx             # NEW
│   │
│   ├── common/
│   │   ├── ErrorBoundary.tsx        # UNCHANGED
│   │   ├── Toast.tsx                # NEW — toast container + individual toast
│   │   ├── UpgradePrompt.tsx        # NEW
│   │   └── StepIndicator.tsx        # NEW — reusable multi-step dot indicator
│   │
│   ├── layout/
│   │   ├── AppShell.tsx             # MODIFIED — no change needed (route structure handles it)
│   │   ├── Header.tsx               # MODIFIED — dynamic tenant, plan badge, user menu
│   │   ├── Sidebar.tsx              # MODIFIED — plan-aware items, dynamic profile
│   │   ├── StatusBar.tsx            # UNCHANGED
│   │   └── UserMenu.tsx             # NEW — avatar + dropdown extracted from Header
│   │
│   └── dashboard/
│       └── DashboardPage.tsx        # MODIFIED — plan limits, upgrade CTA, twin count
│
├── pages/                           # NEW directory
│   ├── LoginPage.tsx
│   ├── RegisterPage.tsx
│   ├── SettingsPage.tsx
│   ├── TwinCreatePage.tsx
│   ├── TwinDetailPage.tsx
│   └── TwinSettingsPage.tsx
│
├── hooks/
│   └── useAuth.ts                   # NEW — convenience hook for auth state
│
└── __tests__/
    ├── authStore.test.ts            # NEW
    ├── planStore.test.ts            # NEW
    ├── LoginPage.test.tsx           # NEW
    ├── RegisterPage.test.tsx        # NEW
    ├── ProtectedRoute.test.tsx      # NEW
    ├── PlanGate.test.tsx            # NEW
    ├── SettingsPage.test.tsx         # NEW
    └── rest-client-auth.test.ts     # NEW
```

---

## 12. Implementation Order

Phased to minimize merge conflicts and allow incremental testing:

### Phase 8a — Auth Foundation (no UI changes)
1. Create `src/types/auth.ts`, `src/types/plan.ts`, `src/types/apiKeys.ts`
2. Create `src/stores/authStore.ts` (login/register/logout/refresh actions)
3. Create `src/stores/planStore.ts` (fetchPlans, fetchCurrentPlan)
4. Create `src/stores/toastStore.ts`
5. Hardened `src/api/rest.ts` — add `setTokenGetter`, `setAuthRefreshCallback`, 401 interceptor, Authorization header
6. Hardened `src/api/websocket.ts` — add `setTokenGetter`, token in connect URL
7. Create `src/components/auth/AuthProvider.tsx` — inject token getter + refresh callback into RestClient and WebSocket
8. **Gate:** All existing routes still work (auth is optional — if token is null, requests go without Authorization)

### Phase 8b — Auth Pages
1. Create `src/components/auth/ProtectedRoute.tsx`
2. Create `src/components/auth/PublicRoute.tsx`
3. Create `src/pages/LoginPage.tsx` + styles
4. Create `src/pages/RegisterPage.tsx` + styles
5. Update `App.tsx` — add auth routes, wrap protected routes
6. **Gate:** Can log in, register, see dashboard. Existing pages still work.

### Phase 8c — AppShell Auth Integration
1. Modify `Header.tsx` — dynamic tenant name, plan badge, UserMenu
2. Modify `Sidebar.tsx` — plan-aware nav items, dynamic profile
3. Create `src/components/layout/UserMenu.tsx`
4. Create `src/components/common/Toast.tsx` + mount in AppShell
5. **Gate:** Nav shows correct items per plan. User sees their name/tenant.

### Phase 8d — Dashboard Plan Integration
1. Modify `DashboardPage.tsx` — usage banner, twin limits, upgrade CTA
2. Create `src/components/common/UpgradePrompt.tsx`
3. Create `src/components/auth/PlanGate.tsx`
4. **Gate:** Basic users see limits. Pro+ users see Intel link.

### Phase 8e — Settings + Twin Pages
1. Create `src/pages/SettingsPage.tsx` — all 4 tabs
2. Create `src/pages/TwinCreatePage.tsx` — 4-step wizard
3. Create `src/pages/TwinDetailPage.tsx`
4. Create `src/pages/TwinSettingsPage.tsx`
5. Add all routes to `App.tsx`
6. **Gate:** Full SaaS platform experience.

### Phase 8f — Polish + Tests
1. Add all unit tests
2. Add framer-motion page transitions
3. Toast notifications wired to all actions
4. Loading skeletons for all async pages
5. Empty states for API keys, users, twins
6. Responsive audit at 1280px and 1440px
7. Keyboard navigation audit
8. **Gate:** Production-ready Phase 8 frontend.

---

## Appendix: Key Design Decisions

### A. Why in-memory token storage?
XSS-resistant. The `access_token` never touches `localStorage` or `sessionStorage`. If an attacker injects a script, they cannot extract the token. The refresh token is stored in an httpOnly cookie set by the backend — inaccessible to JavaScript entirely.

**Trade-off:** Page refresh forces a `/api/v1/auth/refresh` call. This adds ~200ms to initial load. Mitigated by the loading skeleton.

### B. Why inject token getter into RestClient rather than import authStore?
Separation of concerns. `RestClient` is a data-access layer — it should not import UI state stores. Injection via `setTokenGetter` keeps it testable and prevents circular dependencies.

### C. Why a full TwinCreatePage instead of just a modal?
The enhanced wizard (4 steps with agent discovery) is too complex for a modal. A dedicated page provides more space for the agent connection guide, copyable commands, and live agent discovery status. The dashboard retains a "New Digital Paryty" button that navigates to `/twins/new`.

### D. Why not use Next.js or Remix?
The existing codebase is Vite + React SPA. Migrating the framework at Phase 8 would introduce unacceptable risk. The PixiJS/WebGL rendering engine is tightly coupled to the SPA lifecycle. A framework migration is a separate initiative.

### E. Plan gating — frontend vs. backend
Frontend gating is UX convenience (hiding unavailable features). Backend enforces authorization. A user could inspect and unhide a gated route, but the API will return 403. This is by design — defense in depth.
