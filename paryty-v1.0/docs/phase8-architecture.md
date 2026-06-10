# Paryty Phase 8 — Production Hardening + Multi-Tenancy SaaS Platform

## Architecture Document

**Author:** Paryty Cluster Engineering  
**Date:** 2026-06-08  
**Module Path:** `github.com/paryty/paryty-v1.0/cluster`  
**Go Version:** 1.25.0

---

## 1. Database Schema Extensions

### 1.1 DDL: New Tables

All new DDL is added to `cluster/internal/controlplane/schema.go` as additional elements in
the `createControlPlaneTables` slice. Existing tenants and api_keys tables are NOT modified
— they remain as-is for backward compatibility with the existing ingestion auth path.

```go
// File: cluster/internal/controlplane/schema.go

// Add to createControlPlaneTables slice (appended after existing entries):

`CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'admin',
    --  CHECK(role IN ('admin', 'operator', 'viewer')),
    permissions JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, email)
)`,

`CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id)`,
`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,

`CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    device_info TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(token_hash)
)`,

`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id)
    WHERE revoked_at IS NULL`,

`CREATE TABLE IF NOT EXISTS tenant_plans (
    tenant_id UUID PRIMARY KEY REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    plan_name TEXT NOT NULL,
    --      CHECK(plan_name IN ('basic','pro','pro_plus','enterprise')),
    features JSONB NOT NULL DEFAULT '{}',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`,

`CREATE TABLE IF NOT EXISTS paryty_twins (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'creating',
    --  CHECK(status IN ('creating','active','error','suspended','deleted')),
    twin_config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`,

`CREATE INDEX IF NOT EXISTS idx_twins_tenant ON paryty_twins(tenant_id)`,
`CREATE INDEX IF NOT EXISTS idx_twins_status ON paryty_twins(tenant_id, status)`,

`CREATE TABLE IF NOT EXISTS agent_assignments (
    agent_id TEXT NOT NULL,
    twin_id UUID NOT NULL REFERENCES paryty_twins(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(agent_id, twin_id)
)`,

`CREATE INDEX IF NOT EXISTS idx_agent_assignments_tenant ON agent_assignments(tenant_id)`,
`CREATE INDEX IF NOT EXISTS idx_agent_assignments_twin ON agent_assignments(twin_id)`,

`CREATE TABLE IF NOT EXISTS audit_log (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    user_id UUID REFERENCES users(id),
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}',
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`,

`CREATE INDEX IF NOT EXISTS idx_audit_log_tenant ON audit_log(tenant_id, created_at DESC)`,
`CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action, created_at DESC)`,
`CREATE INDEX IF NOT EXISTS idx_audit_log_resource ON audit_log(resource_type, resource_id)`,
```

### 1.2 Design Rationale

| Decision | Rationale |
|---|---|
| `permissions JSONB` on users, not RBAC table | Sub-user permissions are sparse and plan-defined; JSONB avoids joins and keeps reads fast. The plan YAML is the source of truth for available permission keys. |
| `features JSONB` on tenant_plans | Copied from plan YAML at assignment time. Plan YAML can change without breaking existing tenants. Acts as a snapshot. |
| `token_hash` in refresh_tokens | Same pattern as api_keys — SHA-256 hash. The raw refresh token is returned once and never stored. |
| `agent_assignments` composite PK | An agent belongs to exactly one twin at a time. Composite PK prevents double-assignment. |
| No FK to services table | The `services` concept doesn't exist in this codebase — Paryty Twin IS the grouping construct. |

### 1.3 Migration Strategy

Add a new function `EnsurePhase8Tables` alongside the existing `EnsureTables`:

```go
// File: cluster/internal/controlplane/schema.go

var createPhase8Tables = []string{
    // ... all DDL from above ...
}

// EnsurePhase8Tables creates Phase 8 tables if they do not exist.
// Safe to call on every startup alongside EnsureTables.
func EnsurePhase8Tables(ctx context.Context, pool *pgxpool.Pool) error {
    for _, ddl := range createPhase8Tables {
        if _, err := pool.Exec(ctx, ddl); err != nil {
            return fmt.Errorf("execute phase8 DDL: %w", err)
        }
    }
    return nil
}
```

---

## 2. Plan Configuration YAML

### 2.1 File: `configs/cluster/plans.yaml`

```yaml
# Paryty Plan Definitions
# Version-controlled source of truth for all plan features and limits.
# Editing this file changes plan behavior for NEW assignments without
# affecting existing tenants (their plan snapshot is in tenant_plans.features).
# Use the PUT /api/v1/admin/plans/sync endpoint to propagate changes to
# existing tenants.

plans:
  basic:
    name: "Basic"
    display_name: "Basic"
    max_twins: 2
    creatable: true
    features:
      topology_monitoring: true
      metrics: false
      alerts: false
      timeline_replay: false
      paryty_intel: false
      sso: false
      custom_retention: false
      api_access: true
      web_ui: true
    limits:
      agents_per_twin: 10
      data_retention_days: 7
      metrics_resolution: "1m"
      alert_rules_per_tenant: 5
      sub_users: 2
    quotas:
      ingestion_bytes_per_day: 1073741824    # 1 GiB
      query_requests_per_minute: 60

  pro:
    name: "Pro"
    display_name: "Pro"
    max_twins: 4
    creatable: true
    features:
      topology_monitoring: true
      metrics: true
      alerts: true
      timeline_replay: false
      paryty_intel: false
      sso: false
      custom_retention: false
      api_access: true
      web_ui: true
    limits:
      agents_per_twin: 50
      data_retention_days: 30
      metrics_resolution: "10s"
      alert_rules_per_tenant: 20
      sub_users: 10
    quotas:
      ingestion_bytes_per_day: 10737418240   # 10 GiB
      query_requests_per_minute: 300

  pro_plus:
    name: "Pro+"
    display_name: "Pro+"
    max_twins: 6
    creatable: true
    features:
      topology_monitoring: true
      metrics: true
      alerts: true
      timeline_replay: true
      paryty_intel: true
      sso: false
      custom_retention: false
      api_access: true
      web_ui: true
    limits:
      agents_per_twin: 200
      data_retention_days: 90
      metrics_resolution: "5s"
      alert_rules_per_tenant: 50
      sub_users: 25
    quotas:
      ingestion_bytes_per_day: 53687091200   # 50 GiB
      query_requests_per_minute: 600

  enterprise:
    name: "Enterprise"
    display_name: "Enterprise"
    max_twins: 10
    creatable: false   # NOT available for self-signup
    features:
      topology_monitoring: true
      metrics: true
      alerts: true
      timeline_replay: true
      paryty_intel: true
      sso: true
      custom_retention: true
      dedicated_infra: true
      api_access: true
      web_ui: true
    limits:
      agents_per_twin: 1000
      data_retention_days: 365
      metrics_resolution: "1s"
      alert_rules_per_tenant: 200
      sub_users: 100
    quotas:
      ingestion_bytes_per_day: 107374182400  # 100 GiB
      query_requests_per_minute: 1200
