# Paryty v1.0 — Gap Resolution Guide

**Date:** 2026-06-20  
**Scope:** 44 gaps across 8 categories — exact locations, root causes, and step-by-step resolutions  
**Evidence Basis:** Source code line-by-line inspection, 3 parallel research sub-tasks, gitleaks + semgrep + pip-audit + npm audit + Go vet + wapiti scans  
**Target:** Close all gaps → cloud launch ready

---

## STATUS KEY

| Symbol | Meaning |
|--------|---------|
| 🔴 | NOT STARTED — gap present, no work done |
| 🟡 | PARTIAL — some work done, incomplete |
| 🟢 | RESOLVED — verified fixed by tool output or code inspection |
| ⚪ | FALSE POSITIVE — reported in audit but not actually a gap |

---

## CATEGORY A: SECURITY — 14 Gaps

---

### SEC-01 🔴 TLS Disabled on All Transport Paths

**Severity:** CRITICAL  
**Discovered by:** Enterprise Readiness Audit #8-#14, security code inspection  
**Blocks:** Multi-tenant data confidentiality, SOC2/GDPR compliance, any production use

**Exact Locations:**

| File | Line(s) | Current Code |
|------|---------|-------------|
| `deploy/compose/docker-compose.dev.yaml` | 28-36 | Redpanda `PLAINTEXT://0.0.0.0:9092`, `PLAINTEXT://redpanda:9092` (5 plaintext listeners) |
| `deploy/compose/docker-compose.dev.yaml` | 160, 189 | `DATABASE_URL: postgres://...?sslmode=disable` (2 occurrences) |
| `deploy/compose/docker-compose.dev.yaml` | 228 | `PARYTY_SEAWEEDFS_USE_SSL: "false"` |
| `deploy/compose/docker-compose.dev.yaml` | 211 | Healthcheck: `wget -qO- http://localhost:8080/health` |
| `deploy/compose/docker-compose.dev.yaml` | 158 | `"${PARYTY_PORT_INGESTION}:8080  # gRPC ingestion"` (plaintext gRPC) |
| `cluster/cmd/query/main.go` | 137 | `queryPort := flag.String("port", "8080", "Query service HTTP port")` |
| `cluster/cmd/query/main.go` | 646-657 | `http.Server{}` with `ListenAndServe()` — zero TLS config, no cert files |
| `deploy/docker/Dockerfile.cluster` | 39 | `EXPOSE 8080` — plain port, no cert copy, no TLS args |
| `deploy/docker/Dockerfile.cluster` | 42 | `ENTRYPOINT ["/app/service"]` — no `--tls-cert` / `--tls-key` flags |
| `cluster/internal/security/tls.go` | 1-89 | TLS config loading code EXISTS (`parseTLSCert`, `NewTLSConfig`, `NewMutualTLSConfig`) but NEVER CALLED by main.go |
| `cluster/internal/security/middleware.go` | 262 | HSTS header SET: `"max-age=63072000; includeSubDomains; preload"` — but ignored by browsers on HTTP |

**Root Cause:** `main.go` unconditionally spins up `http.Server{Addr: port}` with no TLS. All infrastructure services configured as PLAINTEXT. TLS code exists in `security/tls.go` but is dead code.

**Resolution Steps:**
1. Generate self-signed certs for dev: `openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes -subj "/CN=localhost"`
2. In `main.go`, replace `ListenAndServe()` with `ListenAndServeTLS("cert.pem", "key.pem")` or use `tls.Config` from `security.NewTLSConfig()`
3. In `docker-compose.dev.yaml`: change Redpanda PLAINTEXT→SSL listeners, PostgreSQL `sslmode=disable`→`sslmode=require`, SeaweedFS `USE_SSL: "false"`→`"true"`
4. In `Dockerfile.cluster`: copy certs, add `--tls-cert` and `--tls-key` flags
5. Add TLS handshake timeout: `srv.ReadTimeout`, `srv.WriteTimeout` to 30s
6. Test: `curl -k https://localhost:8080/healthz` must return 200

---

### SEC-02 🟡 RLS Middleware Not on All Database-Accessing Routes

**Severity:** HIGH  
**Discovered by:** Checkpoint 8.1, code inspection  
**Blocks:** Tenant data isolation guarantees

**Exact Locations:**

| Route Group | File:Line | Current State |
|-------------|-----------|---------------|
| `/api/v1` auth group | `cmd/query/main.go:408` | `authd.Use(queryService.RLSTenantMiddleware())` — WIRED ✅ |
| Intel API group | `cmd/query/main.go:612-621` | `intelGroup.Use(auth.GinJWTAuth(...))` — RLS **MISSING** 🔴 |
| SSE routes | `cmd/query/main.go:597-599` | `sseAuth = auth.GinJWTAuthFlexible(...)` — RLS **MISSING** 🔴 |

```go
// main.go lines 612-621 — Intel group: NO RLSTenantMiddleware
intelGroup := router.Group("/api/v1")
intelGroup.Use(auth.GinJWTAuth(tokenManager))
intelGroup.Use(plan.GinPlanInfoInjector(planEngine))
intelGroup.Use(plan.GinFeatureGate(planEngine, "paryty_intel"))
intelHandlers.RegisterRoutes(intelGroup)

// main.go lines 597-599 — SSE routes: NO RLSTenantMiddleware
router.GET("/api/v1/timeline/replay", sseAuth, sseHandler.HandleTimeline)
router.GET("/api/v1/metrics/:agent_id/stream", sseAuth, sseHandler.HandleMetricsStream)
router.GET("/api/v1/events/stream", sseAuth, sseHandler.HandleEventStream)
```

**Resolution Steps:**
1. Add `intelGroup.Use(queryService.RLSTenantMiddleware())` between lines 616-617 in main.go
2. Add `router.Use(queryService.RLSTenantMiddleware())` before SSE route declarations at line ~597
3. Verify: write a test that attempts to query Intel/SSE endpoints with a JWT from a different tenant — must return empty results

---

### SEC-03 🔴 RLSTenantInterceptor Bypasses RLS for "default" Tenant

**Severity:** CRITICAL  
**Discovered by:** Code inspection during research  
**Blocks:** Tenant isolation — any request with tenant="default" has NO database-level isolation

**Exact Location:** `cluster/internal/api/ingestion/grpc_adapter.go`, line 803

```go
// Line 803 — CRITICAL: Bypasses RLS for "default" or empty tenant
if tenant == "" || tenant == "default" {
    return handler(ctx, req)   // No RLS applied!
}
```

**"default" tenant origins (5+ locations):**

| File | Line | Code |
|------|------|------|
| `api/handler/timeline.go` | 50, 79, 121, 170, 254 | `tenant = "default"` (5 hardcoded fallbacks) |
| `api/query/intel_handlers.go` | 139, 366 | `req.ServiceID = "default"`, `req.AgentId = "default"` |
| `api/handler/simulation.go` | 59, 108 | `req.TenantID = "default"`, `tenantID = "default"` |
| `cmd/query/main.go` | 103 | `return []string{"default"}` (event consumer fallback) |

**Resolution Steps:**
1. Remove the `tenant == "" || tenant == "default"` bypass condition at grpc_adapter.go:803 — all tenants must pass RLS
2. Replace all 5 `tenant = "default"` hardcodes in `timeline.go` with proper tenant extraction from JWT context (`requireTenant` or `TenantFromContext`)
3. Replace `req.ServiceID = "default"` in `intel_handlers.go` with JWT-derived tenant ID
4. Replace `req.TenantID = "default"` in `simulation.go` with JWT-derived tenant ID
5. Ensure `TenantFromContext` (grpc_adapter.go:52) raises an error instead of returning `""` — never fall back

---

### SEC-04 🟢 RequirePermission — Two Implementations, Auth Version Correct

**Severity:** Resolved (original audit flagged wrong version)

**Finding:** The `auth.RequirePermission()` (at `auth/permission_middleware.go:19`) used by main.go has `c.Next()` at line 55 and all type assertions are checked. This version is CORRECT.

The `security.RequirePermission()` (at `security/rbac.go:88`) has an unchecked type assertion at line 99 but is NOT the version wired by main.go (main.go uses `auth.RequirePermission`).

