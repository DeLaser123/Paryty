# Checkpoint 1 Gap Execution Plan

## Context

The user wants to execute all 14 gaps in `docs/Checkpoint_1_Gaps.md` sequentially, with per-item verification gates. After each gap is implemented, the user must physically test it and approve before we proceed to the next.

**Execution order follows the document**: Step 1 (8.1, 8.5, 8.6) → Step 2 (8.2, 8.3, 8.4) → Step 3 (8.7, 8.8, 8.9) → Step 4 (8.10, 8.11) → Step 5 (5.1, 5.2, 5.3).

---

## Gap 8.1: PostgreSQL Row-Level Security

**Status:** PARTIALLY IMPLEMENTED — RLS is already enabled in `schema.go` (lines 204-253) for `paryty_twins`, `agent_assignments`, `api_keys`, and `agent_registrations`. The gap document's approach has been partially followed.

**What exists:**
- RLS enabled via `DO $$` blocks for 4 tables
- Policies created: `tenant_isolation_paryty_twins`, `tenant_isolation_agent_assignments`, `tenant_isolation_api_keys`, `tenant_isolation_agent_registrations`
- All use `current_setting('app.current_tenant_id')::uuid`

**What is missing (per the gap doc):**
1. `audit_log` table RLS — the gap asks for it but schema.go doesn't have it
2. A standalone `EnsureRLSPolicies()` function — gap asks for one but RLS is embedded in `createPhase8Tables`
3. `SET LOCAL app.current_tenant_id` middleware in REST (`rest.go`) and gRPC (`grpc_adapter.go`)
4. Integration tests in `cluster/internal/controlplane/rls_test.go`

### Implementation Steps

1. **Add RLS for `audit_log`** to `cluster/internal/controlplane/schema.go`:
   - Add `DO $$` block for `ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY`
   - Add policy `tenant_isolation_audit` on `audit_log`

2. **Create `EnsureRLSPolicies()` function** in `cluster/internal/controlplane/schema.go`:
   - Extracts the RLS DDL into a separate function (or wrapper) per the gap doc
   - Idempotent — safe to call on every startup

3. **Add RLS session variable middleware**:
   - In `cluster/internal/api/query/rest.go`: Add middleware that runs `SET LOCAL app.current_tenant_id = $1` on each request using the tenant from JWT claims
   - In `cluster/internal/api/ingestion/grpc_adapter.go`: Add interceptor that sets the session variable on gRPC requests

4. **Write integration tests** in `cluster/internal/controlplane/rls_test.go`:
   - `TestRLS_TenantIsolation` — Tenant A cannot read Tenant B's twins
   - `TestRLS_WriteIsolation` — Tenant A cannot write to Tenant B's twin
   - `TestRLS_AuditIsolation` — Tenant A cannot read Tenant B's audit logs

### Files to Modify
- `cluster/internal/controlplane/schema.go`
- `cluster/internal/api/query/rest.go`
- `cluster/internal/api/ingestion/grpc_adapter.go`
- `cluster/internal/controlplane/rls_test.go` (new)

### Verification Command
```
cd cluster; go test -run TestRLS -v ./internal/controlplane/
```

---

## Gap 8.5: Agent Registration End-to-End

**Status:** IMPLEMENTATION EXISTS — `RegisterAgent` handler in `grpc_adapter.go` (lines 206-325) is fully functional with twin assignment, identity resolution, and command dispatch.

**What is missing:** Integration tests per the gap doc.

### Implementation Steps
1. Create `cluster/internal/auth/agent_registration_test.go`
2. Write tests:
   - `TestAgentRegistration_FullFlow` — create twin → API key → register agent → verify DB record
   - `TestAgentRegistration_TwinNotFound` — nonexistent twin returns error
   - `TestAgentRegistration_TwinSuspended` — suspended twin rejects registration
   - `TestAgentRegistration_AgentLimit` — plan limit enforced

### Files to Modify
- `cluster/internal/auth/agent_registration_test.go` (new)

### Verification Command
```
cd cluster; go test -run TestAgentRegistration -v ./internal/auth/
```

---

## Gap 8.6: mTLS Enforcement

**Status:** TLS helpers exist in `tls.go` with tests. No `RequireMTLS()` function or ingestion startup wiring.

### Implementation Steps
1. Add `RequireMTLS()` function to `cluster/internal/security/tls.go`
2. Wire mTLS check in `cluster/cmd/ingestion/main.go` startup
3. Add tests to `cluster/internal/security/tls_test.go`:
   - `TestRequireMTLS_Enabled` / `TestRequireMTLS_Disabled`
   - `TestServerTLS_ValidCerts` (TLS 1.3 min version)
   - `TestServerTLS_MissingFiles`

### Files to Modify
- `cluster/internal/security/tls.go`
- `cluster/internal/security/tls_test.go`
- `cluster/cmd/ingestion/main.go`

### Verification Command
```
cd cluster; go test -run TestTLS -v ./internal/security/
```

---

## Gap 8.2: Twin CRUD Lifecycle Tests

**Status:** Tests-only. DO NOT modify existing code. `TwinHandler` is fully implemented.

### Implementation Steps
1. Create `cluster/internal/auth/twin_lifecycle_test.go`
2. Write tests: Create, Get, Update, Delete, TenantIsolation, PlanLimitEnforcement

### Verification Command
```
cd cluster; go test -run TestTwinLifecycle -v ./internal/auth/
```

---

## Gap 8.3: Quota Enforcement Tests

**Status:** Tests-only. `RequireTwinQuota`, `RequireAgentQuota`, and `CheckQuota` exist.

### Implementation Steps
1. Create `cluster/internal/plan/quota_test.go`
2. Write tests: IngestionBytesPerDay, QueryRequestsPerMinute, ResetAfterWindow, DifferentPlans

### Verification Command
```
cd cluster; go test -run TestQuota -v ./internal/plan/
```

---

## Gap 8.4: Rate Limiting Load Tests

**Status:** `RateLimiter` has unit tests already. Need load/stress tests.

### Implementation Steps
1. Create `cluster/internal/api/ingestion/ratelimit_load_test.go`
2. Write tests: BurstTraffic, ConcurrentTenants, WindowReset, DifferentEndpoints

### Verification Command
```
cd cluster; go test -run TestRateLimit -race -v ./internal/api/ingestion/
```

---

## Gaps 8.7-8.8: Kubernetes Manifests & Helm Charts

New deployment infrastructure. Will create `deploy/kubernetes/` and `deploy/helm/` structures.

---

## Gap 8.9: CI/CD Pipeline

Update `.github/workflows/ci.yml` and `cd.yml`.

---

## Gap 8.10: Load Testing

Create `tests/load/` directory with agent simulator and scenarios.

---

## Gap 8.11: Self-Monitoring

Implement in `cluster/internal/monitoring/self_monitor.go`, `alert_rules.go`, `dashboard.go`.

---

## Gaps 5.1-5.3: Frontend

Timeline Replay UI, Twin Management UI, Sub-User Management UI.

---

## Verification Protocol

For each gap:
1. Implement the gap
2. Run the verification command
3. Present execution report to user
4. Provide unambiguous physical test instructions
5. Wait for user approval before proceeding to next gap
