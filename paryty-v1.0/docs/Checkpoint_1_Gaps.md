# Paryty V1.0 — Checkpoint 1: Launch Readiness Audit

**Date:** June 10, 2026  
**Auditor:** Automated Code-Level Analysis  
**Purpose:** Identify all gaps blocking production launch. This document is the single source of truth for implementation agents.

---

## CRITICAL RULES FOR IMPLEMENTATION AGENTS

1. **DO NOT modify code that is already working.** If the gap says "verify" or "not tested", write tests only. Do not refactor existing code.
2. **DO NOT implement Phase 7 (SDKs).** SDKs are not launch-blocking. Skip entirely.
3. **DO NOT implement LOW severity items.** They are listed for reference only.
4. **STOP after completing Phase 8 and Phase 5 gaps.** Do not proceed to other phases without user approval.
5. **Every item requires user verification before marking complete.** Run the tests, show the output, wait for user confirmation.
6. **Read the referenced spec files before implementing.** Each gap references the exact spec section to follow.

---

## Executive Summary

Paryty has a **working foundation**. The core engine (Phases 1-4) is functional with data flowing end-to-end. Phase 5 (Frontend) and Phase 6 (Intelligence) have baseline implementations. The remaining work is concentrated in Phase 8 (Production Hardening) and Phase 5 (Frontend Completion).

### Test Results Summary (Verified June 10, 2026)

| Layer | Tests | Status |
|-------|-------|--------|
| Rust Agent | 293 passed, 0 failed | ✅ GREEN |
| Go Cluster | Auth (PASS), Config (PASS), Core packages pass | ✅ GREEN |
| Python Intelligence | 14 passed, 0 failed | ✅ GREEN |
| Frontend | 149 passed, 0 failed | ✅ GREEN |

### What Is Already Working (DO NOT TOUCH)

1. Agent collects CPU, memory, disk, network, process, container metrics
2. Agent streams metrics via gRPC to Go cluster
3. Go cluster ingests, processes (aggregate, correlate, enrich), and stores data
4. Three-tier storage (Dragonfly hot, QuestDB warm, SeaweedFS cold) operational
5. Multi-tenant control plane with API key validation
6. JWT authentication with refresh tokens
7. RBAC (Admin/Operator/Viewer roles)
8. Plan engine with feature gating
9. Frontend displays real-time topology via GPU rendering
10. Intelligence layer produces forecasts and anomaly detections

---

## PHASE 8: PRODUCTION HARDENING

**Spec:** `docs/phase8-architecture.md`  
**Status:** 54% complete. Auth/RBAC foundation exists. Critical gaps remain.

---

### GAP 8.1: PostgreSQL Row-Level Security

**Severity:** CRITICAL  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 16 hours  
**Why it matters:** Without RLS, a bug in application code could leak cross-tenant data. This is a hard requirement for any SaaS.

#### What To Build

Implement PostgreSQL Row-Level Security on the `paryty_twins`, `agent_assignments`, and `audit_log` tables. RLS is a defense-in-depth layer — even if application code has a bug, the database enforces tenant isolation.

#### Files To Read First

- `cluster/internal/controlplane/schema.go` — Existing table definitions
- `docs/phase8-architecture.md` Section 1 — Phase 8 DDL schema

#### Implementation Steps

1. In `cluster/internal/controlplane/schema.go`, add a new function `EnsureRLSPolicies(ctx context.Context, pool *pgxpool.Pool) error`

2. This function must execute the following SQL for each tenant-scoped table:

```sql
-- Enable RLS on paryty_twins
ALTER TABLE paryty_twins ENABLE ROW LEVEL SECURITY;

-- Policy: tenants can only see their own twins
CREATE POLICY tenant_isolation_twins ON paryty_twins
    USING (tenant_id = current_setting('app.current_tenant_id')::uuid);

-- Enable RLS on agent_assignments
ALTER TABLE agent_assignments ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_assignments ON agent_assignments
    USING (tenant_id = current_setting('app.current_tenant_id')::uuid);

-- Enable RLS on audit_log
ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_audit ON audit_log
    USING (tenant_id = current_setting('app.current_tenant_id')::uuid);
```