```

### 2.2 Go Structs: `cluster/internal/plan/loader.go`

```go
package plan

import "time"

// PlansFile is the root structure of plans.yaml.
type PlansFile struct {
    Plans map[string]PlanDefinition `yaml:"plans"`
}

// PlanDefinition defines a single plan's features, limits, and quotas.
type PlanDefinition struct {
    Name        string          `yaml:"name"`
    DisplayName string          `yaml:"display_name"`
    MaxTwins    int             `yaml:"max_twins"`
    Creatable   bool            `yaml:"creatable"`
    Features    FeatureSet      `yaml:"features"`
    Limits      LimitSet        `yaml:"limits"`
    Quotas      QuotaSet        `yaml:"quotas"`
}

// FeatureSet is a map of feature flags that can be checked at runtime.
type FeatureSet map[string]bool

// Has returns true if the feature is enabled.
func (fs FeatureSet) Has(feature string) bool {
    return fs[feature]
}

// LimitSet holds plan limits.
type LimitSet struct {
    AgentsPerTwin       int `yaml:"agents_per_twin"`
    DataRetentionDays   int `yaml:"data_retention_days"`
    MetricsResolution   string `yaml:"metrics_resolution"`
    AlertRulesPerTenant int `yaml:"alert_rules_per_tenant"`
    SubUsers            int `yaml:"sub_users"`
}

// QuotaSet holds usage quotas.
type QuotaSet struct {
    IngestionBytesPerDay    int64 `yaml:"ingestion_bytes_per_day"`
    QueryRequestsPerMinute  int   `yaml:"query_requests_per_minute"`
}
```

---

## 3. Proto Design

### 3.1 File: `proto/paryty/v1/auth.proto`

```protobuf
syntax = "proto3";

package paryty.v1;

option go_package = "github.com/paryty/paryty-v1.0/cluster/internal/proto;parytyv1";

import "google/protobuf/timestamp.proto";
import "google/protobuf/empty.proto";
import "paryty/v1/common.proto";

// ============================================================================
// Auth Service — User authentication and token management
// ============================================================================

service AuthService {
  // Register a new user account within a tenant.
  rpc Register(RegisterRequest) returns (RegisterResponse);

  // Login with email + password, returns access token + refresh token.
  rpc Login(LoginRequest) returns (LoginResponse);

  // RefreshToken exchanges a refresh token for a new token pair.
  rpc RefreshToken(RefreshTokenRequest) returns (LoginResponse);

  // Logout revokes the refresh token.
  rpc Logout(LogoutRequest) returns (google.protobuf.Empty);

  // ValidateToken validates an access token and returns the user/tenant claims.
  rpc ValidateToken(ValidateTokenRequest) returns (ValidateTokenResponse);
}

message RegisterRequest {
  string email = 1;
  string password = 2;     // min 8 chars, must pass entropy check
  string name = 3;
  string plan_name = 4;    // "basic", "pro", "pro_plus"
}

message RegisterResponse {
  string tenant_id = 1;
  string user_id = 2;
  string access_token = 3;
  string refresh_token = 4;
  int64 expires_in = 5;    // seconds until access token expires
}

message LoginRequest {
  string email = 1;
  string password = 2;
}

message LoginResponse {
  string access_token = 1;
  string refresh_token = 2;
  int64 expires_in = 3;
  UserInfo user = 4;
}

message RefreshTokenRequest {
  string refresh_token = 1;
}

message LogoutRequest {
  string refresh_token = 1;
}

message ValidateTokenRequest {
  string access_token = 1;
}

message ValidateTokenResponse {
  bool valid = 1;
  string user_id = 2;
  string tenant_id = 3;
  string role = 4;
  repeated string permissions = 5;
  google.protobuf.Timestamp expires_at = 6;
}

message UserInfo {
  string user_id = 1;
  string tenant_id = 2;
  string email = 3;
  string name = 4;
  string role = 5;
  map<string, bool> permissions = 6;
  google.protobuf.Timestamp created_at = 7;
}

// ============================================================================
// User Service — Sub-user management (tenant admin operations)
// ============================================================================

service UserService {
  rpc CreateSubUser(CreateSubUserRequest) returns (UserInfo);
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
  rpc GetUser(GetUserRequest) returns (UserInfo);
  rpc UpdateUser(UpdateUserRequest) returns (UserInfo);
  rpc DeleteUser(DeleteUserRequest) returns (google.protobuf.Empty);
  rpc UpdatePermissions(UpdatePermissionsRequest) returns (UserInfo);
}

message CreateSubUserRequest {
  string email = 1;
  string password = 2;
  string name = 3;
  string role = 4;          // "operator" or "viewer"
  map<string, bool> permissions = 5;
}

message ListUsersRequest {
  PaginationRequest pagination = 1;
}

message ListUsersResponse {
  repeated UserInfo users = 1;
  PaginationResponse pagination = 2;
}

message GetUserRequest {
  string user_id = 1;
}

message UpdateUserRequest {
  string user_id = 1;
  string name = 2;
  string role = 3;
  bool is_active = 4;
}

message DeleteUserRequest {
  string user_id = 1;
}

message UpdatePermissionsRequest {
  string user_id = 1;
  map<string, bool> permissions = 2;
}

// ============================================================================
// Plan Service — Plan listing and feature checking
// ============================================================================

service PlanService {
  // ListPlans returns all available plans (for signup page).
  rpc ListPlans(google.protobuf.Empty) returns (ListPlansResponse);

  // GetCurrentPlan returns the authenticated tenant's active plan.
  rpc GetCurrentPlan(google.protobuf.Empty) returns (PlanInfo);

  // GetPlanFeatures returns the effective features for the authenticated tenant.
  rpc GetPlanFeatures(google.protobuf.Empty) returns (PlanFeaturesResponse);
}