**Resolution:** No action needed for the wired version. Optionally fix `security/rbac.go:99` with `if roleStr, ok := userRole.(string); ok` as a defense-in-depth measure.

---

### SEC-05 🔴 Cross-Tenant API Key Access via Unscoped Rotation

**Severity:** Reclassified to OK — tenant validation IS enforced in SQL WHERE clause.

**Exact Location:** `cluster/internal/controlplane/apikey.go`, lines 222-225:
```go
tag, err := m.pool.Exec(ctx,
    `UPDATE api_keys SET key_hash = $1, key_prefix = $2
     WHERE key_id = $3 AND tenant_id = $4 AND revoked_at IS NULL`,
    string(hash), prefix, keyID, tenantID,
)
```
`tenantID` comes from JWT claims via `requireTenant(c)` (rest.go:1858) — cannot be spoofed.

**Resolution:** No action needed.

---

### SEC-06 🟢 Refresh Tokens Stored in PostgreSQL (Not In-Memory)

**Severity:** Resolved — FALSE POSITIVE in original audit

**Exact Location:** `cluster/internal/auth/handler.go`, lines 626-640:
```go
func (h *AuthHandler) storeRefreshToken(ctx context.Context, userID, refreshToken string, expiresAt time.Time) error {
    refreshHash := HashRefreshToken(refreshToken)
    if _, err := h.db.Exec(ctx, `
        INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
        VALUES ($1, $2, $3)
    `, userID, refreshHash, expiresAt); err != nil {
```
`h.db` is `*pgxpool.Pool` — PostgreSQL-backed, survives restart.

**Resolution:** No action needed.

---

### SEC-07 🔴 Logout Doesn't Revoke All Token Families

**Severity:** HIGH  
**Discovered by:** Enterprise Audit #5, code inspection  
**Blocks:** Proper session termination

**Exact Location:** `cluster/internal/auth/handler.go`, line 471:
```go
_, err = h.db.Exec(ctx, `
    UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1
`, tokenID)
```
This revokes only the SPECIFIC token ID presented at logout. If an attacker has a different refresh token from the same family (pre-rotation), that token remains valid.

**Resolution Steps:**
1. Add a `family_id` column to `refresh_tokens` table
2. On each token rotation, assign the same `family_id` to the new token
3. On logout, revoke ALL tokens in the family: `UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = $1`
4. Store the `family_id` in the JWT refresh claim for lookup

---

### SEC-08 🔴 Hardcoded API Keys in Git History

**Severity:** CRITICAL (Working tree resolved, git history NOT)  
**Discovered by:** Gitleaks scan — 20 leaks including 5 production `pk_live_*` keys  
**Tested:** `gitleaks git --report-format json --report-path gitleaks.json .`

**Working Tree Status:** CLEAN ✅ (all keys replaced with env vars in security audit)
**Git History Status:** DIRTY 🔴 (keys visible in commit history — `git log -p` shows them)

**Exact Locations (working tree already fixed):**

| File | Old Secret | Fixed To |
|------|-----------|----------|
| `scripts/gen_key_hash.py` | `pk_live_6a89eaf0d...` | `os.environ["PARYTY_API_KEY"]` |
| `scripts/insert_api_key.py` | `pk_live_6a89eaf0d...` | `os.environ["PARYTY_API_KEY"]` |
| `scripts/verify_key.py` | `pk_live_9e379f2c...` | `os.environ["PARYTY_API_KEY"]` |
| `scripts/test_nvidia.py` | `nvapi-qulAKQB7...` | `os.environ["NVIDIA_API_KEY"]` |
| `scripts/kimi_proxy.py` | `nvapi-qulAKQB7...` | `os.environ["NVIDIA_API_KEY"]` |
| `scripts/kimi_https_proxy.py` | `nvapi-qulAKQB7...` | `os.environ["NVIDIA_API_KEY"]` |
| `deploy/compose/docker-compose.dev.yaml` | JWT secret | `${PARYTY_JWT_SECRET}` |

**Resolution Steps:**
1. **IMMEDIATELY** rotate all keys on the production cluster via API key manager
2. Run `git filter-repo` to purge from history (BFG Repo-Cleaner alternative):
   ```
   git filter-repo --path configs/agent/ --path scripts/ --invert-paths --force
   ```
3. Alternatively, if history rewrite is not possible, mark keys as revoked in DB and rotate
4. Add CI pre-commit hook: `gitleaks protect --staged --verbose`

---

### SEC-09 🔴 No Encryption at Rest

**Severity:** HIGH  
**Blocks:** SOC2, HIPAA, GDPR compliance

**Exact Locations:**

| Service | File:Line | Current State |
|---------|-----------|---------------|
| PostgreSQL | `docker-compose.dev.yaml:160,189` | `sslmode=disable` — no TLS, no pgcrypto |
| SeaweedFS | `docker-compose.dev.yaml:228` | `USE_SSL: "false"` — unencrypted volumes |
| Dragonfly | `docker-compose.dev.yaml:98-99` | No `--tls-port`, no `--tls-cert-file` |
| QuestDB | `docker-compose.dev.yaml` | No TLS configuration at all |

**Resolution Steps:**
1. PostgreSQL: Enable SSL (`sslmode=verify-full`), mount certs, configure `pg_hba.conf` with `hostssl`
2. SeaweedFS: Set `PARYTY_SEAWEEDFS_USE_SSL=true`, mount TLS certs for filer+volume
3. Dragonfly: Add `--tls-port 6380 --tls-cert-file /certs/cert.pem --tls-key-file /certs/key.pem`
4. QuestDB: Configure TLS acceptor via `questdb.conf` or env var `QDB_HTTP_TLS_ENABLED=true`
5. Add KMS integration (AWS KMS / HashiCorp Vault) for encryption key management
6. Implement application-level field encryption for PII columns (email, name) in PostgreSQL using pgcrypto

---

### SEC-10 🔴 CSP Header Allows Plain-Text WebSocket

**Severity:** MEDIUM  
**Discovered by:** Code inspection of SecurityHeaders middleware

**Exact Location:** `cluster/internal/security/middleware.go`, line 270:
```go
c.Header("Content-Security-Policy",
    "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
    "img-src 'self' data: blob:; connect-src 'self' ws: wss:; "+
    "font-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
```
`connect-src 'self' ws: wss:` — allows plaintext WebSocket (`ws:`)

**Resolution Steps:**
1. Change `ws: wss:` to `wss:` only (line 270)
2. Add `upgrade-insecure-requests;` directive to auto-upgrade HTTP→HTTPS
3. Verify with CSP evaluator: https://csp-evaluator.withgoogle.com/

---

### SEC-11 🔴 JWT in WebSocket URL Query Parameters

**Severity:** HIGH  
**Discovered by:** Enterprise Audit #12  
**Impact:** Tokens logged in proxy/access logs, server logs, browser history

**Exact Location:** The WebSocket connection is established via `ws://host:port/path?token=eyJ...`. Search pattern: `new WebSocket(` in frontend code and `?token=` in URL construction.

**Resolution Steps:**
1. Move WebSocket auth from query parameter to WebSocket protocol header:
   ```javascript
   const ws = new WebSocket("wss://host/path");
   // Don't append ?token=...
   ```
2. Use the httpOnly cookie (`paryty_access_token`) for WebSocket auth — SameSite=Strict cookies are sent on WebSocket upgrades
3. Update `GinJWTAuthFlexible` middleware to read token from cookie AND Authorization header, prioritizing header
4. Remove `?token=` fallback from WebSocket URL construction in all frontend files

---

### SEC-12 🔴 mTLS Not Enforced for Agent→Cluster gRPC

**Severity:** HIGH  
**Discovered by:** Checkpoint 8.6, code inspection  
**Blocks:** Agent identity verification, prevents man-in-the-middle on agent telemetry

**Exact Location:** `cluster/internal/security/tls.go`, lines 35-55:
```go
func NewMutualTLSConfig(certFile, keyFile, caCertFile string) (*tls.Config, error) {
    // ... COMPLETE implementation exists but is never called ...
}
```
This function exists but is never invoked by `main.go` or any gRPC server startup code.

**Resolution Steps:**
1. In `cmd/ingestion/main.go` (or wherever gRPC server starts), call `security.NewMutualTLSConfig()` and pass to `grpc.Creds()`
2. Generate CA certificate:
   ```
   openssl req -x509 -newkey rsa:4096 -keyout ca-key.pem -out ca-cert.pem -days 3650 -nodes -subj "/CN=Paryty CA"
   ```