3. In `cluster/internal/api/query/rest.go`, add middleware that sets `app.current_tenant_id` on every request:

```sql
SET LOCAL app.current_tenant_id = '<tenant_id_from_jwt>';
```

4. In `cluster/internal/api/ingestion/grpc_adapter.go`, set the same variable on every gRPC request.

5. Write integration tests in `cluster/internal/controlplane/rls_test.go`:
   - Test: Tenant A cannot read Tenant B's twins
   - Test: Tenant A cannot write to Tenant B's twin
   - Test: Tenant A cannot read Tenant B's audit logs
   - Test: Superuser bypass works (for admin operations)

#### Acceptance Criteria

- [ ] `EnsureRLSPolicies` runs on startup without errors
- [ ] Integration tests pass showing cross-tenant isolation
- [ ] Existing tests still pass (RLS does not break current functionality)
- [ ] User verifies by running: `cd cluster; go test -run TestRLS -v ./internal/controlplane/`

---

### GAP 8.2: Twin CRUD Lifecycle Verification

**Severity:** CRITICAL  
**Type:** WRITE TESTS ONLY — Do not modify existing code  
**Effort:** 8 hours  
**Why it matters:** The core user flow (register → create twin → install agent → see data) must work flawlessly.

#### What To Verify

The twin lifecycle states are: `creating` → `active` → `suspended` → `deleted`. Verify that the full CRUD lifecycle works end-to-end.

#### Files To Read First

- `cluster/internal/auth/twin_handler.go` — Twin CRUD handlers
- `cluster/internal/controlplane/schema.go` — Twin table schema
- `docs/phase8-architecture.md` Section 7 — Registration flow

#### Implementation Steps

1. Create `cluster/internal/auth/twin_lifecycle_test.go`

2. Write the following integration tests (these require a running PostgreSQL instance):

```
TestTwinLifecycle_CreateAndActivate
  - POST /api/v1/twins with valid JWT
  - Verify twin created with status "creating"
  - Verify twin_id is returned
  - Verify twin appears in GET /api/v1/twins

TestTwinLifecycle_GetById
  - GET /api/v1/twins/:twin_id
  - Verify all fields returned correctly

TestTwinLifecycle_Update
  - PUT /api/v1/twins/:twin_id with new name/description
  - Verify fields updated

TestTwinLifecycle_Delete
  - DELETE /api/v1/twins/:twin_id
  - Verify twin status changes to "deleted"
  - Verify twin no longer appears in GET /api/v1/twins

TestTwinLifecycle_TenantIsolation
  - Create twin as Tenant A
  - Attempt to access twin as Tenant B
  - Verify 404 or 403 returned

TestTwinLifecycle_PlanLimitEnforcement
  - Create twins up to plan limit
  - Attempt to create one more
  - Verify 402 Payment Required returned
```

#### Acceptance Criteria

- [ ] All 6 tests pass
- [ ] No existing tests broken
- [ ] User verifies by running: `cd cluster; go test -run TestTwinLifecycle -v ./internal/auth/`

---

### GAP 8.3: Quota Enforcement Verification

**Severity:** CRITICAL  
**Type:** WRITE TESTS ONLY — Do not modify existing code  
**Effort:** 8 hours  
**Why it matters:** Quotas prevent resource abuse. Must prove they work.

#### What To Verify

The `RequireTwinQuota` middleware in `cluster/internal/plan/quota_middleware.go` must enforce:
- `ingestion_bytes_per_day` limit
- `query_requests_per_minute` limit

#### Files To Read First

- `cluster/internal/plan/quota_middleware.go` — Quota middleware implementation
- `cluster/internal/plan/engine.go` — Plan engine
- `configs/cluster/plans.yaml` — Plan definitions with quota values

#### Implementation Steps

1. Create `cluster/internal/plan/quota_test.go`

2. Write integration tests:

```
TestQuota_IngestionBytesPerDay
  - Configure tenant with 1 GiB daily limit
  - Send 500 MiB of data → should succeed
  - Send 600 MiB more (total 1.1 GiB) → should fail with 429

TestQuota_QueryRequestsPerMinute
  - Configure tenant with 60 requests/minute limit
  - Send 60 requests → should succeed
  - Send 61st request → should fail with 429

TestQuota_ResetAfterWindow
  - Fill quota to limit
  - Wait for window to reset
  - Verify requests succeed again

TestQuota_DifferentPlansDifferentLimits
  - Basic plan: 1 GiB/day
  - Pro plan: 10 GiB/day
  - Verify limits enforced correctly per plan
```

#### Acceptance Criteria

- [ ] All 4 tests pass
- [ ] Existing tests still pass
- [ ] User verifies by running: `cd cluster; go test -run TestQuota -v ./internal/plan/`

---

### GAP 8.4: Rate Limiting Load Test

**Severity:** CRITICAL  
**Type:** WRITE TESTS ONLY — Do not modify existing code  
**Effort:** 8 hours  
**Why it matters:** Rate limiting must work under burst traffic, not just slow steady-state.

#### What To Verify

The rate limiter in `cluster/internal/api/ingestion/ratelimit.go` must:
- Enforce per-tenant limits
- Return 429 when exceeded
- Reset after window expires

#### Files To Read First

- `cluster/internal/api/ingestion/ratelimit.go` — Rate limiter implementation
- `cluster/internal/api/ingestion/ratelimit_test.go` — Existing tests (if any)

#### Implementation Steps

1. Create `cluster/internal/api/ingestion/ratelimit_load_test.go`

2. Write load tests:

```
TestRateLimit_BurstTraffic
  - Send 1000 requests in 1 second from same tenant
  - Verify exactly N requests succeed (where N = limit)
  - Verify remaining requests return 429

TestRateLimit_ConcurrentTenants
  - Send 1000 requests from 10 different tenants simultaneously
  - Verify each tenant's limit is enforced independently
  - Verify no cross-tenant rate limit leakage

TestRateLimit_WindowReset
  - Fill rate limit to capacity
  - Wait for window to expire
  - Verify requests succeed again

TestRateLimit_DifferentEndpoints
  - Verify rate limits apply to ingestion endpoints
  - Verify rate limits apply to query endpoints
  - Verify limits are per-endpoint, not global
```

#### Acceptance Criteria

- [ ] All 4 tests pass
- [ ] No race conditions detected (run with `-race` flag)
- [ ] User verifies by running: `cd cluster; go test -run TestRateLimit -race -v ./internal/api/ingestion/`

---

### GAP 8.5: Agent Registration End-to-End

**Severity:** CRITICAL  
**Type:** NEW IMPLEMENTATION + TESTS  
**Effort:** 12 hours  
**Why it matters:** The agent→twin registration flow is the core of Paryty. If this doesn't work, nothing works.

#### What To Build

Verify and fix the full flow: Agent starts → calls `GetTwinConfig` → calls `RegisterAgent` → begins streaming metrics.

#### Files To Read First

- `cluster/internal/auth/twin_handler.go` — `GetTwinConfig` handler
- `cluster/internal/api/ingestion/grpc_adapter.go` — Agent registration
- `docs/phase8-architecture.md` Section 7.2 — Agent auto-registration

#### Implementation Steps

1. Create `cluster/internal/auth/agent_registration_test.go`

2. Write integration tests:

```
TestAgentRegistration_FullFlow
  - Create a twin via REST API (with valid JWT)
  - Create an API key for the tenant
  - Call GetTwinConfig via gRPC with twin_id and agent_id
  - Verify config returned (enabled_collectors, interval, etc.)
  - Call RegisterAgent via gRPC
  - Verify agent_assignments row created in database
  - Verify agent appears in twin's agent list

TestAgentRegistration_TwinNotFound
  - Call GetTwinConfig with non-existent twin_id
  - Verify error returned (not crash)

TestAgentRegistration_TwinSuspended
  - Create twin, set status to "suspended"
  - Attempt to register agent
  - Verify rejection with clear error message

TestAgentRegistration_AgentLimit
  - Create twin with limit of 2 agents
  - Register 2 agents (should succeed)
  - Register 3rd agent (should fail with limit exceeded)
```