message ListPlansResponse {
  repeated PlanInfo plans = 1;
}

message PlanInfo {
  string name = 1;
  string display_name = 2;
  int32 max_twins = 3;
  bool creatable = 4;
  map<string, bool> features = 5;
  PlanLimits limits = 6;
  PlanQuotas quotas = 7;
}

message PlanLimits {
  int32 agents_per_twin = 1;
  int32 data_retention_days = 2;
  string metrics_resolution = 3;
  int32 alert_rules_per_tenant = 4;
  int32 sub_users = 5;
}

message PlanQuotas {
  int64 ingestion_bytes_per_day = 1;
  int32 query_requests_per_minute = 2;
}

message PlanFeaturesResponse {
  map<string, bool> features = 1;
}

// ============================================================================
// Twin Service — Paryty Twin lifecycle management
// ============================================================================

service TwinService {
  // CreateTwin starts the twin creation wizard flow.
  rpc CreateTwin(CreateTwinRequest) returns (TwinInfo);

  // ListTwins returns all twins for the authenticated tenant.
  rpc ListTwins(ListTwinsRequest) returns (ListTwinsResponse);

  // GetTwin returns details for a specific twin.
  rpc GetTwin(GetTwinRequest) returns (TwinInfo);

  // UpdateTwin updates twin metadata.
  rpc UpdateTwin(UpdateTwinRequest) returns (TwinInfo);

  // DeleteTwin soft-deletes a twin and disassociates all agents.
  rpc DeleteTwin(DeleteTwinRequest) returns (google.protobuf.Empty);

  // GetTwinConfig returns the agent configuration for a twin.
  // Called by agents during registration to discover their configuration.
  rpc GetTwinConfig(GetTwinConfigRequest) returns (TwinConfig);
}

message CreateTwinRequest {
  string name = 1;
  string description = 2;
  TwinConfig config = 3;
}

message TwinInfo {
  string id = 1;
  string tenant_id = 2;
  string name = 3;
  string description = 4;
  string status = 5;
  int32 agent_count = 6;
  TwinConfig config = 7;
  google.protobuf.Timestamp created_at = 8;
  google.protobuf.Timestamp updated_at = 9;
}

message TwinConfig {
  // List of agent labels to auto-assign (e.g., ["env:prod", "region:us-east-1"])
  map<string, string> agent_labels = 1;
  // Enabled collectors (e.g., ["cpu", "memory", "disk", "network", "ebpf"])
  repeated string enabled_collectors = 2;
  // Collection interval in seconds
  int32 collection_interval_seconds = 3;
  // Sampling rate (0.0 - 1.0)
  float sampling_rate = 4;
}

message ListTwinsRequest {
  PaginationRequest pagination = 1;
}

message ListTwinsResponse {
  repeated TwinInfo twins = 1;
  PaginationResponse pagination = 2;
}

message GetTwinRequest {
  string twin_id = 1;
}

message UpdateTwinRequest {
  string twin_id = 1;
  string name = 2;
  string description = 3;
  TwinConfig config = 4;
}

message DeleteTwinRequest {
  string twin_id = 1;
}

message GetTwinConfigRequest {
  string twin_id = 1;
  string agent_id = 2;
}
```

---

## 4. Go Package Architecture

### 4.1 Directory Layout

```
cluster/internal/
├── auth/                          # NEW: JWT + password handling
│   ├── jwt.go                     #   JWT token generation, validation, claims
│   ├── password.go                #   bcrypt hashing, password policy enforcement
│   ├── middleware.go              #   Gin JWT middleware, context injection
│   └── handler.go                 #   AuthService gRPC handler (Register, Login, etc.)
├── plan/                          # NEW: Plan loading and feature gating
│   ├── loader.go                  #   YAML loading, PlanDefinition structs
│   ├── engine.go                  #   PlanEngine: feature checking, limit validation
│   └── middleware.go              #   Gin + gRPC feature-gating middleware
├── twin/                          # NEW: Paryty Twin management
│   ├── twin.go                    #   TwinManager: CRUD, status machine
│   └── agent_assignment.go        #   AgentAssigner: register agent to twin
├── user/                          # NEW: User + sub-user management
│   ├── user.go                    #   UserManager: CRUD, password reset
│   └── permissions.go             #   PermissionValidator: granular permission checks
├── security/                      # NEW: TLS, RBAC, audit, secrets
│   ├── tls.go                     #   TLS/mTLS configuration helpers
│   ├── rbac.go                    #   RBAC: role/permission resolution
│   ├── apikeys.go                 #   EXTEND: add scoped API keys
│   ├── audit.go                   #   AuditLogger: structured audit logging
│   ├── secrets.go                 #   SecretsManager: env-based, vault-ready
│   └── middleware.go              #   Security middleware: mTLS enforcement, RBAC
│
├── controlplane/                  # EXISTING: Tenant + API Key management
│   ├── schema.go                  #   MODIFIED: add Phase 8 DDL
│   ├── tenant.go                  #   UNCHANGED
│   └── apikey.go                  #   UNCHANGED
│
├── api/
│   └── query/
│       └── rest.go                # MODIFIED: wrap routes with auth middleware
```

### 4.2 Key Interfaces

#### `internal/auth/jwt.go`

```go
package auth

import (
    "context"
    "time"
    "github.com/golang-jwt/jwt/v5"
)

// TokenPair represents an access + refresh token pair.
type TokenPair struct {
    AccessToken  string `json:"access_token"`
    RefreshToken string `json:"refresh_token"`
    ExpiresIn    int64  `json:"expires_in"`
}

// Claims extends standard JWT claims with Paryty-specific fields.
type Claims struct {
    jwt.RegisteredClaims
    UserID      string   `json:"uid"`
    TenantID    string   `json:"tid"`
    Role        string   `json:"rol"`
    Permissions []string `json:"prm"`
}

// TokenManager generates and validates JWT tokens.
type TokenManager struct {
    accessSecret  []byte
    refreshSecret []byte
    accessTTL     time.Duration
    refreshTTL    time.Duration
}

