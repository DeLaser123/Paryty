# Phase 8 — Production Hardening + Multi-Tenant SaaS Platform

**Status:** READY FOR EXECUTION
**Target LOC:** ~12,000 (Phase 8 spec: ~7,000 + Multi-tenancy: ~5,000)
**Primary Agents:** go-cluster-engineer, frontend-engineer, integration-engineer, security-reviewer

---

## Context

Phase 8 transforms Paryty from a working observability system into an enterprise-grade SaaS platform. The existing codebase has tenant management and API keys (controlplane module) but lacks authentication for web users, plan-based feature gating, Paryty Twin lifecycle management, sub-user permissions, TLS/mTLS, audit logging, and self-monitoring. The frontend has zero auth UI.

This plan implements both the Phase 8 hardened spec (security, observability, deployment, testing) AND the user's multi-tenancy requirements (registration with plan selection, sub-user management with granular permissions, Paryty Twin creation wizard, enterprise-grade plan configurability).

## Architecture Decisions (LOCKED)

| Decision | Selection | Rationale |
|---|---|---|
| Web auth | JWT (HS256, 15min access + 7d refresh) | Browser-friendly; API keys remain for agents/machines |
| Token storage | In-memory access token; httpOnly cookie refresh token | XSS-resistant; refresh survives page reload |
| Plan definitions | YAML config file (`configs/cluster/plans.yaml`) | Single source of truth, version-controlled, edit without code changes |
| Plan assignments | PostgreSQL `tenant_plans` table (JSONB snapshot) | YAML changes don't break existing tenants; explicit sync for upgrades |
| RBAC model | Simple 3-role hierarchy (admin/operator/viewer) + JSONB permission overrides | Covers 90% of cases; per-user overrides for the 10% |
| Paryty Twin | Agent-driven discovery via `twin_id` in agent config | User creates twin → gets ID → installs agent with ID → auto-discovers |
| Feature gating | 3-layer: feature check → limit check → plan active check | Defense in depth; returns 402 with upgrade hint |
| mTLS | cert-manager (K8s) + self-signed fallback (non-K8s) | Per Phase 8 spec; graceful degradation |

## Plan Definitions

```yaml
# configs/cluster/plans.yaml
plans:
  basic:
    max_twins: 2
    creatable: true
    features: { topology_monitoring: true, metrics: false, alerts: false, timeline_replay: false, paryty_intel: false }
    limits: { agents_per_twin: 10, data_retention_days: 7, sub_users: 2 }
  pro:
    max_twins: 4
    creatable: true
    features: { topology_monitoring: true, metrics: true, alerts: true, timeline_replay: false, paryty_intel: false }
    limits: { agents_per_twin: 50, data_retention_days: 30, sub_users: 10 }
  pro_plus:
    max_twins: 6
    creatable: true
    features: { topology_monitoring: true, metrics: true, alerts: true, timeline_replay: true, paryty_intel: true }
    limits: { agents_per_twin: 200, data_retention_days: 90, sub_users: 25 }
  enterprise:
    max_twins: 10
    creatable: false  # NOT available for self-signup
    features: { topology_monitoring: true, metrics: true, alerts: true, timeline_replay: true, paryty_intel: true, sso: true, custom_retention: true, dedicated_infra: true }
```

---

## Task Breakdown

### TASK 1: Proto + Codegen (Contract First)
**Agent:** integration-engineer
**Files to create/modify:**
- `proto/paryty/v1/auth.proto` — AuthService, UserService, PlanService, TwinService (~300 LOC)
- Run `buf generate` to produce Go stubs
- Update `cluster/internal/proto/` with generated `.pb.go` files

**Verification:** Proto compiles without errors; generated Go code compiles.

---

### TASK 2: Database Schema Extensions
**Agent:** go-cluster-engineer
**Files to modify:**
- `cluster/internal/controlplane/schema.go` — Add `EnsurePhase8Tables()` with DDL for:
  - `users` (id, tenant_id, email, password_hash, name, role, permissions JSONB)
  - `refresh_tokens` (id, user_id, token_hash, device_info, expires_at, revoked_at)
  - `tenant_plans` (tenant_id, plan_name, features JSONB, started_at, expires_at)
  - `paryty_twins` (id, tenant_id, name, description, status, twin_config JSONB)
  - `agent_assignments` (agent_id, twin_id, tenant_id, composite PK)
  - `audit_log` (id, tenant_id, user_id, action, resource_type, resource_id, details JSONB)