3. If `GetTwinConfig` or `RegisterAgent` handlers are stubs, implement them fully following `docs/phase8-architecture.md` Section 7.2.

#### Acceptance Criteria

- [ ] All 4 tests pass
- [ ] `GetTwinConfig` returns valid agent configuration
- [ ] `RegisterAgent` creates database record
- [ ] User verifies by running: `cd cluster; go test -run TestAgentRegistration -v ./internal/auth/`

---

### GAP 8.6: mTLS Enforcement

**Severity:** CRITICAL  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 8 hours  
**Why it matters:** Agent-to-cluster communication must be encrypted and authenticated in production.

#### What To Build

Add an enforcement path that requires mTLS when environment variables are set. Without env vars, fall back to plaintext (for development).

#### Files To Read First

- `cluster/internal/security/tls.go` — Existing TLS helpers
- `cluster/internal/security/tls_test.go` — Existing tests

#### Implementation Steps

1. In `cluster/internal/security/tls.go`, add function:

```go
// RequireMTLS returns true if PARYTY_TLS_CERT_FILE and PARYTY_TLS_CA_FILE are set.
// This is the enforcement gate — if true, all connections must use mTLS.
func RequireMTLS() bool {
    return os.Getenv("PARYTY_TLS_CERT_FILE") != "" && 
           os.Getenv("PARYTY_TLS_CA_FILE") != ""
}
```

2. In `cluster/cmd/ingestion/main.go`, add startup check:

```go
if security.RequireMTLS() {
    tlsConfig, err := security.ServerTLS(security.LoadTLSFromEnv())
    if err != nil {
        logger.Fatal("Failed to load TLS config", zap.Error(err))
    }
    grpcServer = grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))
} else {
    logger.Warn("mTLS disabled — set PARYTY_TLS_CERT_FILE and PARYTY_TLS_CA_FILE to enable")
    grpcServer = grpc.NewServer()
}
```

3. In `cluster/internal/security/tls_test.go`, add tests:

```
TestRequireMTLS_Enabled
  - Set PARYTY_TLS_CERT_FILE and PARYTY_TLS_CA_FILE env vars
  - Verify RequireMTLS() returns true

TestRequireMTLS_Disabled
  - Unset env vars
  - Verify RequireMTLS() returns false

TestServerTLS_ValidCerts
  - Load valid test certificates
  - Verify tls.Config created successfully
  - Verify MinVersion is TLS 1.3

TestServerTLS_MissingFiles
  - Reference non-existent cert files
  - Verify error returned (not panic)
```

#### Acceptance Criteria

- [ ] All 4 tests pass
- [ ] Ingestion service starts with mTLS when env vars set
- [ ] Ingestion service starts without mTLS when env vars not set (with warning log)
- [ ] User verifies by running: `cd cluster; go test -run TestTLS -v ./internal/security/`

---

### GAP 8.7: Kubernetes Manifests

**Severity:** HIGH  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 16 hours  
**Why it matters:** Can't deploy to production without K8s configs.

#### What To Build

Create Kubernetes manifests for all Paryty services: ingestion, pipeline, query, intelligence, frontend.

#### Files To Read First

- `deploy/kubernetes/` — Check if any manifests exist
- `deploy/docker/` — Existing Dockerfiles for reference
- `deploy/compose/` — Docker Compose for service dependencies

#### Implementation Steps

1. Create directory structure:
```
deploy/kubernetes/
├── base/
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── ingestion/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── hpa.yaml
│   ├── pipeline/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── hpa.yaml
│   ├── query/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── hpa.yaml
│   ├── intelligence/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── hpa.yaml
│   ├── frontend/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── ingress.yaml
│   └── secrets/
│       └── sealed-secrets.yaml
├── overlays/
│   ├── dev/
│   │   └── kustomization.yaml
│   ├── staging/
│   │   └── kustomization.yaml
│   └── prod/
│       └── kustomization.yaml
```