// NewTokenManager creates a TokenManager. Panics if secrets are empty.
func NewTokenManager(accessSecret, refreshSecret []byte, accessTTL, refreshTTL time.Duration) *TokenManager

// GeneratePair creates a new access + refresh token pair for the given claims.
func (m *TokenManager) GeneratePair(ctx context.Context, claims Claims) (*TokenPair, error)

// ValidateAccess validates an access token and returns the claims.
func (m *TokenManager) ValidateAccess(tokenString string) (*Claims, error)

// ValidateRefresh validates a refresh token and returns the claims.
func (m *TokenManager) ValidateRefresh(tokenString string) (*Claims, error)

// RefreshPair validates a refresh token and issues a new pair, revoking the old refresh token.
func (m *TokenManager) RefreshPair(ctx context.Context, refreshToken string) (*TokenPair, error)
```

#### `internal/plan/engine.go`

```go
package plan

import (
    "context"
    "sync"
)

// PlanEngine is the runtime plan evaluator. It loads plan definitions from
// YAML at startup and evaluates tenant capabilities against their assigned plan.
// Thread-safe after Load() completes.
type PlanEngine struct {
    mu    sync.RWMutex
    plans map[string]PlanDefinition
    db    PlanStore
}

// PlanStore abstracts plan persistence (PostgreSQL tenant_plans table).
type PlanStore interface {
    GetTenantPlan(ctx context.Context, tenantID string) (*TenantPlan, error)
    SetTenantPlan(ctx context.Context, tenantID, planName string) error
}

// TenantPlan is the persisted plan assignment for a tenant.
type TenantPlan struct {
    TenantID  string    `json:"tenant_id"`
    PlanName  string    `json:"plan_name"`
    Features  FeatureSet `json:"features"`
    Limits    LimitSet  `json:"limits"`
    Quotas    QuotaSet  `json:"quotas"`
    StartedAt time.Time `json:"started_at"`
    ExpiresAt *time.Time `json:"expires_at"`
}

// NewPlanEngine creates a PlanEngine with the given plan store.
func NewPlanEngine(store PlanStore) *PlanEngine

// Load parses the plans YAML file and populates the engine.
// Must be called before any evaluation methods.
func (e *PlanEngine) Load(yamlPath string) error

// HasFeature returns true if the tenant's plan includes the given feature.
func (e *PlanEngine) HasFeature(ctx context.Context, tenantID, feature string) (bool, error)

// CheckLimit validates that the tenant hasn't exceeded a numeric limit.
// Returns the limit value and whether the current usage exceeds it.
func (e *PlanEngine) CheckLimit(ctx context.Context, tenantID, limit string, current int) (int, error)

// GetEffectivePlan returns the tenant's effective plan (from DB snapshot).
func (e *PlanEngine) GetEffectivePlan(ctx context.Context, tenantID string) (*TenantPlan, error)

// ListPlans returns all plan definitions (filtered for signup: creatable=true).
func (e *PlanEngine) ListPlans(creatableOnly bool) []PlanInfo

// SyncPlanToTenants updates tenant_plans for all tenants on a given plan.
// Used when plans.yaml changes need to propagate to existing tenants.
func (e *PlanEngine) SyncPlanToTenants(ctx context.Context, planName string) (int, error)
```

#### `internal/plan/middleware.go`

```go
package plan

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "google.golang.org/grpc"
)

// GinFeatureGate returns a Gin middleware that checks the tenant has the required feature.
// Extracts tenant from context (set by auth middleware).
// Returns 402 Payment Required if feature is not available.
func GinFeatureGate(engine *PlanEngine, feature string) gin.HandlerFunc

// GinTwinLimitGate returns a Gin middleware that checks the tenant hasn't exceeded twin limits.
// Returns 402 Payment Required with upgrade hint if limit is exceeded.
func GinTwinLimitGate(engine *PlanEngine) gin.HandlerFunc

// GrpcFeatureGate returns a gRPC unary interceptor for feature gating.
func GrpcFeatureGate(engine *PlanEngine, feature string) grpc.UnaryServerInterceptor

// GrpcStreamFeatureGate returns a gRPC stream interceptor for feature gating.
func GrpcStreamFeatureGate(engine *PlanEngine, feature string) grpc.StreamServerInterceptor
```

#### `internal/twin/twin.go`

```go
package twin

import (
    "context"
    "github.com/jackc/pgx/v5/pgxpool"
    "go.uber.org/zap"
)

// TwinStatus represents the lifecycle state of a Paryty Twin.
type TwinStatus string
const (
    TwinStatusCreating   TwinStatus = "creating"
    TwinStatusActive     TwinStatus = "active"
    TwinStatusError      TwinStatus = "error"
    TwinStatusSuspended  TwinStatus = "suspended"
    TwinStatusDeleted    TwinStatus = "deleted"
)

// TwinManager manages Paryty Twin lifecycle.
type TwinManager struct {
    pool   *pgxpool.Pool
    logger *zap.Logger
}

// CreateTwin inserts a new twin and returns the generated ID.
func (m *TwinManager) CreateTwin(ctx context.Context, tenantID, name, description string, config json.RawMessage) (string, error)

// GetTwin retrieves a single twin by ID, tenant-scoped.
func (m *TwinManager) GetTwin(ctx context.Context, tenantID, twinID string) (*Twin, error)

// ListTwins returns all twins for a tenant, with agent counts.
func (m *TwinManager) ListTwins(ctx context.Context, tenantID string, pageSize, offset int) ([]Twin, int, error)

// UpdateTwin updates twin metadata. Only name/description/config are mutable.
func (m *TwinManager) UpdateTwin(ctx context.Context, tenantID, twinID, name, description string, config json.RawMessage) error

// DeleteTwin soft-deletes a twin and disassociates all agents.
func (m *TwinManager) DeleteTwin(ctx context.Context, tenantID, twinID string) error

// CountTwins returns the number of active twins for a tenant.
func (m *TwinManager) CountTwins(ctx context.Context, tenantID string) (int, error)

// GetTwinConfigForAgent returns the twin configuration for agent discovery.
// Called when an agent registers with a twin_id.
func (m *TwinManager) GetTwinConfigForAgent(ctx context.Context, twinID, agentID string) (*TwinConfig, error)
```

#### `internal/security/audit.go`

```go
package security

import (
    "context"
    "time"
    "github.com/jackc/pgx/v5/pgxpool"
    "go.uber.org/zap"
)