3. Generate agent certs signed by CA
4. Add agent cert generation to CI/CD agent build step
5. Add `--tls-cert`, `--tls-key`, `--ca-cert` flags to agent binary (`agent/src/main.rs`)
6. Test: start cluster with mTLS, attempt plaintext agent connection → must be rejected

---

### SEC-13 🔴 No Rate Limiting on Query, Metrics, Topology Endpoints

**Severity:** MEDIUM  
**Discovered by:** Code inspection of rate limiter coverage  
**Impact:** DoS via cost-intensive queries (wide time ranges, complex topology graphs)

**Current Rate Limiters (covered endpoints only):**

| Limiter | Scope | Limit | Endpoints |
|---------|-------|-------|-----------|
| `loginRateLimiter` | per email | 5/15min | POST /auth/login |
| `registerRateLimiter` | per IP | 5/hour | POST /auth/register |
| `downloadRateLimiter` | per IP | 10/hour | GET /agents/download/:platform |
| `RequireTwinQuota` | per tenant | per plan | POST /twins |

**Uncovered Endpoints (no rate limiting):**

| Endpoint | Risk |
|----------|------|
| GET /api/v1/topology | High — complex graph query, 10K+ node response |
| POST /api/v1/metrics/query | High — can request 90-day window (recently capped via server-side validation but can still request 89 days × many metrics) |
| GET /api/v1/metrics/:id/stream | High — SSE stream per agent, open connections |
| POST /api/v1/traces/query | Medium — trace spans can be large |
| GET /api/v1/events | Medium — event volume can be high |
| GET /api/v1/intel/* | Medium — ML computation cost |

**Resolution Steps:**
1. Add `tenantRateLimiter` middleware checking `X-Paryty-Tenant` header → per-tenant limits
2. Apply to `/api/v1` route group in `main.go` after `RLSTenantMiddleware`
3. Configure limits per plan tier (Free: 10 req/min, Pro: 100 req/min, Business: 1000 req/min, Enterprise: unlimited)
4. Store in Dragonfly with TTL (1 minute window) for multi-pod distribution
5. Add `Retry-After` header to 429 responses

---

### SEC-14 🔴 In-Memory Rate Limiters Won't Scale Multi-Pod

**Severity:** MEDIUM  
**Discovered by:** Enterprise Audit Warn #4, code inspection

**Exact Locations:**

| Rate Limiter | File:Line | Storage |
|-------------|-----------|---------|
| `loginRateLimiter` | `auth/handler.go:43` | `map[string]*loginWindow` (in-process) |
| `registerRateLimiter` | `auth/handler.go:137` | `map[string]*registerWindow` (in-process) |
| `downloadRateLimiter` | `api/query/rest.go:2716` | `map[string]*downloadWindow` (in-process) |
| `tokenBucketLimiter` | `security/middleware.go:341` | `map[string]*bucketState` (in-process) |

**Impact:** In a 3-pod deployment, each pod has its own counter. A brute-force attacker sending 5 requests to pod A, 5 to pod B, 5 to pod C = 15 total without hitting any pod's individual limit.

**Resolution Steps:**
1. Replace in-memory maps with Dragonfly-backed atomic counters using `INCR` + `EXPIRE`:
   ```
   key = "ratelimit:login:" + email
   count = INCR key
   if count == 1: EXPIRE key 900  # 15 minutes
   if count > 5: return 429
   ```
2. Use Dragonfly's `MULTI/EXEC` for atomic check-and-increment to avoid race conditions
3. Implement as `DragonflyRateLimiter` struct that wraps `dragonfly.Client`
4. Keep in-memory version as fallback for single-instance / dev mode

---

## CATEGORY B: INFRASTRUCTURE & DEPLOYMENT — 8 Gaps

---

### INF-01 🔴 No Production-Ready Agent Binary Hosting

**Severity:** CRITICAL  
**Discovered by:** Spec 04 requirements, CI/CD audit  
**Impact:** Agent download endpoint returns 404 for all platforms — onboarding is broken

**Exact Location:** `cluster/internal/api/query/rest.go`, lines 2722-2757:
```go
func (s *QueryService) DownloadAgentBinary(c *gin.Context) {
    binaryDir := os.Getenv("PARYTY_BINARY_DIR")
    if binaryDir == "" {
        binaryDir = "./bin/agents"    // Directory does not exist!
    }
    path := filepath.Join(binaryDir, filename)
    if _, err := os.Stat(path); os.IsNotExist(err) {
        c.JSON(http.StatusNotFound, gin.H{"message": "agent binary not available for download"})
        return   // <-- This ALWAYS fires
    }
}
```

**Current State:**
- `bin/agents/` directory: **DOES NOT EXIST**
- `Dockerfile.agent`: builds binary inside container ONLY, no copy-out
- CI: `cargo build --release` but no artifact upload
- CD: No agent image build job at all

**Resolution Steps:**
1. Create directory: `mkdir -p bin/agents/`
2. Configure CI to cross-compile and upload agent binaries:
   ```yaml
   - name: Build agent binaries
     run: |
       cd agent
       cargo build --release --target x86_64-unknown-linux-gnu
       cargo build --release --target x86_64-pc-windows-gnu
   - uses: actions/upload-artifact@v4
     with:
       name: agent-binaries
       path: agent/target/*/release/paryty-agent*
   ```
3. Configure CD to download binaries and host:
   ```yaml
   - uses: actions/download-artifact@v4
     with: { name: agent-binaries, path: bin/agents/ }
   - run: aws s3 sync bin/agents/ s3://paryty-releases/agents/ --acl public-read
   ```
4. Update `DownloadAgentBinary` handler to serve from S3/CDN with redirect
5. Set `PARYTY_BINARY_DIR` env var in docker-compose and K8s

---

### INF-02 🔴 K8s Secrets Are Placeholders

**Severity:** CRITICAL  
**Discovered by:** Code inspection of `sealed-secrets.yaml`  

**Exact Location:** `deploy/kubernetes/base/secrets/sealed-secrets.yaml`:

| Line | Field | Current Value |
|------|-------|---------------|
| 38 | `password` | `"CHANGE_ME_IN_PRODUCTION"` |
| 47 | `secret` (JWT) | `"CHANGE_ME_IN_PRODUCTION_MIN_32_BYTES_REQUIRED"` |
| 56 | `tls.crt` | `""` (empty) |
| 57 | `tls.key` | `""` (empty) |
| 58 | `ca.crt` | `""` (empty) |

All stored as `stringData` (plaintext in repo), not actually sealed with kubeseal.

**Resolution Steps:**
1. Replace `sealed-secrets.yaml` with ExternalSecrets Operator integration:
   ```yaml
   apiVersion: external-secrets.io/v1beta1
   kind: ExternalSecret
   spec:
     secretStoreRef:
       name: aws-secretsmanager
     target:
       name: paryty-secrets
     data:
     - secretKey: db-password
       remoteRef: { key: "prod/paryty/db-password" }
   ```
2. For non-AWS: use Bitnami Sealed Secrets:
   ```bash
   kubectl create secret generic paryty-secrets \
     --from-literal=db-password=$(openssl rand -base64 32) \
     --from-literal=jwt-secret=$(openssl rand -base64 64) \
     --dry-run=client -o yaml | kubeseal > sealed-secrets.yaml
   ```
3. Generate TLS certs via cert-manager (see INF-06)
4. Remove all `CHANGE_ME` placeholders
5. Add `.gitignore` rule: `deploy/kubernetes/base/secrets/*-unsealed.yaml`

---

### INF-03 🔴 Agent DaemonSet Runs Effectively Privileged

**Severity:** CRITICAL  
**Discovered by:** Enterprise Audit #20, code inspection  
**Impact:** Container escape → full node compromise

**Exact Location:** `deploy/kubernetes/base/agent-daemonset.yaml`:

| Line | Field | Value | Risk |
|------|-------|-------|------|
| 16 | `hostPID` | `true` | Access to all host processes |
| 17 | `hostNetwork` | `true` | Access to all host network interfaces |
| 63 | `SYS_ADMIN` capability | true | Nearly equivalent to root |
| 64 | `NET_ADMIN` capability | true | Network stack manipulation |
| 65 | `SYS_PTRACE` capability | true | Read any process memory |
| 67 | `runAsNonRoot` | `false` | Runs as UID 0 |
| 68 | `allowPrivilegeEscalation` | `true` | Can gain additional capabilities |
| 71-72 | `/proc` mount | `hostPath: /proc` | Host process filesystem accessible |
| 74-75 | `/sys` mount | `hostPath: /sys` | Host kernel parameters accessible |

**Resolution Steps:**
1. Remove `hostPID: true` — eBPF programs attach to kernel hooks, don't need host PID namespace
2. Remove `hostNetwork: true` — agents report via gRPC, don't need host network stack
3. Remove `SYS_ADMIN` — not required for BPF_PROG_TYPE_KPROBE, BPF_PROG_TYPE_TRACEPOINT, or BPF_PROG_TYPE_SOCK_OPS
4. Keep `NET_ADMIN` only if required for `BPF_PROG_TYPE_SCHED_CLS` (traffic control) — verify
5. Keep `SYS_PTRACE` only if required for kprobe attachment — verify (perf_event_open may need CAP_PERFMON instead since Linux 5.8)
6. Set `runAsNonRoot: true` with UID 65534 (nobody) — eBPF programs load via libbpf which uses `bpf()` syscall, BPF operations check `CAP_BPF` + `CAP_NET_ADMIN` not UID 0
7. Set `allowPrivilegeEscalation: false`
8. Mount only required paths: `/sys/kernel/debug` (read-only), `/sys/fs/bpf` (mounted BPF filesystem)
9. Replace `/proc` hostPath mount with container-scoped `/proc` (from container runtime)
10. Add seccomp profile that allows only `bpf`, `perf_event_open`, `socket` syscalls

---

### INF-04 🔴 No Database Migration Framework

**Severity:** HIGH  
**Discovered by:** Code inspection of schema management  
**Impact:** No versioning, no rollback, no migration tracking, schema drift risk

**Exact Locations:**

| File | Lines | Description |
|------|-------|-------------|
| `cluster/internal/controlplane/schema.go` | 32-57, 75-364 | `CREATE TABLE IF NOT EXISTS` at startup (idempotent, no versioning) |
| `cluster/migrations/` | 1 file | Only `003_twin_name_unique_index.sql` (390 bytes) |
| `tests/deploy/init-sim-db.sql` | 83 lines | Test-only simulation schema |

**Resolution Steps:**
1. Install golang-migrate:
   ```bash
   go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
   ```
2. Create sequential migration directory:
   ```
   cluster/migrations/
     001_create_tenants_and_keys.up.sql
     002_create_users_and_tokens.up.sql
     003_twin_name_unique_index.up.sql
     004_create_agent_tables.up.sql
     005_abilities_column.up.sql
     ... each with matching .down.sql
   ```
3. Extract current `CREATE TABLE IF NOT EXISTS` DDL into migration files 001-004
4. Add migration runner in `cmd/query/main.go`:
   ```go
   import "github.com/golang-migrate/migrate/v4"
   m, _ := migrate.New("file://migrations", dbURL)
   if err := m.Up(); err != nil && err != migrate.ErrNoChange {
       log.Fatal(err)
   }
   ```
5. Add `schema_migrations` tracking table (auto-created by golang-migrate)
6. Add migration step to CD pipeline before deploy

---

### INF-05 🔴 Two Competing Helm Chart Systems

**Severity:** HIGH  
**Discovered by:** Code inspection — 2 separate Helm chart hierarchies  
**Impact:** CI/CD only deploys one system, the other is dead code

**System A:** `deploy/helm/paryty/charts/` (5 subcharts: ingestion, pipeline, query, intelligence, frontend)  
**System B:** `cluster/deploy/helm/` (7 charts: paryty-agent, paryty-cluster, paryty-frontend, paryty-storage, paryty-security, paryty-monitoring, paryty parent)

**CD Workflow (`cd.yml` lines 200-223) only references System B.**

**Resolution Steps:**
1. Choose ONE chart system (recommend System B — it has more comprehensive coverage and is CD-referenced)
2. Merge missing subcharts from System A into System B:
   - `ingestion` → `paryty-cluster/charts/ingestion/` or add to `paryty-cluster/templates/`
   - `pipeline` → same
   - `query` → add to `paryty-cluster/`
   - `intelligence` → add to `paryty-cluster/` or keep as `paryty-intelligence/` standalone
3. Delete `deploy/helm/paryty/` (System A) to avoid confusion
4. Update `deploy/helm/paryty/Chart.yaml` → move to `deploy/helm/paryty-cluster/Chart.yaml`

---

### INF-06 🔴 No cert-manager or Service Mesh Integration

**Severity:** HIGH  
**Discovered by:** Zero `ClusterIssuer`, `cert-manager`, `istio`, `linkerd` matches in entire `deploy/` directory

**Resolution Steps:**
1. Install cert-manager:
   ```bash
   helm repo add jetstack https://charts.jetstack.io
   helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace
   ```
2. Create ClusterIssuer:
   ```yaml
   apiVersion: cert-manager.io/v1
   kind: ClusterIssuer
   metadata: { name: letsencrypt-prod }
   spec:
     acme:
       server: https://acme-v02.api.letsencrypt.org/directory
       email: ops@paryty.io
       privateKeySecretRef: { name: letsencrypt-prod }
       solvers: [http01: { ingress: { class: nginx } }]
   ```
   File: `deploy/kubernetes/base/tls/cluster-issuer.yaml` (NEW)
3. Add Certificate resources to each service's K8s manifest
4. For service mesh: deploy Istio sidecar injection (ambient mesh for simpler ops):
   ```bash
   istioctl install --set profile=ambient
   ```
5. Add `PeerAuthentication` policy for mTLS within mesh
6. Add `AuthorizationPolicy` for service-to-service access control

---

### INF-07 🔴 Python Intelligence Layer Missing from CI/CD

**Severity:** MEDIUM  
**Discovered by:** CI/CD workflow inspection  

**Exact Location:** `.github/workflows/ci.yml` — 12 jobs: Rust, Go, TypeScript only. Zero Python jobs.

**Resolution Steps:**
1. Add to `ci.yml`:
   ```yaml
   lint-python:
     runs-on: ubuntu-latest
     steps:
       - uses: actions/checkout@v4
       - uses: actions/setup-python@v5
         with: { python-version: '3.12' }
       - run: pip install ruff mypy
       - run: ruff check intelligence/
       - run: mypy --strict intelligence/

   test-python:
     runs-on: ubuntu-latest
     steps:
       - uses: actions/checkout@v4
       - run: pip install -r intelligence/requirements.txt
       - run: pytest intelligence/tests/ -v --cov=intelligence

   security-python:
     runs-on: ubuntu-latest
     steps:
       - run: pip install pip-audit bandit
       - run: pip-audit -r intelligence/requirements.txt
       - run: bandit -r intelligence/ -f json -o bandit.json
   ```
2. Add to `cd.yml`:
   ```yaml
   build-intelligence:
     uses: docker/build-push-action@v5
     with:
       context: intelligence
       file: intelligence/Dockerfile
       push: true
       tags: ghcr.io/paryty/intelligence:${{ github.sha }}
   ```

---

### INF-08 🔴 Single-Architecture Builds (linux/amd64 only)

**Severity:** MEDIUM  
**Discovered by:** CD workflow inspection — no `platforms:` parameter

**Resolution Steps:**
1. Add `platforms: linux/amd64,linux/arm64` to all `docker/build-push-action@v5` steps in `cd.yml`:
   ```yaml
   - uses: docker/build-push-action@v5
     with:
       platforms: linux/amd64,linux/arm64
       # ... rest of config
   ```
2. Set up QEMU for cross-platform emulation:
   ```yaml
   - uses: docker/setup-qemu-action@v3
   ```
3. Set up Docker Buildx:
   ```yaml
   - uses: docker/setup-buildx-action@v3
   ```
4. Add agent Windows build target: `x86_64-pc-windows-gnu` (cross-compile from Linux CI runner)
5. Add agent macOS build target: `x86_64-apple-darwin` (for Apple Silicon dev laptops)

---

## CATEGORY C: FEATURE COMPLETENESS — 7 Gaps

---

### FEAT-01 ⚪ MetricChart — ALREADY WIRED (False Positive)

**Status:** RESOLVED — research confirmed MetricChart, MetricCards, and TimeRangeSelector are ALL imported and rendered in `MetricsView.tsx` (lines 6-8, 72-85).

**No action needed.**

---

### FEAT-02 🟢 Alert Acknowledge — ALREADY WIRED to Backend

**Status:** RESOLVED — fixed in spec 08 security audit. `alertsStore.acknowledgeAlert()` is now async and calls `POST /api/v1/alerts/:id/acknowledge` via `client.acknowledgeAlert(alertId)`.

**No action needed.**

---

### FEAT-03 ⚪ AlertPanel — ALREADY WIRED (False Positive)

**Status:** RESOLVED — research confirmed AlertPanel IS imported at `AlertView.tsx:3` and rendered at lines 77-82, wired to `alerts.selectAlert()` at line 51.

**No action needed.**

---

### FEAT-04 🟡 Upgrade Button Is a Stub Toast

**Severity:** MEDIUM  
**Discovered by:** Code inspection of SettingsPage.tsx  

**Exact Location:** `frontend/src/pages/SettingsPage.tsx`, lines 270-279:
```tsx
onClick={() => addToast({ type: 'info', message: 'Plan upgrade coming soon — contact sales@paryty.io' })}
```

**Resolution Steps:**
1. Create `POST /api/v1/plans/change` backend endpoint that validates plan transition rules (e.g., Free→Pro allowed, Pro→Business allowed, no downgrade without support)
2. Wire Upgrade button to call the endpoint:
   ```tsx
   onClick={async () => {
     await client.post('/api/v1/plans/change', { planName: plan.name });
     addToast({ type: 'success', message: `Upgraded to ${plan.displayName}!` });
     fetchCurrentPlan();
   }}
   ```
3. Add plan change confirmation modal showing price/feature diff
4. Integrate Stripe billing webhook for paid plan activation
5. Handle plan change audit logging

---

### FEAT-05 🟢 Change Password Backend — ALREADY IMPLEMENTED

**Status:** RESOLVED — fixed in spec 11 security audit. `POST /api/v1/auth/change-password` handler exists in `auth_rest_adapter.go:460-508` with bcrypt verification, password policy validation, and DB update.

**No action needed.**

---

### FEAT-06 🔴 Metric Naming Convention Mismatch

**Severity:** HIGH  
**Discovered by:** Code inspection — 3 incompatible naming systems  
**Impact:** All metric queries return zero data because frontend names don't match backend names

**Three Naming Systems:**

| Layer | File:Line | Name Examples |
|-------|-----------|---------------|
| Frontend dropdown | `MetricsView.tsx:62-67` | `cpu_usage`, `memory_usage`, `disk_io`, `network_io` (underscore, no units) |
| Backend fallback | `rest.go:762-773` | `cpu.usage_percent`, `memory.usage_percent`, `disk.usage_percent` (dot notation, with `_percent` suffix) |
| Agent proto | `agent/src/metal/batch.rs:271` | Structured `MetricBatch` with nested protobuf fields — NO flat metric names |

**Resolution Steps:**
1. Define a canonical metric name registry in a shared location (proto or constant file):
   ```go
   // cluster/internal/metrics/names.go (NEW)
   const (
       MetricCPUUsage    = "cpu.usage_percent"
       MetricMemUsage    = "memory.usage_percent"
       MetricDiskIORead  = "disk.read_bytes_per_sec"
       MetricDiskIOWrite = "disk.write_bytes_per_sec"
       MetricNetRxBytes  = "network.rx_bytes_per_sec"
       MetricNetTxBytes  = "network.tx_bytes_per_sec"
       MetricCPULoad1m   = "cpu.load_average_1m"
       MetricMemUsed     = "memory.used_bytes"
   )
   ```
2. Update frontend dropdown to use canonical names:
   ```tsx
   { label: 'CPU Usage', value: 'cpu.usage_percent' },
   { label: 'Memory Usage', value: 'memory.usage_percent' },
   // etc.
   ```
3. Update `GetMetricNames` (rest.go:762) to return canonical names
4. Ensure agent scraper writes metric names that match canonical registry
5. Add backend validation: reject unknown metric names with 400 error
6. Add frontend enum or const object for metric names

---

### FEAT-07 🔴 No Anomaly List REST Endpoint

**Severity:** HIGH  
**Discovered by:** Route inspection — no `GET /anomalies` route  
**Impact:** Cannot retrieve historical/paginated anomalies; WebSocket-only means missed anomalies on disconnect

**Exact Location:** `cluster/internal/api/query/rest.go:91-131` — anomaly routes are absent from `RegisterRoutes`. Only anomaly endpoints exist in `intel_handlers.go:102-104` (POST detect, GET status, POST explain) — all under `/api/v1/intel/anomalies/`.

**Resolution Steps:**
1. Add route: `api.GET("/anomalies", s.ListAnomalies)` in `rest.go:RegisterRoutes` (near line 119, after alert routes)
2. Implement `ListAnomalies` handler:
   ```go
   func (s *QueryService) ListAnomalies(c *gin.Context) {
       tenantID, _ := requireTenant(c)
       limit := c.DefaultQuery("limit", "50")
       offset := c.DefaultQuery("offset", "0")
       severity := c.Query("severity")
       
       anomalies, err := s.store.ListAnomalies(ctx, tenantID, limit, offset, severity)
       if err != nil {
           c.JSON(500, ...)
           return
       }
       c.JSON(200, anomalies)
   }
   ```
3. Implement `ListAnomalies` in storage layer querying QuestDB anomaly table
4. Add pagination, filtering by severity, date range, agent_id
5. Return standard envelope: `{ data: [...], total: N, limit, offset }`

---

### FEAT-08 🔴 No Email Verification

**Severity:** HIGH  
**Discovered by:** Zero `email_verified` / `verify_email` matches in entire `auth/handler.go`  

**Exact Location:** `cluster/internal/auth/handler.go:219-323` — Register function creates user immediately with no verification step.

**Resolution Steps:**
1. Add `email_verified BOOLEAN DEFAULT FALSE` column to `users` table
2. On registration, generate verification token and store in `email_verifications` table:
   ```sql
   CREATE TABLE email_verifications (
       id UUID PRIMARY KEY,
       user_id UUID REFERENCES users(id),
       token_hash TEXT NOT NULL,
       expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours',
       created_at TIMESTAMPTZ DEFAULT NOW()
   );
   ```
3. Send verification email (SMTP, SendGrid, or AWS SES)
4. Create `GET /api/v1/auth/verify-email?token=...` endpoint
5. On verification: set `email_verified = TRUE`, delete token
6. During login: reject unverified emails with specific error: "Please verify your email before logging in. Check your inbox."
7. Add "Resend verification email" button to login error state

---

### FEAT-09 🔴 No Password Reset Flow

**Severity:** HIGH  
**Discovered by:** Zero `forgot_password` / `reset_password` / `ForgotPassword` matches in entire codebase  

**Exact Location:** LoginPage has a `<Link to="/forgot-password">` (LoginPage.tsx, post-edit), but `/forgot-password` route is **not registered** in `App.tsx`.

**Resolution Steps:**
1. Create `frontend/src/pages/ForgotPasswordPage.tsx` — email input form
2. Create `POST /api/v1/auth/forgot-password` backend endpoint:
   - Accept email, look up user, generate reset token (SHA-256, 1-hour expiry)
   - Store token hash in `password_reset_tokens` table
   - Send email with link: `https://paryty.io/reset-password?token=<token>`
   - Always return 200 "If an account with that email exists, a reset link has been sent" (don't reveal account existence)
3. Create `frontend/src/pages/ResetPasswordPage.tsx` — new password form (token from URL query)
4. Create `POST /api/v1/auth/reset-password` backend endpoint:
   - Validate token hash against DB, check expiry (1 hour)
   - Validate new password against policy
   - Update password hash, revoke all refresh tokens, delete reset token
5. Register routes in `App.tsx` and `main.go`
6. Add rate limiting: 3 reset emails per email per hour

---

### FEAT-10 🔴 Forecast Model Accuracy Returns Zeros

**Severity:** MEDIUM  
**Discovered by:** Code inspection of `intel_handlers.go:261-308`  

**Exact Location:** `cluster/internal/api/query/intel_handlers.go`, lines 297-305:
```go
if len(result) == 0 {
    result["default"] = gin.H{
        "bestModel":       "Ensemble",
        "weights":         gin.H{"Linear Regression": 0.0, "Prophet": 0.0, "XGBoost": 0.0},
        "accuracy":        gin.H{"Linear Regression": 0.0, "Prophet": 0.0, "XGBoost": 0.0},
        "lastTrained":     "",
        "trainingSamples": 0,
    }
}
```

**Resolution Steps:**
1. Return a proper error response when no models are trained:
   ```go
   c.JSON(http.StatusServiceUnavailable, gin.H{
       "error": "MODEL_NOT_TRAINED",
       "message": "No forecasting models have been trained yet. Models require at least 7 days of historical data.",
   })
   ```
2. Add a `model_training_status` field to the response: `"status": "untrained"` | `"training"` | `"ready"` | `"degraded"`
3. Trigger initial model training on first agent connection (collect 7 days minimum before serving predictions)
4. Display "Models training — X days of data collected" in the frontend

---

## CATEGORY D: TESTING COVERAGE — 5 Gaps

---

### TST-01 🔴 No Load Testing Executed

**Severity:** CRITICAL  
**Discovered by:** Checkpoint 8.10, `tests/load/` directory empty  

**Spec requires:**
- Load test scenarios for 100, 1K, 10K concurrent agents
- Performance targets: ingestion p99 <100ms, query p99 <500ms, error rate <0.1%

**Resolution Steps:**
1. Create `tests/load/` with k6 test scripts:
   ```javascript
   // tests/load/scenarios/baseline.js
   import http from 'k6/http';
   import { check } from 'k6';
   export const options = { vus: 100, duration: '5m' };
   export default function() {
     const res = http.post('http://localhost:8080/api/v1/ingestion/metrics', payload);
     check(res, { 'status 200': r => r.status === 200 });
   }
   ```
2. Create agent simulator: `tests/load/agent_simulator.go` — spawns N goroutines sending MetricBatch protos over gRPC
3. Run scenarios:
   ```bash
   k6 run tests/load/scenarios/baseline.js
   k6 run tests/load/scenarios/scale.js
   k6 run tests/load/scenarios/burst.js
   ```
4. Collect p50/p95/p99 latency, error rate, throughput
5. Add load test results to CI as artifacts
6. Set performance budgets in CI: fail if p99 > target

---

### TST-02 🔴 No E2E Test Suite

**Severity:** CRITICAL  
**Discovered by:** Checkpoint 8.5 — agent registration E2E test specified but not implemented  

**Resolution Steps:**
1. Create E2E test framework using Playwright + Go test harness:
   ```
   tests/e2e/
     setup.go        — Start docker-compose, wait for healthy
     teardown.go     — Stop services, collect logs
     registration_test.go  — Agent starts, registers, appears in agent list
     twin_lifecycle_test.go — Create twin, assign agent, verify metrics flow
     auth_flow_test.go — Register → Login → Token refresh → Logout → Token invalid
   ```
2. Write critical user journey tests:
   - **Signup → Login → Create Twin → Connect Agent → View Metrics**
   - **Alert fires → Acknowledge → Alert resolves**
   - **Timeline replay → Scrub → Export**
3. Run in CI as a separate job matrix entry (after all build/test gates pass)
4. Add E2E test dashboard to track success rate

---

### TST-03 🔴 Twin CRUD Lifecycle Tests Missing

**Severity:** HIGH  
**Discovered by:** Checkpoint 8.2  

**Resolution Steps:**
1. Create `cluster/internal/twin/twin_lifecycle_test.go`:
   ```go
   func TestTwinLifecycle(t *testing.T) {
       // Create twin
       twin, err := tm.CreateTwin(ctx, tenantID, "test-twin", "desc", config, nil)
       require.NoError(t, err)
       
       // Read twin
       got, err := tm.GetTwin(ctx, twin.ID)
       require.Equal(t, twin.Name, got.Name)
       
       // Update twin
       updated, err := tm.UpdateTwin(ctx, twin.ID, "updated-name", "new desc")
       require.Equal(t, "updated-name", updated.Name)
       
       // Soft delete
       err = tm.DeleteTwin(ctx, twin.ID)
       require.NoError(t, err)
       
       // Verify deleted twin not in list
       twins, _ := tm.ListTwins(ctx, tenantID)
       require.NotContains(t, twinNames(twins), "updated-name")
   }
   ```
2. Test concurrent creation (race condition check)
3. Test quota enforcement boundary (at limit, just below limit)
4. Test name uniqueness (duplicate name within tenant)

---

### TST-04 🔴 Quota Enforcement Boundary Tests Missing

**Severity:** HIGH  
**Discovered by:** Checkpoint 8.3  

**Resolution Steps:**
1. Create `cluster/internal/plan/quota_boundary_test.go`:
   ```go
   func TestQuotaAtLimit(t *testing.T) {
       // Create up to limit (e.g., maxTwins = 1 for Free plan)
       _, err := tm.CreateTwin(ctx, tenantID, "twin-1", "", config, nil)
       require.NoError(t, err)
       
       // Create one more — must fail with 402
       _, err = tm.CreateTwin(ctx, tenantID, "twin-2", "", config, nil)
       require.Error(t, err)
       require.Contains(t, err.Error(), "quota exceeded")
   }
   
   func TestConcurrentQuotaEnforcement(t *testing.T) {
       // 10 goroutines try to create twin when 1 slot remains
       // Only 1 must succeed
   }
   ```
3. Test upgrade path: Free→Pro increases maxTwins, verify old twins retained

---

### TST-05 🟢 Python Intelligence Unit Tests Pass (14/14)

**Severity:** LOW (tests exist, integration gap remains)  
**Discovered by:** `intelligence/tests/` directory  

**Resolution Steps:**
1. Add integration test that verifies Go↔Python gRPC contract:
   ```python
   # intelligence/tests/test_grpc_contract.py
   def test_forecast_request_response_matches_proto():
       # Verify proto field names match between generated Go and Python code
   ```
2. Add `buf breaking` check to CI to prevent proto contract drift
3. Add mock Python service for Go handler tests

---

## CATEGORY E: DATA INTEGRITY — 2 Gaps

---

### DAT-01 🔴 Abilities Not Persisted to Database

**Severity:** CRITICAL  
**Discovered by:** Spec 03 G-01, G-02, G-03  
**Impact:** Abilities selected in wizard are lost on page refresh; no server-side ability tracking

**Exact Locations:**

| Layer | File | Issue |
|-------|------|-------|
| Proto | `proto/paryty/v1/twin.proto` | `CreateTwinRequest` has no `abilities` field |
| Proto | `proto/paryty/v1/twin.proto` | `TwinInfo` has no `abilities` field |
| DB | `paryty_twins` table | No `abilities` column |
| Backend | `twin.go` | `TwinConfig` struct has `Abilities []string` (added in reality impl) but not flowing through proto |
| REST | `rest.go` CreateTwin handler | Parses `abilities` from JSON body (added in reality impl) |
| Frontend | `dashboardStore.ts` commitDraft | Sends `abilities` in payload (added in reality impl) |

**Current partial state:** Abilities are SENT by frontend and PARSED by REST handler, but NOT stored through the proto/gRPC layer. They're injected into the REST response map via `twin_rest_adapter.go`.

**Resolution Steps:**
1. Add to `proto/paryty/v1/twin.proto`:
   ```protobuf
   message CreateTwinRequest {
       string name = 1;
       string description = 2;
       TwinConfig config = 3;
       repeated string abilities = 4;  // NEW
   }
   message TwinInfo {
       // ... existing fields ...
       repeated string abilities = 12;  // NEW
   }
   ```
2. Regenerate code: `buf generate`
3. Add DB migration:
   ```sql
   ALTER TABLE paryty_twins ADD COLUMN IF NOT EXISTS abilities JSONB DEFAULT '[]';
   CREATE INDEX IF NOT EXISTS idx_twins_abilities ON paryty_twins USING GIN (abilities);
   ```
4. Update `TwinConfig` → store abilities in dedicated column (already partially done)
5. Update `twinInfoToProto` → include abilities field
6. Update `protoInfoToTwin` → parse abilities from proto
7. Update frontend `TwinDetails` → remove local-only fallback, use backend data

---

### DAT-02 🔴 Rate Limit State Lost on Restart

**Severity:** MEDIUM  
**Discovered by:** Code inspection — all rate limiters use in-memory Go maps  
**Note:** This is a DUPLICATE of SEC-14. See SEC-14 for resolution steps.

---

## CATEGORY F: OBSERVABILITY — 3 Gaps

---

### OBS-01 🔴 Self-Monitoring NOT Implemented

**Severity:** HIGH  
**Discovered by:** Checkpoint 8.11, `observability-golden-signals.md` defines metrics but none exported  

**Golden signals defined but NOT exported:**

| Component | Required Metric | Status |
|-----------|----------------|--------|
| Agent | `paryty.agent.grpc.send.duration_ms` | NOT EXPORTED |
| Agent | `paryty.agent.metrics.collected.total` | NOT EXPORTED |
| Agent | `paryty.agent.edge_buffer.size` | NOT EXPORTED |
| Ingestion | `paryty.cluster.ingestion.process.duration_ms` | NOT EXPORTED |
| Pipeline | `paryty.cluster.pipeline.stage.duration_ms` | NOT EXPORTED |
| Storage | `paryty.cluster.storage.hot.read.duration_ms` | NOT EXPORTED |
| Query | `paryty.cluster.query.duration_ms` | NOT EXPORTED |
| Intelligence | `paryty.intelligence.prediction.duration_ms` | NOT EXPORTED |
| Frontend | `paryty.frontend.render.frame_ms` | NOT EXPORTED |

**Resolution Steps:**
1. Initialize OpenTelemetry SDK in each component's startup:
   ```go
   // cluster/cmd/query/main.go
   import "go.opentelemetry.io/otel"
   import "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
   
   exporter, _ := otlpmetricgrpc.New(ctx)
   provider := metric.NewMeterProvider(metric.WithReader(periodicReader))
   otel.SetMeterProvider(provider)
   ```
2. Define meters and instruments per component:
   ```go
   meter := otel.Meter("paryty.cluster.query")
   queryDuration, _ := meter.Float64Histogram("paryty.cluster.query.duration_ms")
   // Wrap handlers with timing middleware
   ```
3. Export to OTLP collector (Paryty's own ingestion — dogfooding)
4. Add `/metrics` endpoint serving OTLP data (already routed but returns placeholder)
5. Add admin dashboard at `/admin/health` showing all component health

---

### OBS-02 🔴 No Operational Alert Rules for Paryty

**Severity:** HIGH  
**Discovered by:** Dogfooding principle violated — zero Paryty health alerts  

**Resolution Steps:**
1. Create alert rules for Paryty cluster health:
   ```yaml
   # cluster/configs/alert_rules.yaml
   rules:
     - name: "Cluster Ingestion Down"
       metric: paryty.cluster.ingestion.active_streams
       condition: "== 0"
       severity: critical
       message: "No active gRPC ingestion streams"
     
     - name: "Storage Hot Tier Latency High"
       metric: paryty.cluster.storage.hot.read.duration_ms
       condition: "p99 > 10"
       severity: warning
       message: "Dragonfly read latency above 10ms"
     
     - name: "Agent Disconnected"
       metric: paryty.agent.grpc.connected
       condition: "== 0"
       severity: critical
       message: "Agent connection lost"
   ```
2. Register rules via `POST /api/v1/alerts/rules` at startup
3. Add alert rules for: pipeline lag, QuestDB write failures, SeaweedFS upload failures, frontend error rate

---

### OBS-03 🔴 No Unified Log Format

**Severity:** MEDIUM  
**Discovered by:** 4 different logging systems across 4 languages  

| Component | Logging Library | Format |
|-----------|----------------|--------|
| Go Cluster | `go.uber.org/zap` | Structured JSON |
| Rust Agent | `tracing` | Structured key-value |
| TypeScript Frontend | `console.*` | Unstructured text |
| Python Intelligence | `logging` + custom | Mixed |

**Resolution Steps:**
1. Standardize on JSON-structured logging across all components:
   ```json
   {"timestamp":"2026-06-20T18:00:00Z","level":"info","component":"cluster.query","trace_id":"abc","message":"query completed","latency_ms":42}
   ```
2. Use `tracing` subscriber with JSON layer in Rust
3. Use `pino` (structured JSON logger) for TypeScript frontend (replaces `console.*`)
4. Use `structlog` with JSON renderer in Python (already recommended by coding standards)
5. Include standard fields in every log line: `timestamp`, `level`, `component`, `trace_id`, `tenant_id`
6. Ship logs to QuestDB via OTLP/log ingestion pipeline

---

## CATEGORY G: SCALABILITY — 3 Gaps

---

### SCL-01 🟡 Single-Binary Pipeline — No Extraction Plan

**Severity:** MEDIUM  
**Discovered by:** Architecture doc states "can extract stages later" but no plan  

**Resolution Steps:**
1. Define inter-stage message protocol:
   ```protobuf
   message PipelineMessage {
       string stage = 1;           // "aggregation", "correlation", "enrichment"
       string tenant_id = 2;
       bytes payload = 3;          // Serialized stage output
       int64 sequence_number = 4;  // For ordering
   }
   ```
2. Add Redpanda topic per stage: `paryty.{tenant}.pipeline.aggregated`, `paryty.{tenant}.pipeline.correlated`, `paryty.{tenant}.pipeline.enriched`
3. Implement feature flag: `PARYTY_PIPELINE_MODE=single|distributed`
4. When distributed: each stage becomes a separate consumer group, messages flow via Redpanda
5. Document extraction procedure with rollback plan

---

### SCL-02 🔴 Static Database Connection Pooling

**Severity:** MEDIUM  
**Discovered by:** No read replicas configured, no dynamic pool sizing  

**Resolution Steps:**
1. Configure PostgreSQL read replicas:
   ```go
   primaryPool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
   replicaPool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_REPLICA_URL"))
   ```
2. Route reads to replica, writes to primary:
   ```go
   type DB struct {
       Writer *pgxpool.Pool  // Primary
       Reader *pgxpool.Pool  // Replica
   }
   ```
3. Configure pool sizes dynamically based on load:
   ```go
   pool.Config().MaxConns = max(10, runtime.NumCPU() * 4)
   ```
4. Add connection pool metrics: active, idle, waiting, max

---

### SCL-03 🔴 No CDN for Frontend Static Assets

**Severity:** LOW  
**Discovered by:** Frontend Dockerfile serves all assets from single nginx instance  

**Resolution Steps:**
1. Configure CloudFront/Cloudflare CDN in front of nginx
2. Add cache-control headers:
   ```nginx
   location /assets/ {
       expires 1y;
       add_header Cache-Control "public, immutable";
   }
   ```
3. Add content hashing to asset filenames (already done by Vite — files like `TopologyCanvas-mixxGHpp.js`)
4. Set CloudFront origin to nginx endpoint
5. Configure CORS for CDN domain in SecurityHeaders middleware

---

## CATEGORY H: DOCUMENTATION — 4 Gaps

---

### DOC-01 🔴 No Operational Runbook

**Severity:** MEDIUM  

**Resolution Steps:**
1. Create `docs/operations/RUNBOOK.md`:
   - **Startup procedure:** `podman-compose -f deploy/compose/docker-compose.infra.yaml -f deploy/compose/docker-compose.dev.yaml up -d`
   - **Health checks:** `curl /healthz`, `curl /readyz`
   - **Common issues:** Redpanda broker down → check disk space; QuestDB OOM → increase heap; Dragonfly connection refused → check port binding
   - **Backup procedure:** pg_dump for PostgreSQL, SeaweedFS volume snapshot, QuestDB SNAPSHOT
   - **Restore procedure:** pg_restore, volume restore, QuestDB RESTORE SNAPSHOT
   - **Incident response:** Triage levels (P0: full outage, P1: degraded, P2: single tenant), escalation contacts
   - **Rollback procedure:** Helm rollback, database migration rollback

---

### DOC-02 🔴 No API Reference Documentation

**Severity:** MEDIUM  

**Resolution Steps:**
1. Generate API reference from protos:
   ```bash
   buf generate --template buf.gen.doc.yaml
   ```
2. Create `docs/api/` with:
   - REST endpoint catalog (method, path, auth, request/response schema)
   - gRPC service reference (from proto comments)
   - WebSocket event reference (event types, payload schema)
3. Add to CI: auto-publish docs to GitHub Pages on merge to main

---

### DOC-03 🔴 No Architecture Decision Records (ADRs)

**Severity:** MEDIUM  

**Resolution Steps:**
1. Create `docs/adr/` directory
2. Write ADRs for each locked decision:
   ```
   docs/adr/
     001-rust-for-agent.md      — Why Rust over Go/C++ for agent
     002-go-for-cluster.md      — Why Go over Rust/Java for cluster
     003-redpanda-over-kafka.md — Why Redpanda over Apache Kafka
     004-dragonfly-over-redis.md — Why Dragonfly over Redis
     005-pixijs-over-threejs.md — Why PixiJS over Three.js/WebGL raw
     006-zustand-over-redux.md  — Why Zustand over Redux/MobX
     007-opentelemetry-over-prometheus.md — Dogfooding rationale
     008-python-ml-microservice.md — Why Python over Go for ML
   ```
3. Include context, decision, alternatives considered, consequences

---

### DOC-04 🔴 No Developer Onboarding Guide

**Severity:** LOW  

**Resolution Steps:**
1. Create `docs/development/ONBOARDING.md`:
   - Prerequisites: Go 1.25+, Node 22+, Rust 1.85+, Python 3.12+, Podman
   - Clone and setup: `git clone`, `make dev-setup`
   - Start dev environment: `podman-compose up -d`, `make run-cluster`, `cd frontend && npm run dev`
   - Project structure overview
   - Key architectural patterns
   - First contribution guide (pick a "good first issue")
   - Debugging tips (how to attach debugger, view logs, run tests)

---

## COMPLETION TRACKER

| ID | Gap | Category | Severity | Status | Effort |
|----|-----|----------|----------|--------|--------|
| SEC-01 | TLS disabled everywhere | Security | CRITICAL | 🔴 NOT STARTED | 2 days |
| SEC-02 | RLS not on Intel/SSE routes | Security | HIGH | 🔴 NOT STARTED | 2 hrs |
| SEC-03 | RLSTenantInterceptor bypasses "default" | Security | CRITICAL | 🔴 NOT STARTED | 4 hrs |
| SEC-04 | RequirePermission (auth version) | Security | OK | 🟢 RESOLVED | — |
| SEC-05 | Cross-tenant API key (SQL enforced) | Security | OK | 🟢 RESOLVED | — |
| SEC-06 | Refresh tokens in PostgreSQL | Security | OK | 🟢 RESOLVED | — |
| SEC-07 | Logout doesn't revoke token family | Security | HIGH | 🔴 NOT STARTED | 4 hrs |
| SEC-08 | API keys in git history | Security | CRITICAL | 🟡 PARTIAL | 4 hrs |
| SEC-09 | No encryption at rest | Security | HIGH | 🔴 NOT STARTED | 3 days |
| SEC-10 | CSP allows ws: | Security | MEDIUM | 🔴 NOT STARTED | 30 min |
| SEC-11 | JWT in WebSocket URL | Security | HIGH | 🔴 NOT STARTED | 4 hrs |
| SEC-12 | mTLS not enforced | Security | HIGH | 🔴 NOT STARTED | 2 days |
| SEC-13 | No rate limiting on query endpoints | Security | MEDIUM | 🔴 NOT STARTED | 1 day |
| SEC-14 | In-memory rate limiters (multi-pod) | Security | MEDIUM | 🔴 NOT STARTED | 1 day |
| INF-01 | Agent binary hosting | Infra | CRITICAL | 🔴 NOT STARTED | 2 days |
| INF-02 | K8s secrets placeholders | Infra | CRITICAL | 🔴 NOT STARTED | 1 day |
| INF-03 | Agent DaemonSet privileged | Infra | CRITICAL | 🔴 NOT STARTED | 1 day |
| INF-04 | No migration framework | Infra | HIGH | 🔴 NOT STARTED | 1 day |
| INF-05 | Two Helm chart systems | Infra | HIGH | 🔴 NOT STARTED | 4 hrs |
| INF-06 | No cert-manager/service mesh | Infra | HIGH | 🔴 NOT STARTED | 2 days |
| INF-07 | Python CI/CD missing | Infra | MEDIUM | 🔴 NOT STARTED | 4 hrs |
| INF-08 | Single-arch builds | Infra | MEDIUM | 🔴 NOT STARTED | 2 hrs |
| FEAT-01 | MetricChart wiring | Feature | OK | 🟢 RESOLVED | — |
| FEAT-02 | Alert acknowledge wiring | Feature | OK | 🟢 RESOLVED | — |
| FEAT-03 | AlertPanel wiring | Feature | OK | 🟢 RESOLVED | — |
| FEAT-04 | Upgrade button stub | Feature | MEDIUM | 🟡 PARTIAL | 4 hrs |
| FEAT-05 | Change password backend | Feature | OK | 🟢 RESOLVED | — |
| FEAT-06 | Metric naming mismatch | Feature | HIGH | 🔴 NOT STARTED | 1 day |
| FEAT-07 | No anomaly list endpoint | Feature | HIGH | 🔴 NOT STARTED | 4 hrs |
| FEAT-08 | No email verification | Feature | HIGH | 🔴 NOT STARTED | 2 days |
| FEAT-09 | No password reset | Feature | HIGH | 🔴 NOT STARTED | 2 days |
| FEAT-10 | Forecast model accuracy | Feature | MEDIUM | 🔴 NOT STARTED | 4 hrs |
| TST-01 | No load testing | Testing | CRITICAL | 🔴 NOT STARTED | 3 days |
| TST-02 | No E2E test suite | Testing | CRITICAL | 🔴 NOT STARTED | 5 days |
| TST-03 | Twin CRUD lifecycle tests | Testing | HIGH | 🔴 NOT STARTED | 1 day |
| TST-04 | Quota boundary tests | Testing | HIGH | 🔴 NOT STARTED | 1 day |
| TST-05 | Python integration tests | Testing | LOW | 🔴 NOT STARTED | 4 hrs |
| DAT-01 | Abilities not persisted | Data | CRITICAL | 🟡 PARTIAL | 1 day |
| DAT-02 | Rate limit state (dup of SEC-14) | Data | MEDIUM | → SEC-14 | — |
| OBS-01 | Self-monitoring not implemented | Observability | HIGH | 🔴 NOT STARTED | 3 days |
| OBS-02 | No Paryty health alerts | Observability | HIGH | 🔴 NOT STARTED | 1 day |
| OBS-03 | No unified log format | Observability | MEDIUM | 🔴 NOT STARTED | 1 day |
| SCL-01 | Pipeline extraction plan | Scalability | MEDIUM | 🔴 NOT STARTED | 2 days |
| SCL-02 | Static DB pooling | Scalability | MEDIUM | 🔴 NOT STARTED | 1 day |
| SCL-03 | No CDN | Scalability | LOW | 🔴 NOT STARTED | 4 hrs |
| DOC-01 | No runbook | Docs | MEDIUM | 🔴 NOT STARTED | 1 day |
| DOC-02 | No API reference | Docs | MEDIUM | 🔴 NOT STARTED | 1 day |
| DOC-03 | No ADRs | Docs | MEDIUM | 🔴 NOT STARTED | 2 days |
| DOC-04 | No onboarding guide | Docs | LOW | 🔴 NOT STARTED | 4 hrs |

**Summary: 7 RESOLVED, 2 PARTIAL, 35 NOT STARTED = 42 actionable gaps**

---

## ESTIMATED EFFORT BY PHASE

| Phase | Gaps | Critical | High | Medium/Low | Effort |
|-------|------|----------|------|------------|--------|
| Phase 1: Immediate Blockers | 9 | 7 | 2 | 0 | 6-9 days |
| Phase 2: Pre-Cloud | 15 | 0 | 11 | 4 | 10-15 days |
| Phase 3: Cloud-Grade | 18 | 1 | 1 | 16 | 8-12 days |
| **Total** | **42** | **8** | **14** | **20** | **24-36 days** |