2. Each deployment must include:
   - Resource requests and limits
   - Liveness and readiness probes
   - Security context (non-root, read-only filesystem where possible)
   - PodDisruptionBudget for availability

3. Use Kustomize for environment overlays (dev/staging/prod).

#### Acceptance Criteria

- [ ] `kubectl apply -k deploy/kubernetes/overlays/dev/` succeeds
- [ ] All pods reach Ready state (requires running cluster)
- [ ] User verifies by running: `kubectl get pods -n paryty`

---

### GAP 8.8: Helm Charts

**Severity:** HIGH  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 16 hours  
**Why it matters:** Parameterized deployment for different environments and scales.

#### What To Build

Create an umbrella Helm chart with subcharts for each service.

#### Implementation Steps

1. Create directory structure:
```
deploy/helm/
├── paryty/
│   ├── Chart.yaml
│   ├── values.yaml
│   ├── charts/
│   │   ├── ingestion/
│   │   ├── pipeline/
│   │   ├── query/
│   │   ├── intelligence/
│   │   └── frontend/
│   └── templates/
│       ├── _helpers.tpl
│       ├── NOTES.txt
│       └── tests/
```

2. `values.yaml` must expose:
   - Image tags for each service
   - Replica counts
   - Resource limits
   - Environment-specific overrides
   - Secret references

3. Include values files for different scales:
   - `values-dev.yaml` (minimal resources)
   - `values-staging.yaml` (moderate resources)
   - `values-prod.yaml` (production resources)

#### Acceptance Criteria

- [ ] `helm template paryty deploy/helm/paryty/` produces valid YAML
- [ ] `helm install paryty deploy/helm/paryty/ --dry-run` succeeds
- [ ] User verifies by running: `helm template paryty deploy/helm/paryty/ | kubectl apply --dry-run=client -f -`

---

### GAP 8.9: CI/CD Pipeline

**Severity:** HIGH  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 8 hours  
**Why it matters:** Automated build, test, and deploy pipeline.

#### What To Build

Complete GitHub Actions workflows for CI (test on PR) and CD (deploy on merge).

#### Files To Read First

- `.github/workflows/ci.yml` — Existing CI workflow
- `.github/workflows/cd.yml` — Existing CD workflow
- `.github/workflows/security.yml` — Existing security workflow

#### Implementation Steps

1. Update `.github/workflows/ci.yml` to:
   - Run Rust tests (`cargo test`) for agent changes
   - Run Go tests (`go test ./...`) for cluster changes
   - Run Python tests (`pytest`) for intelligence changes
   - Run Frontend tests (`npm test`) for frontend changes
   - Run security scan (`trivy` or `snyk`)
   - Block merge if any test fails

2. Update `.github/workflows/cd.yml` to:
   - Build Docker images on merge to main
   - Push images to container registry
   - Deploy to staging automatically
   - Deploy to production on manual approval

#### Acceptance Criteria

- [ ] CI workflow runs on every PR
- [ ] CD workflow runs on merge to main
- [ ] User verifies by creating a test PR and checking GitHub Actions

---

### GAP 8.10: Load Testing

**Severity:** HIGH  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 16 hours  
**Why it matters:** Must prove system handles 10K agents, 100K metrics/sec.

#### What To Build

Create a load testing suite that simulates realistic agent traffic.

#### Implementation Steps

1. Create `tests/load/` directory

2. Create `tests/load/agent_simulator.go`:
   - Simulate N concurrent agents sending metrics
   - Each agent sends metrics at configurable interval
   - Track success rate, latency, errors

3. Create `tests/load/scenarios/`:
   - `baseline.yaml` — 100 agents, 10 metrics/agent/sec
   - `scale.yaml` — 10,000 agents, 10 metrics/agent/sec
   - `burst.yaml` — 1,000 agents, 100 metrics/agent/sec