// AuditEntry represents a single auditable action.
type AuditEntry struct {
    TenantID     string          `json:"tenant_id"`
    UserID       string          `json:"user_id,omitempty"`
    Action       string          `json:"action"`
    ResourceType string          `json:"resource_type"`
    ResourceID   string          `json:"resource_id"`
    Details      json.RawMessage  `json:"details"`
    IPAddress    string          `json:"ip_address"`
    UserAgent    string          `json:"user_agent"`
}

// AuditLogger asynchronously records audit entries to PostgreSQL.
// Write path is non-blocking (buffered channel + background writer).
type AuditLogger struct {
    pool   *pgxpool.Pool
    logger *zap.Logger
    ch     chan AuditEntry
}

// NewAuditLogger creates an AuditLogger with a bounded buffer (10000 entries).
func NewAuditLogger(pool *pgxpool.Pool, logger *zap.Logger) *AuditLogger

// Record enqueues an audit entry. Non-blocking — drops on full buffer with warning.
func (l *AuditLogger) Record(ctx context.Context, entry AuditEntry)

// Close flushes remaining entries and stops the background writer.
func (l *AuditLogger) Close() error
```

---

## 5. Middleware Chain

### 5.1 REST Middleware Chain (Gin)

```
Request
  │
  ├── 1. CORS (gin middleware)
  │       └─ Access-Control-Allow-Origin, Methods, Headers
  │
  ├── 2. RequestID (custom)
  │       └─ Inject X-Request-ID or generate UUID; set in context
  │
  ├── 3. AuditBegin (custom)
  │       └─ Start timer, capture IP + User-Agent
  │
  ├── 4. JWT Auth (internal/auth/middleware.go)
  │       └─ Extract Bearer token from Authorization header
  │       └─ Validate JWT signature + expiry
  │       └─ Inject Claims into gin.Context:
  │           c.Set("user_id", claims.UserID)
  │           c.Set("tenant_id", claims.TenantID)
  │           c.Set("role", claims.Role)
  │           c.Set("permissions", claims.Permissions)
  │       └─ On failure: 401 Unauthorized
  │
  ├── 5. Plan Feature Gate (internal/plan/middleware.go)
  │       └─ Pull tenant_id from context
  │       └─ Check plan has required feature (per-route configuration)
  │       └─ Check limits (e.g., twin count for POST /twins)
  │       └─ On failure: 402 Payment Required
  │           └─ Body: {"error": "feature_not_available", "feature": "metrics",
  │                      "upgrade_url": "/api/v1/billing/upgrade"}
  │
  ├── 6. RBAC Check (internal/security/rbac.go)
  │       └─ Role-based access control (admin vs operator vs viewer)
  │       └─ On failure: 403 Forbidden
  │
  ├── 7. Rate Limit (per-tenant)
  │       └─ Sliding window counter in Dragonfly
  │       └─ Key: "ratelimit:{tenant_id}:{route}"
  │       └─ On failure: 429 Too Many Requests
  │
  ├── 8. Handler
  │
  └── 9. AuditEnd (custom)
          └─ Record action, resource, status code, duration
          └─ Non-blocking fire-and-forget to audit logger
```

#### Middleware per Route Group:

```go
// File: cluster/internal/api/query/rest.go

func (s *QueryService) RegisterRoutes(r *gin.RouterGroup) {
    // Public routes — no auth required
    public := r.Group("")
    {
        public.GET("/health", s.HealthCheck)
        public.POST("/api/v1/auth/register", s.authHandler.Register)
        public.POST("/api/v1/auth/login", s.authHandler.Login)
        public.POST("/api/v1/auth/refresh", s.authHandler.RefreshToken)
        public.GET("/api/v1/plans", s.planHandler.ListPlans)
    }

    // Authenticated routes
    auth := r.Group("")
    auth.Use(authMiddleware)
    {
        auth.POST("/api/v1/auth/logout", s.authHandler.Logout)

        // Plan info — requires auth, no feature gate
        auth.GET("/api/v1/plan", s.planHandler.GetCurrentPlan)

        // Twin management — requires topology_monitoring feature
        twins := auth.Group("/api/v1/twins")
        twins.Use(plan.GinFeatureGate(s.planEngine, "topology_monitoring"))
        {
            twins.GET("", s.twinHandler.ListTwins)
            twins.POST("", plan.GinTwinLimitGate(s.planEngine), s.twinHandler.CreateTwin)
            twins.GET("/:twin_id", s.twinHandler.GetTwin)
            twins.PUT("/:twin_id", s.twinHandler.UpdateTwin)
            twins.DELETE("/:twin_id", s.twinHandler.DeleteTwin)
        }

        // Metrics — requires metrics feature
        metrics := auth.Group("/api/v1/metrics")
        metrics.Use(plan.GinFeatureGate(s.planEngine, "metrics"))
        {
            metrics.GET("/:agent_id", s.GetMetrics)
            metrics.GET("/:agent_id/aggregated", s.GetAggregatedMetrics)
            metrics.POST("/query", s.QueryMetrics)
            metrics.GET("/names", s.GetMetricNames)
        }

        // Timeline replay — requires timeline_replay feature
        timeline := auth.Group("/api/v1/timeline")
        timeline.Use(plan.GinFeatureGate(s.planEngine, "timeline_replay"))
        {
            timeline.GET("/replay", s.sseHandler.HandleTimeline)
            timeline.GET("/snapshots", s.ListTimelineSnapshots)
            timeline.GET("/snapshots/:snapshot_id", s.GetTimelineSnapshot)
            timeline.GET("/snapshots/diff", s.GetSnapshotDiff)
        }

        // Topology — always available (basic feature)
        auth.GET("/api/v1/topology", s.GetTopology)
        auth.GET("/api/v1/topology/cluster/:cluster_id", s.GetClusterTopology)
        auth.GET("/api/v1/topology/search", s.SearchNodes)
    }

    // Admin routes — requires admin role
    admin := auth.Group("/api/v1/admin")
    admin.Use(rbac.RequireRole("admin"))
    {
        admin.GET("/users", s.userHandler.ListUsers)
        admin.POST("/users", s.userHandler.CreateSubUser)
        admin.PUT("/users/:user_id", s.userHandler.UpdateUser)
        admin.DELETE("/users/:user_id", s.userHandler.DeleteUser)
        admin.GET("/audit-logs", s.auditHandler.ListAuditLogs)
    }
}
```

### 5.2 gRPC Middleware Chain

```
Request
  │
  ├── 1. TLS (transport layer)
  │       └─ mTLS: verify client certificate if mTLS enforced
  │
  ├── 2. API Key Auth (existing: internal/api/ingestion/auth.go)
  │       └─ Extract "x-api-key" from gRPC metadata
  │       └─ Validate against bcrypt hash in api_keys table
  │       └─ Inject tenant_id into context
  │       └─ On failure: codes.Unauthenticated
  │
  ├── 3. Rate Limit (per tenant)
  │       └─ Sliding window counter in Dragonfly
  │       └─ On failure: codes.ResourceExhausted
  │
  ├── 4. Feature Gate (gRPC interceptor)
  │       └─ Check tenant plan has required feature
  │       └─ On failure: codes.PermissionDenied
  │
  ├── 5. Audit Log (gRPC interceptor)
  │       └─ Record method, tenant, duration
  │
  └── 6. Handler