**Verification:** `go build ./...` succeeds; DDL executes idempotently.

---

### TASK 3: Plan Configuration + Loader
**Agent:** go-cluster-engineer
**Files to create:**
- `configs/cluster/plans.yaml` — Full plan definitions (Basic/Pro/Pro+/Enterprise)
- `cluster/internal/plan/loader.go` — YAML parsing, PlanDefinition/FeatureSet/LimitSet/QuotaSet structs
- `cluster/internal/plan/engine.go` — PlanEngine with sync.RWMutex, HasFeature(), CheckLimit(), GetEffectivePlan(), ListPlans(), SyncPlanToTenants()
- `cluster/internal/plan/middleware.go` — GinFeatureGate, GinTwinLimitGate, GrpcFeatureGate interceptors

**Verification:** `go test ./internal/plan/...` passes; YAML parses correctly.

---

### TASK 4: JWT Auth + Password Management
**Agent:** go-cluster-engineer
**Files to create:**
- `cluster/internal/auth/jwt.go` — TokenManager: GeneratePair(), ValidateAccess(), ValidateRefresh(), RefreshPair()
- `cluster/internal/auth/password.go` — bcrypt hashing (cost=12), password policy validation
- `cluster/internal/auth/middleware.go` — Gin JWT middleware (extract Bearer, validate, inject claims into context)
- `cluster/internal/auth/handler.go` — AuthService gRPC handler: Register, Login, RefreshToken, Logout, ValidateToken

**Dependency:** `github.com/golang-jwt/jwt/v5` added to go.mod

**Verification:** `go test ./internal/auth/...` passes.

---

### TASK 5: User + Twin Managers
**Agent:** go-cluster-engineer
**Files to create:**
- `cluster/internal/user/user.go` — UserManager: CreateSubUser, ListUsers, GetUser, UpdateUser, DeleteUser
- `cluster/internal/user/permissions.go` — PermissionValidator with role defaults + JSONB overrides
- `cluster/internal/twin/twin.go` — TwinManager: CreateTwin, GetTwin, ListTwins, UpdateTwin, DeleteTwin, CountTwins, GetTwinConfigForAgent
- `cluster/internal/twin/agent_assignment.go` — AgentAssigner: RegisterAgent (upsert agent→twin)

**Verification:** `go test ./internal/user/... ./internal/twin/...` passes.

---

### TASK 6: Security Layer (RBAC, Audit, TLS, Secrets)
**Agent:** go-cluster-engineer
**Files to create:**
- `cluster/internal/security/rbac.go` — RolePermissions map, RequireRole/RequirePermission middleware
- `cluster/internal/security/audit.go` — AuditLogger with buffered channel + PostgreSQL writer
- `cluster/internal/security/tls.go` — ServerTLS(), ClientTLS(), LoadTLSFromEnv()
- `cluster/internal/security/secrets.go` — SecretsManager (env → Vault-ready interface)
- `cluster/internal/security/middleware.go` — RequestID, AuditBegin/End, mTLS enforcement middleware

**Verification:** `go test ./internal/security/...` passes.

---

### TASK 7: Wire Everything — Query Service
**Agent:** go-cluster-engineer
**Files to modify:**
- `cluster/cmd/query/main.go` — Initialize PlanEngine, TokenManager, AuditLogger, wire middleware chain, register new gRPC services, register new REST routes
- `cluster/internal/api/query/rest.go` — Add public routes (auth/register, auth/login, plans), authenticated routes with feature gates, admin routes with RBAC

**Middleware chain (REST):** CORS → RequestID → AuditBegin → JWT Auth → Plan Feature Gate → RBAC → Rate Limit → Handler → AuditEnd

**Route groups:**
- `public`: `/api/v1/auth/register`, `/api/v1/auth/login`, `/api/v1/auth/refresh`, `/api/v1/plans`
- `auth`: All other routes with JWT middleware
- `twins`: Feature-gated on `topology_monitoring`
- `metrics`: Feature-gated on `metrics`
- `timeline`: Feature-gated on `timeline_replay`
- `intel`: Feature-gated on `paryty_intel`
- `admin`: RBAC-gated on `admin` role