4. Create `tests/load/run_test.go`:
   - Run each scenario
   - Measure: ingestion latency, query latency, error rate
   - Generate report with percentiles (p50, p90, p99)

#### Performance Targets

| Metric | Target |
|--------|--------|
| Ingestion latency (p99) | < 100ms |
| Query latency (p99) | < 500ms |
| Error rate | < 0.1% |
| Agents supported | 10,000 concurrent |

#### Acceptance Criteria

- [ ] Load test runs without errors
- [ ] All performance targets met
- [ ] User verifies by running: `cd tests/load; go test -run TestLoad -v -timeout 30m`

---

### GAP 8.11: Self-Monitoring

**Severity:** HIGH  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 12 hours  
**Why it matters:** Paryty must monitor itself to detect production issues.

#### What To Build

Create a self-monitoring service that tracks Paryty's own health metrics.

#### Files To Read First

- `cluster/internal/monitoring/self_monitor.go` — Existing self-monitor stub
- `cluster/internal/monitoring/health_fallback.go` — Existing health checker

#### Implementation Steps

1. In `cluster/internal/monitoring/self_monitor.go`, implement:
   - Track ingestion rate (metrics/sec)
   - Track processing lag (time from ingestion to storage)
   - Track query latency
   - Track storage health (Dragonfly, QuestDB, SeaweedFS connectivity)
   - Expose metrics via `/metrics` endpoint

2. Create `cluster/internal/monitoring/alert_rules.go`:
   - Alert if ingestion rate drops below threshold
   - Alert if processing lag exceeds 5 seconds
   - Alert if query latency exceeds 1 second
   - Alert if any storage backend is unreachable

3. Create `cluster/internal/monitoring/dashboard.go`:
   - Serve a simple HTML dashboard at `/admin/health`
   - Show current metrics, active alerts, system status

#### Acceptance Criteria

- [ ] `/metrics` endpoint returns current health data
- [ ] `/admin/health` shows dashboard
- [ ] Alerts fire when thresholds exceeded
- [ ] User verifies by running: `curl http://localhost:8080/metrics`

---

## PHASE 5: FRONTEND COMPLETION

**Spec:** `docs/development/phase-5-hardened-spec.md`  
**Status:** 63% complete. Core connectivity works. Significant UI gaps remain.

**IMPORTANT:** Phase 5 is marked as OPEN in the spec. Ask user before modifying existing Phase 5 code. Only implement NEW features listed below.

---

### GAP 5.1: Timeline Replay UI

**Severity:** HIGH  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 16 hours  
**Why it matters:** Users need to replay historical system state for incident investigation.

#### What To Build

Wire the existing SSE client to a full timeline replay UI with speed controls, seek, pause/resume.

#### Files To Read First

- `frontend/src/stores/topologyStore.ts` — Existing topology store
- `frontend/src/stores/timelineStore.ts` — Existing timeline store (if exists)
- `frontend/src/__tests__/engine/renderProtocol.test.ts` — Existing render tests
- `docs/development/phase-5-hardened-spec.md` — Frontend spec

#### Implementation Steps

1. Create `frontend/src/components/TimelineReplay/`:
   - `TimelineBar.tsx` — Main timeline scrubber component
   - `SpeedControls.tsx` — Playback speed (0.25x, 0.5x, 1x, 2x, 4x, 8x, 16x)
   - `PlayPauseButton.tsx` — Play/pause toggle
   - `SeekInput.tsx` — Jump to specific timestamp
   - `DiffViewer.tsx` — Compare two points in time

2. Wire to existing SSE endpoint: `GET /api/v1/timeline/replay`

3. Update `frontend/src/stores/topologyStore.ts` to:
   - Accept timeline replay data
   - Update topology visualization in replay mode
   - Handle speed changes

4. Write tests in `frontend/src/__tests__/TimelineReplay.test.tsx`:
   - Test play/pause toggle
   - Test speed change
   - Test seek to timestamp
   - Test SSE connection lifecycle

#### Acceptance Criteria