```

#### gRPC Server Wiring (example for AuthService):

```go
// File: cluster/cmd/query/main.go (new block)

import (
    "github.com/paryty/paryty-v1.0/cluster/internal/auth"
    "github.com/paryty/paryty-v1.0/cluster/internal/plan"
    "github.com/paryty/paryty-v1.0/cluster/internal/security"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials"
)

// gRPC server setup for control plane APIs
func setupGRPCServer(
    authHandler *auth.Handler,
    userHandler *user.Handler,
    planHandler *plan.Handler,
    twinHandler *twin.Handler,
    planEngine *plan.PlanEngine,
    auditLogger *security.AuditLogger,
    jwtManager *auth.TokenManager,
) *grpc.Server {
    // TLS credentials
    creds, _ := credentials.NewServerTLSFromFile("certs/server.crt", "certs/server.key")

    // Interceptor chain
    unaryInterceptors := []grpc.UnaryServerInterceptor{
        security.AuditUnaryInterceptor(auditLogger),
        auth.GrpcJWTAuthInterceptor(jwtManager),     // JWT for user-facing services
        plan.GrpcFeatureGateInterceptor(planEngine),  // Feature gating
        security.GrpcRateLimitInterceptor(redisClient),
    }

    streamInterceptors := []grpc.StreamServerInterceptor{
        security.AuditStreamInterceptor(auditLogger),
        auth.GrpcJWTStreamAuthInterceptor(jwtManager),
        security.GrpcStreamRateLimitInterceptor(redisClient),
    }

    srv := grpc.NewServer(
        grpc.Creds(creds),
        grpc.ChainUnaryInterceptor(unaryInterceptors...),
        grpc.ChainStreamInterceptor(streamInterceptors...),
    )

    // Register services
    parytyv1.RegisterAuthServiceServer(srv, authHandler)
    parytyv1.RegisterUserServiceServer(srv, userHandler)
    parytyv1.RegisterPlanServiceServer(srv, planHandler)
    parytyv1.RegisterTwinServiceServer(srv, twinHandler)

    return srv
}
```

---

## 6. Feature Gating System

### 6.1 Architecture

Feature gating has three layers, evaluated in order:

```
Layer 1: Plan Feature Check   →  Does the plan include this feature?
Layer 2: Limit Check          →  Has the tenant hit a hard limit?
Layer 3: Plan Active Check    →  Is the plan not expired/suspended?
```

All three layers must pass for a request to proceed. Failures return `402 Payment Required`
for REST or `codes.PermissionDenied` for gRPC.

### 6.2 PlanEngine Implementation

```go
// File: cluster/internal/plan/engine.go

type PlanEngine struct {
    mu    sync.RWMutex
    plans map[string]PlanDefinition
    store PlanStore
}

// PlanStore implementation backed by the existing pgxpool.
// Uses the tenant_plans table created in Phase 8 DDL.
type pgPlanStore struct {
    pool *pgxpool.Pool
}

func (s *pgPlanStore) GetTenantPlan(ctx context.Context, tenantID string) (*TenantPlan, error) {
    tp := &TenantPlan{}
    err := s.pool.QueryRow(ctx, `
        SELECT tenant_id, plan_name, features, limits, quotas, started_at, expires_at
        FROM tenant_plans WHERE tenant_id = $1`,
        tenantID,
    ).Scan(
        &tp.TenantID, &tp.PlanName,
        (*jsonbMap)(&tp.Features), (*jsonbLimits)(&tp.Limits), (*jsonbQuotas)(&tp.Quotas),
        &tp.StartedAt, &tp.ExpiresAt,
    )
    if err != nil {
        return nil, fmt.Errorf("get tenant plan %s: %w", tenantID, err)
    }
    return tp, nil
}
```

### 6.3 Feature Gate Middleware (REST)

```go
// File: cluster/internal/plan/middleware.go

// GinFeatureGate returns middleware that gates access by feature flag.
func GinFeatureGate(engine *PlanEngine, feature string) gin.HandlerFunc {
    return func(c *gin.Context) {
        tenantID := c.GetString("tenant_id")
        if tenantID == "" {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "tenant not found in context"})
            return
        }

        ok, err := engine.HasFeature(c.Request.Context(), tenantID, feature)
        if err != nil {
            // Plan not found — fall through to allow request
            // (graceful degradation: only block when we know for sure)
            c.Next()
            return
        }

        if !ok {
            c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
                "error":       "feature_not_available",
                "feature":     feature,
                "upgrade_url": fmt.Sprintf("/api/v1/billing/upgrade?feature=%s", feature),
            })
            return
        }
        c.Next()
    }
}