**Verification:** `go build ./cmd/query/...` succeeds; `go test ./internal/api/query/...` passes.

---

### TASK 8: Agent Ingestion — Twin-Aware Registration
**Agent:** go-cluster-engineer
**Files to modify:**
- `cluster/internal/api/ingestion/grpc_adapter.go` — When agent registers with `twin_id` in metadata, call AgentAssigner.RegisterAgent()
- `cluster/internal/api/ingestion/auth.go` — Extract `twin_id` from gRPC metadata alongside `x-api-key`

**Agent flow:** Starts → gRPC GetTwinConfig(twin_id, agent_id) → gRPC RegisterAgent → gRPC StreamMetrics

**Verification:** `go test ./internal/api/ingestion/...` passes.

---

### TASK 9: Backend Tests
**Agent:** go-cluster-engineer
**Files to create:**
- `cluster/internal/auth/jwt_test.go`, `password_test.go`, `middleware_test.go`
- `cluster/internal/plan/loader_test.go`, `engine_test.go`, `middleware_test.go`
- `cluster/internal/user/user_test.go`, `permissions_test.go`
- `cluster/internal/twin/twin_test.go`, `agent_assignment_test.go`
- `cluster/internal/security/rbac_test.go`, `audit_test.go`, `tls_test.go`

**Verification:** `go test ./...` — all tests pass with race detector.

---

### TASK 10: Backend Verification
**Skill:** verify-go
Run full Go verification pipeline: build → vet → test with race detector → coverage.

---

### TASK 11: Frontend — Auth Foundation (Types, Stores, API Hardening)
**Agent:** frontend-engineer
**Files to create/modify:**
- `frontend/src/types/auth.ts` — User, Tenant, RegisterParams, LoginResponse, SubUser, CreateSubUserParams
- `frontend/src/types/apiKeys.ts` — ApiKey, CreateApiKeyResponse
- `frontend/src/types/plan.ts` — Plan, CurrentPlan, FeatureFlag, ResourceType, UsageInfo
- `frontend/src/types/index.ts` — Add exports for new types
- `frontend/src/stores/authStore.ts` — login, register, logout, refreshToken, setAuth, clearAuth
- `frontend/src/stores/planStore.ts` — fetchPlans, fetchCurrentPlan, hasFeature, usageFor, isNearLimit
- `frontend/src/stores/toastStore.ts` — Toast queue with addToast/removeToast
- `frontend/src/api/rest.ts` — Add setTokenGetter, setAuthRefreshCallback, Authorization header, 401 interceptor
- `frontend/src/api/websocket.ts` — Add setTokenGetter, token in connect URL

**Verification:** `npm run build` succeeds; existing pages still render.

---

### TASK 12: Frontend — Auth Components + Pages
**Agent:** frontend-engineer
**Files to create:**
- `frontend/src/components/auth/AuthProvider.tsx` — Auth context, initial refresh, loading/anonymous/authenticated states
- `frontend/src/components/auth/ProtectedRoute.tsx` — Redirect to /login if anonymous
- `frontend/src/components/auth/PublicRoute.tsx` — Redirect to / if authenticated
- `frontend/src/components/auth/PlanGate.tsx` — Redirect to upgrade prompt if plan insufficient
- `frontend/src/pages/LoginPage.tsx` — Email + password form, error states, "Remember me"
- `frontend/src/pages/RegisterPage.tsx` — 3-step wizard: Account → Plan Selection → Confirm
- `frontend/src/components/common/StepIndicator.tsx` — Reusable step dots
- `frontend/src/components/common/Toast.tsx` — Toast container + individual toasts
- `frontend/src/hooks/useAuth.ts` — Convenience hook

**Verification:** Can log in, register with plan selection, see dashboard.

---