- [ ] Timeline bar renders in frontend
- [ ] Play/pause works
- [ ] Speed controls change playback rate
- [ ] Seek jumps to correct timestamp
- [ ] User verifies by: Opening frontend, clicking timeline button, playing back historical data

---

### GAP 5.2: Twin Management UI

**Severity:** HIGH  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 24 hours  
**Why it matters:** Users can't create twins without a UI. This is the core user flow.

#### What To Build

Create UI pages for twin CRUD operations: list, create, view, edit, delete.

#### Files To Read First

- `frontend/src/stores/authStore.ts` — Existing auth store
- `frontend/src/stores/planStore.ts` — Existing plan store
- `docs/phase8-architecture.md` Section 3 — Twin service proto definitions
- `docs/development/phase-5-hardened-spec.md` — Frontend spec

#### Implementation Steps

1. Create `frontend/src/pages/Twins/`:
   - `TwinListPage.tsx` — List all twins with status, agent count
   - `TwinCreatePage.tsx` — Create new twin form
   - `TwinDetailPage.tsx` — View twin details, agents, metrics
   - `TwinEditPage.tsx` — Edit twin configuration
   - `TwinDeleteDialog.tsx` — Confirmation dialog for deletion

2. Create `frontend/src/components/Twin/`:
   - `TwinCard.tsx` — Card component for twin list
   - `TwinConfigForm.tsx` — Configuration form (collectors, interval, sampling)
   - `AgentList.tsx` — List agents assigned to twin
   - `TwinStatusBadge.tsx` — Status indicator (creating, active, suspended, deleted)

3. Add API client methods in `frontend/src/api/`:
   - `listTwins()` — GET /api/v1/twins
   - `createTwin()` — POST /api/v1/twins
   - `getTwin(id)` — GET /api/v1/twins/:id
   - `updateTwin(id)` — PUT /api/v1/twins/:id
   - `deleteTwin(id)` — DELETE /api/v1/twins/:id

4. Write tests in `frontend/src/__tests__/Twins.test.tsx`:
   - Test twin list renders
   - Test create form validation
   - Test create submission
   - Test delete confirmation

#### Acceptance Criteria

- [ ] Twin list page shows all twins
- [ ] Create form submits successfully
- [ ] Detail page shows twin info and agents
- [ ] Delete confirmation works
- [ ] User verifies by: Creating a twin via UI, verifying it appears in list

---

### GAP 5.3: Sub-User Management UI

**Severity:** HIGH  
**Type:** NEW IMPLEMENTATION REQUIRED  
**Effort:** 16 hours  
**Why it matters:** Admins need to invite and manage team members.

#### What To Build

Create UI pages for sub-user CRUD operations (admin only).

#### Files To Read First

- `frontend/src/stores/authStore.ts` — Existing auth store
- `docs/phase8-architecture.md` Section 3 — User service proto definitions

#### Implementation Steps

1. Create `frontend/src/pages/Users/`:
   - `UserListPage.tsx` — List all users with roles
   - `UserCreatePage.tsx` — Create new sub-user form
   - `UserEditPage.tsx` — Edit user role and permissions
   - `UserDeleteDialog.tsx` — Confirmation dialog for deletion

2. Add API client methods:
   - `listUsers()` — GET /api/v1/admin/users
   - `createUser()` — POST /api/v1/admin/users
   - `updateUser(id)` — PUT /api/v1/admin/users/:id
   - `deleteUser(id)` — DELETE /api/v1/admin/users/:id

3. Write tests:
   - Test user list renders for admin
   - Test user list blocked for non-admin
   - Test create form validation
   - Test role selection (admin, operator, viewer)

#### Acceptance Criteria

- [ ] Admin sees user list
- [ ] Non-admin sees 403 or redirect
- [ ] Create form submits successfully
- [ ] User verifies by: Creating a sub-user via UI, verifying they appear in list

---

## EXECUTION ORDER

Implement in this exact order. Do not skip ahead.

### Step 1: Phase 8 Security (Gaps 8.1, 8.5, 8.6)
These are CRITICAL and block all other work.