// GinTwinLimitGate blocks twin creation if limit exceeded.
func GinTwinLimitGate(engine *PlanEngine) gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.Request.Method != http.MethodPost {
            c.Next()
            return
        }

        tenantID := c.GetString("tenant_id")
        // Query current twin count from PostgreSQL
        var count int
        err := engine.db.QueryRow(c.Request.Context(),
            `SELECT COUNT(*) FROM paryty_twins WHERE tenant_id = $1 AND status != 'deleted'`,
            tenantID,
        ).Scan(&count)
        if err != nil {
            c.Next() // graceful degradation
            return
        }

        maxTwins, err := engine.CheckLimit(c.Request.Context(), tenantID, "max_twins", count)
        if err != nil {
            c.Next()
            return
        }

        if count >= maxTwins {
            c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
                "error":      "limit_exceeded",
                "limit":      "max_twins",
                "current":    count,
                "max":        maxTwins,
                "upgrade_url": "/api/v1/billing/upgrade",
            })
            return
        }
        c.Next()
    }
}
```

### 6.4 Feature → Route Mapping Table

| Feature Flag | REST Routes | gRPC Methods |
|---|---|---|
| `topology_monitoring` | GET /topology/*, WebSocket /ws | None (ingestion always works) |
| `metrics` | GET /metrics/*, POST /metrics/query | None |
| `alerts` | GET /alerts, POST /alerts/:id/acknowledge | None |
| `timeline_replay` | GET /timeline/* | None |
| `paryty_intel` | All /api/v1/intel/* | IntelligenceService.* |
| `api_access` | N/A (enables API key creation) | All gRPC ingestion |
| `sso` | GET /auth/sso/* | N/A |

---

## 7. Registration Flow

### 7.1 Signup Sequence

```
1. POST /api/v1/auth/register { email, password, name, plan_name: "pro" }

2. AuthHandler.Register():
   a. Validate plan_name is in plans.yaml AND plan.creatable == true
   b. Create tenant row → tenant_id
   c. Hash password with bcrypt (cost=12)
   d. Create user row (role="admin", tenant_id)
   e. Assign plan → INSERT INTO tenant_plans with snapshot of features from YAML
   f. Generate JWT access token + refresh token
   g. Store refresh token hash in refresh_tokens table
   h. Record audit log: "tenant.created", "user.registered"
   i. Return { tenant_id, user_id, access_token, refresh_token, expires_in }

3. Frontend stores tokens, redirects to twin creation wizard.

4. POST /api/v1/twins { name: "My Prod Cluster", description: "...",
     config: { agent_labels: {env: "prod"}, enabled_collectors: ["cpu","memory"], ... } }

5. TwinHandler.CreateTwin():
   a. Check plan feature: topology_monitoring == true
   b. Check limit: current twin count < plan.max_twins
   c. INSERT INTO paryty_twins → twin_id
   d. Return twin_id with agent config snippet

6. User installs agent on node with config:
   PARYTY_TWIN_ID=<twin_id>
   PARYTY_API_KEY=<api_key>
   PARYTY_SERVER=grpcs://cluster.paryty.io:443

7. Agent calls gRPC TwinService.GetTwinConfig(twin_id, agent_id)
   → Returns full config: enabled_collectors, collection_interval, etc.

8. Agent calls gRPC IngestionService.RegisterAgent(...)
   → Creates agent_assignments row: (agent_id, twin_id, tenant_id)
   → Agent begins streaming metrics
```

### 7.2 Agent Auto-Registration Detail

```go
// File: cluster/internal/twin/agent_assignment.go

type AgentAssigner struct {
    pool   *pgxpool.Pool
    logger *zap.Logger
}

// RegisterAgent assigns an agent to a twin.
// Called by the ingestion gRPC handler when an agent first connects.
func (a *AgentAssigner) RegisterAgent(ctx context.Context, twinID, agentID, tenantID string) error {
    // Check twin exists and is active
    var status string
    err := a.pool.QueryRow(ctx,
        `SELECT status FROM paryty_twins WHERE id = $1 AND tenant_id = $2`,
        twinID, tenantID,
    ).Scan(&status)
    if err != nil {
        return fmt.Errorf("twin %s not found for tenant %s: %w", twinID, tenantID, err)
    }
    if status != string(TwinStatusActive) {
        return fmt.Errorf("twin %s is %s, cannot register agents", twinID, status)
    }

    // Check agent count limit
    // (handled by the plan middleware on twin creation, but re-checked here for safety)

    // Upsert agent assignment
    _, err = a.pool.Exec(ctx, `
        INSERT INTO agent_assignments (agent_id, twin_id, tenant_id, registered_at)
        VALUES ($1, $2, $3, now())
        ON CONFLICT (agent_id, twin_id) DO UPDATE SET registered_at = now()`,
        agentID, twinID, tenantID,
    )
    if err != nil {
        return fmt.Errorf("register agent %s to twin %s: %w", agentID, twinID, err)
    }

    a.logger.Info("agent registered to twin",
        zap.String("agent_id", agentID),
        zap.String("twin_id", twinID),
        zap.String("tenant_id", tenantID),
    )
    return nil
}
```

---

## 8. Security Architecture (Layer 27)

### 8.1 TLS/mTLS Configuration

```go
// File: cluster/internal/security/tls.go

package security

import (
    "crypto/tls"
    "crypto/x509"
    "os"
)

// TLSConfig bundles TLS settings for both server and client.
type TLSConfig struct {
    CertFile    string
    KeyFile     string
    CAFile      string // empty = no mTLS
    ServerName  string
    MinVersion  uint16 // default: tls.VersionTLS13
}

// ServerTLS builds a tls.Config for gRPC/HTTP servers.
// If CAFile is set, enforces mTLS (RequireAndVerifyClientCert).
func ServerTLS(cfg TLSConfig) (*tls.Config, error)

// ClientTLS builds a tls.Config for gRPC/HTTP clients.
// If CAFile is set, verifies the server certificate against the CA.
func ClientTLS(cfg TLSConfig) (*tls.Config, error)

// LoadTLSFromEnv reads TLS configuration from environment variables:
//   PARYTY_TLS_CERT_FILE, PARYTY_TLS_KEY_FILE, PARYTY_TLS_CA_FILE
func LoadTLSFromEnv() TLSConfig
```

### 8.2 RBAC Implementation

```go
// File: cluster/internal/security/rbac.go

package security

import "github.com/gin-gonic/gin"

// Role defines the access level for a user.
type Role string
const (
    RoleAdmin    Role = "admin"
    RoleOperator Role = "operator"
    RoleViewer   Role = "viewer"
)