### TASK 13: Frontend — AppShell Auth Integration
**Agent:** frontend-engineer
**Files to modify/create:**
- `frontend/src/components/layout/Header.tsx` — Dynamic tenant name (from authStore), plan badge pill (Basic/Pro/Pro+), UserMenu dropdown
- `frontend/src/components/layout/Sidebar.tsx` — Plan-aware nav items (hide Intel for Basic), dynamic profile from authStore
- `frontend/src/components/layout/UserMenu.tsx` — Avatar + dropdown (Settings, Sign out)
- `frontend/src/components/common/UpgradePrompt.tsx` — "This feature requires Pro/Pro+" with link to plan settings

**Verification:** Nav items reflect plan; user sees their name and tenant.

---

### TASK 14: Frontend — Dashboard Plan Integration
**Agent:** frontend-engineer
**Files to modify:**
- `frontend/src/components/dashboard/DashboardPage.tsx` — Plan usage banner ("2/4 Twins"), disable "New Twin" at limit, upgrade CTA, twin creation calls real API

**Verification:** Basic users see limits; Pro+ users see all features.

---

### TASK 15: Frontend — Settings + Twin Pages
**Agent:** frontend-engineer
**Files to create:**
- `frontend/src/pages/SettingsPage.tsx` — 4 tabs: Account, Plan & Billing, API Keys, Users (admin only)
- `frontend/src/pages/TwinCreatePage.tsx` — 4-step wizard: Identity → Abilities → Agent Discovery → Confirm
- `frontend/src/pages/TwinDetailPage.tsx` — Per-twin dashboard with ability cards and recent activity
- `frontend/src/pages/TwinSettingsPage.tsx` — General, Abilities, Agents, Danger Zone tabs

**Verification:** Full SaaS platform UX: settings, twin creation, twin management.

---

### TASK 16: Frontend — Polish (Animations, Loading, Error, Empty States)
**Agent:** frontend-engineer
**Files to modify:**
- Add framer-motion page transitions (AnimatePresence)
- Add loading skeletons for all async pages
- Add error states (401, 409, 429, 500) with appropriate UX
- Add empty states (no twins, no API keys, no users)
- Wire toast notifications to all actions
- Responsive audit at 1280px and 1440px
- Keyboard navigation audit

**Verification:** `npm run build` + manual walkthrough of all states.

---

### TASK 17: Frontend — Update App.tsx Routing
**Agent:** frontend-engineer
**Files to modify:**
- `frontend/src/App.tsx` — Wrap with AuthProvider, add auth routes (/login, /register), add new protected routes (/settings, /twins/new, /twins/:id, /twins/:id/settings), fix /timeline route, fix /intel route, add PlanGate wrappers

**Final route table:**
| Path | Auth | Plan |
|---|---|---|
| `/login` | Public | — |
| `/register` | Public | — |
| `/` | Protected | Basic+ |
| `/topology` | Protected | Basic+ |
| `/metrics` | Protected | Basic+ |
| `/alerts` | Protected | Basic+ |
| `/timeline` | Protected | Pro+ |
| `/intel` | Protected | Pro+ |
| `/settings` | Protected | Basic+ |
| `/twins/new` | Protected | Basic+ |
| `/twins/:id` | Protected | Basic+ |
| `/twins/:id/settings` | Protected | Basic+ |

**Verification:** All routes resolve correctly; auth guards work.

---

### TASK 18: Frontend Tests
**Agent:** frontend-engineer
**Files to create:**
- `frontend/src/__tests__/authStore.test.ts`
- `frontend/src/__tests__/planStore.test.ts`
- `frontend/src/__tests__/LoginPage.test.tsx`
- `frontend/src/__tests__/RegisterPage.test.tsx`
- `frontend/src/__tests__/ProtectedRoute.test.tsx`
- `frontend/src/__tests__/PlanGate.test.tsx`

**Verification:** `npm test` — all tests pass.

---

### TASK 19: Frontend Verification
**Skill:** verify-frontend
Run full frontend verification: TypeScript check → build → tests.

---

### TASK 20: Security Review
**Agent:** security-reviewer
Review all backend and frontend changes for:
- JWT implementation correctness
- Password hashing strength
- API key validation
- RBAC enforcement
- Feature gate bypass attempts
- XSS vulnerabilities in auth UI
- CSRF protection
- Audit log completeness

**Verification:** Security review passes with no HIGH or CRITICAL findings.

---