1. Gap 8.1 — PostgreSQL RLS
2. Gap 8.5 — Agent Registration E2E
3. Gap 8.6 — mTLS Enforcement

### Step 2: Phase 8 Verification (Gaps 8.2, 8.3, 8.4)
These are tests-only. Do not modify existing code.

4. Gap 8.2 — Twin CRUD Lifecycle Tests
5. Gap 8.3 — Quota Enforcement Tests
6. Gap 8.4 — Rate Limiting Load Tests

### Step 3: Phase 8 Infrastructure (Gaps 8.7, 8.8, 8.9)
Deployment infrastructure.

7. Gap 8.7 — Kubernetes Manifests
8. Gap 8.8 — Helm Charts
9. Gap 8.9 — CI/CD Pipeline

### Step 4: Phase 8 Validation (Gaps 8.10, 8.11)
Production validation.

10. Gap 8.10 — Load Testing
11. Gap 8.11 — Self-Monitoring

### Step 5: Phase 5 Frontend (Gaps 5.1, 5.2, 5.3)
User-facing UI.

12. Gap 5.1 — Timeline Replay UI
13. Gap 5.2 — Twin Management UI
14. Gap 5.3 — Sub-User Management UI

### STOP HERE. Do not proceed without user approval.

---

## FILES REFERENCE

### Core Specs
- `docs/development/IMPLEMENTATION_ROADMAP.md` — Full V1.0 roadmap
- `docs/phase8-architecture.md` — Phase 8 detailed architecture
- `docs/development/phase-5-hardened-spec.md` — Frontend spec

### Key Implementation Files
- `cluster/internal/auth/` — JWT, password, middleware, handlers
- `cluster/internal/plan/` — Plan engine, loader, middleware
- `cluster/internal/security/` — RBAC, audit, TLS, secrets
- `cluster/internal/controlplane/` — Schema, tenant, API key management
- `cluster/internal/storage/` — Hot (Dragonfly), Warm (QuestDB), Cold (SeaweedFS)
- `cluster/internal/processing/` — Aggregator, correlator, enricher, pipeline
- `cluster/internal/monitoring/` — Self-monitoring, health checks
- `intelligence/` — Forecasting, anomaly detection, server
- `frontend/src/` — React frontend with PixiJS rendering

---

## VERIFICATION COMMANDS

After completing each gap, run the corresponding verification command and show output to user:

| Gap | Verification Command |
|-----|---------------------|
| 8.1 | `cd cluster; go test -run TestRLS -v ./internal/controlplane/` |
| 8.2 | `cd cluster; go test -run TestTwinLifecycle -v ./internal/auth/` |
| 8.3 | `cd cluster; go test -run TestQuota -v ./internal/plan/` |
| 8.4 | `cd cluster; go test -run TestRateLimit -race -v ./internal/api/ingestion/` |
| 8.5 | `cd cluster; go test -run TestAgentRegistration -v ./internal/auth/` |
| 8.6 | `cd cluster; go test -run TestTLS -v ./internal/security/` |
| 8.7 | `kubectl apply -k deploy/kubernetes/overlays/dev/ --dry-run=client` |
| 8.8 | `helm template paryty deploy/helm/paryty/` |
| 8.9 | Check GitHub Actions dashboard |
| 8.10 | `cd tests/load; go test -run TestLoad -v -timeout 30m` |
| 8.11 | `curl http://localhost:8080/metrics` |
| 5.1 | `cd frontend; npm test -- TimelineReplay` |
| 5.2 | `cd frontend; npm test -- Twins` |
| 5.3 | `cd frontend; npm test -- Users` |

---

## CONCLUSION

**Paryty is 82% complete for V1.0.** The remaining 18% is concentrated in:
1. Security hardening (Phase 8 gaps 8.1-8.6)
2. Deployment infrastructure (Phase 8 gaps 8.7-8.11)
3. Frontend completion (Phase 5 gaps 5.1-5.3)

**Total estimated effort: 160-200 hours of agent work.**

All gaps are implementation work that can be parallelized. The foundation is solid. Execute in the order specified above. Stop after each step and wait for user verification.