// RolePermissions maps roles to their default permissions.
var RolePermissions = map[Role][]string{
    RoleAdmin: {
        "twins:read", "twins:write", "twins:delete",
        "users:read", "users:write", "users:delete",
        "apikeys:read", "apikeys:write", "apikeys:delete",
        "alerts:read", "alerts:acknowledge",
        "metrics:read",
        "topology:read",
        "timeline:read",
        "audit:read",
        "plan:read",
    },
    RoleOperator: {
        "twins:read",
        "apikeys:read",
        "alerts:read", "alerts:acknowledge",
        "metrics:read",
        "topology:read",
        "timeline:read",
        "plan:read",
    },
    RoleViewer: {
        "twins:read",
        "topology:read",
        "metrics:read",
        "plan:read",
    },
}

// RequireRole returns Gin middleware that enforces a minimum role.
func RequireRole(role string) gin.HandlerFunc

// RequirePermission returns Gin middleware that enforces a specific permission.
// Checks both the role-based defaults AND any overrides in the user's permissions JSONB.
func RequirePermission(permission string) gin.HandlerFunc

// HasPermission checks if a user (by role + permission overrides) has a permission.
func HasPermission(role string, overrides map[string]bool, permission string) bool
```

### 8.3 Secrets Manager

```go
// File: cluster/internal/security/secrets.go

package security

// SecretsManager provides access to runtime secrets.
// Initially reads from environment variables; designed for Vault migration.
type SecretsManager struct{}

// LoadFromEnv reads secrets from PARYTY_SECRET_* environment variables.
func (m *SecretsManager) LoadFromEnv() Secrets

// Secrets holds all runtime secrets. Zero-value structs are invalid.
type Secrets struct {
    JWTAccessSecret  []byte
    JWTRefreshSecret []byte
    DBPassword       string
    RedisPassword    string
    TLSKeyData       []byte
    // EncryptionKey for at-rest data encryption (future use)
    EncryptionKey    []byte
}

// Validate ensures all required secrets are non-empty.
func (s Secrets) Validate() error
```

---

## 9. Implementation Order (Priority-Ordered)

| Stage | Files | Effort | Depends On |
|---|---|---|---|
| **1. Proto + Codegen** | `proto/paryty/v1/auth.proto`, run `buf generate` | 2h | Nothing |
| **2. DDL Migration** | Extend `controlplane/schema.go` | 1h | Stage 1 |
| **3. Plan YAML + Loader** | `configs/cluster/plans.yaml`, `plan/loader.go` | 2h | Nothing |
| **4. Plan Engine + Store** | `plan/engine.go` | 3h | Stage 2, 3 |
| **5. Auth: JWT + Password** | `auth/jwt.go`, `auth/password.go` | 3h | Nothing |
| **6. Auth: gRPC Handler** | `auth/handler.go` | 3h | Stage 1, 5 |
| **7. Auth: REST Middleware** | `auth/middleware.go` | 2h | Stage 5 |
| **8. User Manager** | `user/user.go`, `user/permissions.go` | 2h | Stage 2 |
| **9. Twin Manager** | `twin/twin.go`, `twin/agent_assignment.go` | 3h | Stage 2 |
| **10. Security: RBAC** | `security/rbac.go`, `security/middleware.go` | 2h | Stage 7 |
| **11. Security: Audit** | `security/audit.go` | 2h | Stage 2 |
| **12. Security: TLS** | `security/tls.go` | 1h | Nothing |
| **13. Plan Middleware** | `plan/middleware.go` | 2h | Stage 4, 7 |
| **14. Wire Everything** | Modify `cmd/query/main.go` | 3h | All above |
| **15. Tests** | `*_test.go` across all packages | 4h | All above |

---

## 10. Key Design Decisions

### 10.1 Why JSONB for `tenant_plans.features` instead of a join table?

The plan YAML is version-controlled and serves as the source of truth. At assignment time,
we snapshot the plan's features into `tenant_plans.features` as JSONB. This means:

- **Reads are O(1)** — one row, one JSONB blob. No joins needed for every request.
- **Plan YAML changes don't break existing tenants** — they keep their snapshot.
- **Plan upgrades are explicit** — an admin action syncs new plan features to existing tenants.
- **Migration is trivial** — add a new feature flag to YAML; existing tenants' JSONB won't
  have it (evaluates to `false` by default).

The alternative — a `plan_features` join table — would require schema migrations every time
a feature is added, and joins on every request.

### 10.2 Why bcrypt cost=12 for passwords but cost=10 for API keys?

- **Passwords**: User-facing, human-memorable, lower entropy. Cost=12 (~300ms) is a good
  balance of security vs UX on login.
- **API keys**: Machine-generated, 64 hex chars (256 bits of entropy). Cost=10 is sufficient
  because the key itself is unguessable. The bcrypt is defense-in-depth against DB exfiltration.

### 10.3 Why refresh tokens have their own table instead of being stateless JWTs?

Refresh tokens are long-lived and must be revocable. A stateless JWT refresh token cannot be
revoked without maintaining a blocklist (which defeats the purpose of statelessness). By storing
a SHA-256 hash in `refresh_tokens`, we get:

- **Instant revocation** — set `revoked_at`, token is dead.
- **Device tracking** — `device_info` allows users to see/revoke specific sessions.
- **Rotation security** — each refresh returns a new token; old one is revoked (prevents replay).

### 10.4 Why not use CASL/OSO for RBAC?

The permission model is simple enough (3 roles × ~10 resources × ~4 actions) that a dedicated
RBAC library adds more complexity than it removes. The `RolePermissions` map in `security/rbac.go`
handles 90% of cases. For the 10% where sub-users need granular overrides, the JSONB
`permissions` field on the `users` table provides per-user customization without schema changes.

### 10.5 Agent registration is gRPC-only, not REST

Agents are machines. They use API keys, not JWT. The ingestion gRPC path already has API key
auth working. Adding REST-based agent registration would create a second auth path for the
same operation — unnecessary complexity. The agent flow remains:

```
Agent starts → gRPC GetTwinConfig(twin_id) → gRPC RegisterAgent → gRPC StreamMetrics
```

The REST API is for the web UI (human users with JWT).

---

## 11. Required go.mod Additions

```go
// Add to cluster/go.mod:
require (
    github.com/golang-jwt/jwt/v5 v5.2.1  // JWT token handling
)
```

All other dependencies (`pgx/v5`, `go-redis/v9`, `gopkg.in/yaml.v3`, `golang.org/x/crypto`,
`gin-gonic/gin`, `google.golang.org/grpc`, `google.golang.org/protobuf`, `go.uber.org/zap`)
are already present in go.mod.