### TASK 21: Integration Tests
**Agent:** go-cluster-engineer (with integration-engineer review)
**Files to create:**
- `tests/integration/auth_flow_test.go` — Register → Login → Refresh → Logout
- `tests/integration/twin_flow_test.go` — Create twin → Agent register → Query twin
- `tests/integration/plan_gating_test.go` — Basic user blocked from Pro+ features

**Verification:** `go test -v ./tests/integration/...` passes.

---

### TASK 22: Cross-Component Integration Verification
**Skill:** test-integration
Verify protobuf contracts, auth flow end-to-end, frontend→backend API contracts.

---

### TASK 23: Phase 8 Spec — Self-Monitoring + Health Fallback + SLO Tracker
**Agent:** go-cluster-engineer
**Files to create:**
- `cluster/internal/monitoring/self_monitor.go` — Paryty self-instrumentation with Go SDK
- `cluster/internal/monitoring/health_fallback.go` — Independent health checker (TCP/HTTP/Ping)
- `cluster/internal/monitoring/alert_webhook.go` — Webhook alert channel
- `cluster/internal/monitoring/slo_tracker.go` — SLO tracking with error budgets

**Verification:** `go test ./internal/monitoring/...` passes.

---

### TASK 24: Phase 8 Spec — Helm Charts
**Agent:** go-cluster-engineer
**Files to create:**
- `deploy/helm/paryty/Chart.yaml` — Umbrella chart with 6 subcharts
- `deploy/helm/paryty/values.yaml` — Global values
- `deploy/helm/paryty/values-dev.yaml` — Dev overrides
- `deploy/helm/paryty/values-staging.yaml` — Staging overrides
- `deploy/helm/paryty/values-prod.yaml` — Production overrides
- `deploy/helm/paryty/templates/_helpers.tpl` — Shared template helpers
- `deploy/helm/paryty-agent/` — Subchart (Chart.yaml, values.yaml, templates/)
- `deploy/helm/paryty-cluster/` — Subchart
- `deploy/helm/paryty-frontend/` — Subchart
- `deploy/helm/paryty-security/` — Subchart (RBAC, certs, policies)
- `deploy/helm/paryty-monitoring/` — Subchart (self-monitoring)
- `deploy/helm/paryty-storage/` — Subchart (storage dependencies)

**Verification:** `helm lint deploy/helm/paryty` passes; `helm template` generates valid K8s manifests.

---

### TASK 25: Phase 8 Spec — CI/CD Enhancement
**Agent:** go-cluster-engineer
**Files to modify:**
- `.github/workflows/cd.yml` — Add container scanning (Trivy), SLSA provenance, Helm chart publishing

**Verification:** CI/CD workflow syntax validates.

---

### TASK 26: Final Verification — Full Suite
Run all verification skills:
1. `/verify-go` — Go build, vet, test, race
2. `/verify-frontend` — TypeScript check, build, test
3. `/test-integration` — Cross-component tests

---

## Files Summary

### Backend — New Files (25+)
| File | LOC |
|---|---|
| `proto/paryty/v1/auth.proto` | ~300 |
| `configs/cluster/plans.yaml` | ~100 |
| `cluster/internal/auth/jwt.go` | ~150 |
| `cluster/internal/auth/password.go` | ~100 |
| `cluster/internal/auth/middleware.go` | ~100 |
| `cluster/internal/auth/handler.go` | ~250 |
| `cluster/internal/plan/loader.go` | ~100 |
| `cluster/internal/plan/engine.go` | ~250 |
| `cluster/internal/plan/middleware.go` | ~150 |
| `cluster/internal/user/user.go` | ~200 |
| `cluster/internal/user/permissions.go` | ~100 |
| `cluster/internal/twin/twin.go` | ~250 |
| `cluster/internal/twin/agent_assignment.go` | ~100 |
| `cluster/internal/security/rbac.go` | ~150 |
| `cluster/internal/security/audit.go` | ~150 |
| `cluster/internal/security/tls.go` | ~150 |
| `cluster/internal/security/secrets.go` | ~100 |
| `cluster/internal/security/middleware.go` | ~100 |
| `cluster/internal/monitoring/self_monitor.go` | ~400 |
| `cluster/internal/monitoring/health_fallback.go` | ~300 |
| `cluster/internal/monitoring/alert_webhook.go` | ~100 |
| `cluster/internal/monitoring/slo_tracker.go` | ~200 |
| `deploy/helm/` (12 files) | ~1,200 |
| `tests/integration/` (3 files) | ~600 |

### Backend — Modified Files (5)
| File | Change |
|---|---|
| `cluster/internal/controlplane/schema.go` | Add Phase 8 DDL |
| `cluster/cmd/query/main.go` | Wire auth, plans, middleware |
| `cluster/internal/api/query/rest.go` | Add auth routes + feature gates |
| `cluster/internal/api/ingestion/grpc_adapter.go` | Twin-aware agent registration |
| `cluster/internal/api/ingestion/auth.go` | Extract twin_id from metadata |

### Frontend — New Files (25+)
| File | LOC |
|---|---|
| `src/types/auth.ts` | ~50 |
| `src/types/apiKeys.ts` | ~30 |
| `src/types/plan.ts` | ~50 |
| `src/stores/authStore.ts` | ~150 |
| `src/stores/planStore.ts` | ~120 |
| `src/stores/toastStore.ts` | ~50 |
| `src/hooks/useAuth.ts` | ~20 |
| `src/components/auth/AuthProvider.tsx` | ~150 |
| `src/components/auth/ProtectedRoute.tsx` | ~30 |
| `src/components/auth/PublicRoute.tsx` | ~30 |
| `src/components/auth/PlanGate.tsx` | ~50 |
| `src/components/common/Toast.tsx` | ~80 |
| `src/components/common/UpgradePrompt.tsx` | ~50 |
| `src/components/common/StepIndicator.tsx` | ~40 |
| `src/components/layout/UserMenu.tsx` | ~80 |
| `src/pages/LoginPage.tsx` | ~200 |
| `src/pages/RegisterPage.tsx` | ~400 |
| `src/pages/SettingsPage.tsx` | ~500 |
| `src/pages/TwinCreatePage.tsx` | ~400 |
| `src/pages/TwinDetailPage.tsx` | ~300 |
| `src/pages/TwinSettingsPage.tsx` | ~200 |
| `src/__tests__/` (8 test files) | ~600 |

### Frontend — Modified Files (6)
| File | Change |
|---|---|
| `src/App.tsx` | Auth routes, new routes, PlanGate wrappers |
| `src/types/index.ts` | Add exports |
| `src/api/rest.ts` | Auth headers, 401 interceptor |
| `src/api/websocket.ts` | Token in URL |
| `src/components/layout/Header.tsx` | Dynamic tenant, plan badge, UserMenu |
| `src/components/layout/Sidebar.tsx` | Plan-aware items, dynamic profile |
| `src/components/dashboard/DashboardPage.tsx` | Plan limits, upgrade CTA |

---

## Verification Strategy

After each task group, run the appropriate verification skill:

1. **Tasks 1-10 (Backend):** `/verify-go` → build, vet, test, race
2. **Tasks 11-19 (Frontend):** `/verify-frontend` → TypeScript, build, test
3. **Task 20 (Security):** `/code-review` on all changes
4. **Task 21-22 (Integration):** `/test-integration`
5. **Task 26 (Final):** All verification skills + manual walkthrough

### Manual Verification Checklist
- [ ] Register as new tenant with Basic plan → see 2 twin limit
- [ ] Register as new tenant with Pro plan → see 4 twin limit, metrics, alerts
- [ ] Register as new tenant with Pro+ plan → see 6 twin limit, timeline, intel
- [ ] Attempt to register as Enterprise → blocked (creatable: false)
- [ ] Create Paryty Twin via wizard → see twin in dashboard
- [ ] Create sub-user with viewer role → sub-user can only read
- [ ] Create sub-user with operator role → sub-user can read + acknowledge alerts
- [ ] Admin can change sub-user permissions
- [ ] Basic user cannot access /intel or /timeline → sees upgrade prompt
- [ ] API key creation → shown once, copyable
- [ ] Session expiry → silent refresh → if fails, redirect to login
- [ ] Plan upgrade → new features become available
- [ ] Audit logs recorded for all admin actions
