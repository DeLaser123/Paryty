# Phase 8 Hardened Specification — Production Hardening

**Version:** 1.0.0
**Status:** LOCKED — All architectural decisions finalized
**Target LOC:** ~7,000
**Estimated Effort:** 4-5 weeks for a senior infrastructure engineer

---

## Table of Contents

1. [Phase 8 Overview & Decisions](#1-phase-8-overview--decisions)
2. [Pre-Phase Setup](#2-pre-phase-setup)
3. [Layer 27: Security & Multi-Tenancy](#3-layer-27-security--multi-tenancy)
4. [Layer 28: Observability (Dogfooding)](#4-layer-28-observability-dogfooding)
5. [Layer 29: Deployment & CI/CD](#5-layer-29-deployment--cicd)
6. [Layer 30: Testing & Validation](#6-layer-30-testing--validation)
7. [Verification Gates](#7-verification-gates)
8. [Performance Targets](#8-performance-targets)
9. [Contingency & Rollback](#9-contingency--rollback)
10. [Appendices](#10-appendices)

---

## 1. Phase 8 Overview & Decisions

### 1.1 What Phase 8 Delivers

Phase 8 makes Paryty enterprise-ready. mTLS secures all communication. RBAC controls access. Paryty monitors itself (dogfooding). Helm charts enable one-command Kubernetes deployment. Integration and load tests validate the entire system.

**Before Phase 8:** Working system but no security hardening, no self-monitoring, no production deployment tooling, no end-to-end validation.

**After Phase 8:** Enterprise-grade, production-ready Paryty v1.0. Secure, observable, deployable, validated.

### 1.2 Architectural Decisions (LOCKED)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Certificate Management | **A — cert-manager + non-K8s fallback** | K8s-native with self-signed fallback for VMs/bare metal. |
| 2 | RBAC Model | **A — Simple Role Hierarchy** | Admin/Operator/Viewer. Covers 90% of use cases. |
| 3 | Self-Monitoring | **A — Paryty SDK + custom fallback** | Full dogfooding. No competitor tools. Custom lightweight fallback for critical infra. |
| 4 | Helm Chart Scope | **B — Umbrella Chart with Subcharts** | Parent chart with independent subcharts per component. Flexible scaling. |

### 1.3 What Gets Built

| Layer | Component | LOC | Description |
|-------|-----------|-----|-------------|
| 27 | TLS/mTLS | ~600 | cert-manager integration + self-signed fallback |
| 27 | RBAC | ~800 | Role hierarchy, API key management, tenant isolation |
| 27 | Audit Logging | ~400 | All admin actions + data access logged |
| 27 | Secrets Management | ~300 | K8s Secrets + Vault integration |
| 28 | Self-Monitoring | ~1,200 | Paryty SDK instrumentation + custom health fallback |
| 28 | Performance Dashboards | ~600 | SLO tracking, capacity monitoring |
| 28 | Chaos Testing | ~400 | Failure injection, reconnection, data loss scenarios |
| 29 | Helm Charts | ~800 | Umbrella chart + 6 subcharts + values files |
| 29 | CI/CD Enhancement | ~400 | Container scanning, SLSA provenance, release automation |
| 30 | Integration Tests | ~800 | Agent→Cluster→Storage e2e |
| 30 | Load Tests | ~500 | 10K agent simulation, query perf under load |
| 30 | Scenario Tests | ~200 | Synthetic workloads, failure scenarios |

### 1.4 Design Principles

1. **No competitor dependencies** — Paryty monitors itself. No Prometheus, no Grafana, no Datadog. We eat our own dogfood.
2. **Defense in depth** — mTLS + RBAC + audit logging + network policies. Every layer is independently secure.
3. **Graceful degradation** — If cert-manager is unavailable, fall back to self-signed. If Paryty monitoring is down, custom fallback catches critical failures.
4. **Zero-trust networking** — Every service-to-service call is authenticated and encrypted.

---

## 2. Pre-Phase Setup

### 2.1 New File Structure

```
deploy/
├── helm/
│   ├── paryty/                          # Umbrella chart
│   │   ├── Chart.yaml
│   │   ├── values.yaml                  # Global values
│   │   ├── values-dev.yaml              # Dev overrides
│   │   ├── values-staging.yaml          # Staging overrides
│   │   ├── values-prod.yaml             # Production overrides
│   │   └── templates/
│   │       ├── _helpers.tpl             # Shared template helpers
│   │       ├── namespace.yaml
│   │       └── NOTES.txt                # Post-install notes
│   ├── paryty-agent/                    # Subchart: DaemonSet agent
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/
│   ├── paryty-cluster/                  # Subchart: Cluster services
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/
│   ├── paryty-frontend/                 # Subchart: Frontend
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/
│   ├── paryty-security/                 # Subchart: RBAC, certs, policies
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/
│   ├── paryty-monitoring/               # Subchart: Self-monitoring
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/
│   └── paryty-storage/                  # Subchart: Storage dependencies
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/

cluster/
├── internal/
│   ├── security/
│   │   ├── tls.go                       # TLS config, cert loading
│   │   ├── rbac.go                      # Role hierarchy, permission checks
│   │   ├── apikeys.go                   # API key CRUD + rotation
│   │   ├── audit.go                     # Audit log writer
│   │   ├── tenant.go                    # Tenant isolation
│   │   └── middleware.go                # Security middleware chain
│   └── monitoring/
│       ├── self_monitor.go              # Paryty self-instrumentation
│       ├── health_fallback.go           # Custom lightweight health checks
│       ├── slo_tracker.go               # SLO tracking
│       └── dashboard.go                 # Dashboard data provider

scripts/
├── security/
│   ├── generate-certs.sh               # Self-signed cert generation
│   ├── rotate-certs.sh                  # Certificate rotation
│   └── setup-vault.sh                   # Vault PKI setup
├── testing/
│   ├── integration_test.go             # Integration test runner
│   ├── load_test.go                     # Load test runner
│   └── chaos_test.sh                    # Chaos test scenarios

tests/
├── integration/
│   ├── pipeline_test.go                 # Agent→Cluster→Storage
│   ├── query_test.go                    # Query layer integration
│   └── intelligence_test.go             # ML model validation
├── load/
│   ├── agent_load_test.go               # 10K agent simulation
│   ├── query_load_test.go               # Query under load
│   └── storage_load_test.go             # Storage throughput
└── chaos/
    ├── agent_reconnect_test.go          # Agent reconnection
    ├── redpanda_failure_test.go         # Data loss scenarios
    └── storage_failover_test.go         # Storage failover
```

---

## 3. Layer 27: Security & Multi-Tenancy

### 3.1 TLS/mTLS

**File:** `cluster/internal/security/tls.go` (~300 LOC)

```go
// TLS configuration for all Paryty services.
//
// SUPPORTS TWO MODES:
//
// 1. cert-manager (Kubernetes): Automatic certificate issuance and rotation.
//    - Services request certificates via Kubernetes annotations
//    - cert-manager handles issuance, rotation, and revocation
//    - Works with Let's Encrypt, HashiCorp Vault, or self-signed issuers
//
// 2. Self-signed fallback (non-Kubernetes): Manual certificate management.
//    - Scripts generate CA and service certificates
//    - Certificates stored on filesystem
//    - Rotation via cron job or systemd timer

package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"
)

// TLSMode defines how TLS certificates are managed.
type TLSMode string

const (
	// TLSModeCertManager uses Kubernetes cert-manager for certificates.
	TLSModeCertManager TLSMode = "cert-manager"
	// TLSModeSelfSigned uses self-signed certificates (fallback for non-K8s).
	TLSModeSelfSigned TLSMode = "self-signed"
	// TLSModeDisabled disables TLS (development only).
	TLSModeDisabled TLSMode = "disabled"
)

// TLSConfig configures TLS for a service.
type TLSConfig struct {
	// Mode determines how certificates are managed.
	Mode TLSMode `yaml:"mode" json:"mode"`
	// CertDir is the directory containing TLS certificates.
	CertDir string `yaml:"cert_dir" json:"cert_dir"`
	// CertFile is the path to the TLS certificate.
	CertFile string `yaml:"cert_file" json:"cert_file"`
	// KeyFile is the path to the TLS private key.
	KeyFile string `yaml:"key_file" json:"key_file"`
	// CAFile is the path to the CA certificate for mTLS verification.
	CAFile string `yaml:"ca_file" json:"ca_file"`
	// RequireClientCert enables mTLS (server verifies client certificate).
	RequireClientCert bool `yaml:"require_client_cert" json:"require_client_cert"`
	// MinVersion is the minimum TLS version (default: 1.3).
	MinVersion uint16 `yaml:"min_version" json:"min_version"`
	// CertRotationInterval is how often to check for cert rotation (default: 1h).
	CertRotationInterval time.Duration `yaml:"cert_rotation_interval" json:"cert_rotation_interval"`
}

// DefaultTLSConfig returns sensible TLS defaults.
func DefaultTLSConfig() TLSConfig {
	return TLSConfig{
		Mode:                 TLSModeCertManager,
		CertDir:              "/etc/paryty/certs",
		MinVersion:           tls.VersionTLS13,
		CertRotationInterval: 1 * time.Hour,
	}
}

// TLSManager handles TLS certificate loading and rotation.
type TLSManager struct {
	mu          sync.RWMutex
	config      TLSConfig
	cert        *tls.Certificate
	certPool    *x509.CertPool
	stopCh      chan struct{}
	lastReload  time.Time
}

// NewTLSManager creates a new TLS manager.
func NewTLSManager(cfg TLSConfig) (*TLSManager, error) {
	m := &TLSManager{
		config: cfg,
		stopCh: make(chan struct{}),
	}

	if cfg.Mode == TLSModeDisabled {
		return m, nil
	}

	if err := m.loadCertificates(); err != nil {
		return nil, fmt.Errorf("failed to load TLS certificates: %w", err)
	}

	return m, nil
}

// loadCertificates loads TLS certificates from disk.
func (m *TLSManager) loadCertificates() error {
	cert, err := tls.LoadX509KeyPair(m.config.CertFile, m.config.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to load cert/key pair: %w", err)
	}

	m.mu.Lock()
	m.cert = &cert
	m.lastReload = time.Now()
	m.mu.Unlock()

	// Load CA certificate for mTLS
	if m.config.CAFile != "" {
		caCert, err := os.ReadFile(m.config.CAFile)
		if err != nil {
			return fmt.Errorf("failed to read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caCert)

		m.mu.Lock()
		m.certPool = pool
		m.mu.Unlock()
	}

	return nil
}

// TLSConfig returns a tls.Config for server use.
func (m *TLSManager) TLSConfig() *tls.Config {
	if m.config.Mode == TLSModeDisabled {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
			m.mu.RLock()
			defer m.mu.RUnlock()
			return m.cert, nil
		},
	}

	if m.config.RequireClientCert && m.certPool != nil {
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		cfg.ClientCAs = m.certPool
	}

	return cfg
}

// StartRotationWatcher watches for certificate rotation.
func (m *TLSManager) StartRotationWatcher() {
	go func() {
		ticker := time.NewTicker(m.config.CertRotationInterval)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.checkAndReload()
			}
		}
	}()
}

func (m *TLSManager) checkAndReload() {
	// Check if cert files have been modified since last reload
	// Reload if changed (cert-manager updates the secret mount)
	info, err := os.Stat(m.config.CertFile)
	if err != nil {
		return
	}
	if info.ModTime().After(m.lastReload) {
		m.loadCertificates()
	}
}

// Stop stops the certificate rotation watcher.
func (m *TLSManager) Stop() {
	close(m.stopCh)
}
```

**File:** `cluster/internal/security/middleware.go` (~200 LOC)

```go
// Security middleware chain for HTTP and gRPC services.
//
// MIDDLEWARE ORDER (outermost to innermost):
//   1. TLS termination (handled by server)
//   2. Audit logging (logs all requests)
//   3. Tenant extraction (extract tenant from API key)
//   4. RBAC enforcement (check role permissions)
//   5. Rate limiting (per-tenant rate limits)
//   6. Handler

package security

import (
	"context"
	"net/http"
	"time"
)

// SecurityMiddleware chains all security middleware together.
func SecurityMiddleware(
	audit *AuditLogger,
	rbac *RBACEnforcer,
	apiKeys *APIKeyStore,
	next http.Handler,
) http.Handler {
	return auditMiddleware(audit,
		tenantMiddleware(apiKeys,
			rbacMiddleware(rbac,
				next,
			),
		),
	)
}

// auditMiddleware logs all requests for compliance.
func auditMiddleware(audit *AuditLogger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Capture response status
		rw := &statusCapturer{ResponseWriter: w, status: 200}

		next.ServeHTTP(rw, r)

		audit.Log(AuditEntry{
			Timestamp:  start,
			Action:     r.Method + " " + r.URL.Path,
			TenantID:   TenantFromContext(r.Context()),
			UserID:     UserFromContext(r.Context()),
			IPAddress:  r.RemoteAddr,
			StatusCode: rw.status,
			Duration:   time.Since(start),
		})
	})
}

// tenantMiddleware extracts tenant ID from API key.
func tenantMiddleware(apiKeys *APIKeyStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			http.Error(w, "missing API key", http.StatusUnauthorized)
			return
		}

		key, err := apiKeys.Validate(apiKey)
		if err != nil {
			http.Error(w, "invalid API key", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), tenantKey{}, key.TenantID)
		ctx = context.WithValue(ctx, userKey{}, key.UserID)
		ctx = context.WithValue(ctx, roleKey{}, key.Role)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rbacMiddleware enforces role-based access control.
func rbacMiddleware(rbac *RBACEnforcer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := RoleFromContext(r.Context())
		resource := r.URL.Path
		action := r.Method

		if !rbac.Check(role, resource, action) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
```

### 3.2 RBAC & API Key Management

**File:** `cluster/internal/security/rbac.go` (~300 LOC)

```go
// Role-Based Access Control for Paryty.
//
// ROLE HIERARCHY:
//   Admin   → Full access (read, write, delete, configure, manage users)
//   Operator → Read + acknowledge alerts + run simulations + manage API keys
//   Viewer  → Read-only access to all resources
//
// RESOURCE TYPES:
//   - metrics (query, export)
//   - traces (query, export)
//   - topology (view, export)
//   - alerts (view, acknowledge, configure)
//   - simulations (view, run, cancel)
//   - settings (view, modify)
//   - users (view, create, delete)
//   - api-keys (view, create, rotate, revoke)

package security

import "strings"

// Role represents a user role.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

// Permission represents an action on a resource.
type Permission string

const (
	PermRead      Permission = "read"
	PermWrite     Permission = "write"
	PermDelete    Permission = "delete"
	PermConfigure Permission = "configure"
	PermManage    Permission = "manage"
)

// RBACEnforcer enforces role-based access control.
type RBACEnforcer struct {
	policies map[Role]map[string][]Permission
}

// NewRBACEnforcer creates a new RBAC enforcer with default policies.
func NewRBACEnforcer() *RBACEnforcer {
	e := &RBACEnforcer{
		policies: make(map[Role]map[string][]Permission),
	}
	e.loadDefaults()
	return e
}

func (e *RBACEnforcer) loadDefaults() {
	// Admin: full access to everything
	e.policies[RoleAdmin] = map[string][]Permission{
		"metrics":     {PermRead, PermWrite, PermDelete, PermConfigure},
		"traces":      {PermRead, PermWrite, PermDelete, PermConfigure},
		"topology":    {PermRead, PermWrite, PermDelete, PermConfigure},
		"alerts":      {PermRead, PermWrite, PermDelete, PermConfigure},
		"simulations": {PermRead, PermWrite, PermDelete, PermConfigure},
		"settings":    {PermRead, PermWrite, PermConfigure},
		"users":       {PermRead, PermWrite, PermDelete, PermManage},
		"api-keys":    {PermRead, PermWrite, PermDelete, PermManage},
	}

	// Operator: read + limited write
	e.policies[RoleOperator] = map[string][]Permission{
		"metrics":     {PermRead},
		"traces":      {PermRead},
		"topology":    {PermRead},
		"alerts":      {PermRead, PermWrite},
		"simulations": {PermRead, PermWrite},
		"settings":    {PermRead},
		"users":       {PermRead},
		"api-keys":    {PermRead, PermWrite},
	}

	// Viewer: read-only
	e.policies[RoleViewer] = map[string][]Permission{
		"metrics":     {PermRead},
		"traces":      {PermRead},
		"topology":    {PermRead},
		"alerts":      {PermRead},
		"simulations": {PermRead},
		"settings":    {PermRead},
		"users":       {},
		"api-keys":    {PermRead},
	}
}

// Check verifies if a role has permission for a resource action.
func (e *RBACEnforcer) Check(role Role, resource string, action string) bool {
	perms, ok := e.policies[role]
	if !ok {
		return false
	}

	// Extract resource type from path (e.g., "/api/v1/metrics/query" → "metrics")
	resourceType := extractResourceType(resource)
	requiredPerm := methodToPermission(action)

	allowed, ok := perms[resourceType]
	if !ok {
		return false
	}

	for _, p := range allowed {
		if p == requiredPerm {
			return true
		}
	}

	return false
}

// extractResourceType extracts the resource type from a URL path.
func extractResourceType(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// methodToPermission maps HTTP methods to permissions.
func methodToPermission(method string) Permission {
	switch method {
	case "GET":
		return PermRead
	case "POST", "PUT", "PATCH":
		return PermWrite
	case "DELETE":
		return PermDelete
	default:
		return PermRead
	}
}
```

**File:** `cluster/internal/security/apikeys.go` (~200 LOC)

```go
// API Key management for Paryty.
//
// API keys are the primary authentication mechanism.
// Each key is associated with a tenant and role.
//
// KEY FORMAT: paryty_<random32chars>
// EXAMPLE:   paryty_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6

package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// APIKey represents an API key.
type APIKey struct {
	ID          string    `json:"id"`
	KeyHash     string    `json:"key_hash"` // SHA-256 hash, never store plaintext
	TenantID    string    `json:"tenant_id"`
	UserID      string    `json:"user_id"`
	Role        Role      `json:"role"`
	Name        string    `json:"name"`         // Human-readable name
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
	Revoked     bool      `json:"revoked"`
	RevokedAt   time.Time `json:"revoked_at,omitempty"`
}

// APIKeyStore manages API keys.
type APIKeyStore struct {
	// Backed by database (PostgreSQL or embedded)
	// Keys stored as SHA-256 hashes
}

// CreateKey creates a new API key.
// Returns the plaintext key (shown only once).
func (s *APIKeyStore) CreateKey(tenantID, userID, name string, role Role, ttl time.Duration) (string, *APIKey, error) {
	// Generate random key
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate key: %w", err)
	}

	plaintext := "paryty_" + hex.EncodeToString(randomBytes)
	hash := sha256.Sum256([]byte(plaintext))

	key := &APIKey{
		ID:        generateKeyID(),
		KeyHash:   hex.EncodeToString(hash[:]),
		TenantID:  tenantID,
		UserID:    userID,
		Role:      role,
		Name:      name,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(ttl),
	}

	// Store in database
	// s.db.InsertAPIKey(key)

	return plaintext, key, nil
}

// Validate checks an API key and returns the associated key info.
func (s *APIKeyStore) Validate(plaintext string) (*APIKey, error) {
	hash := sha256.Sum256([]byte(plaintext))
	hashStr := hex.EncodeToString(hash[:])

	// Look up by hash
	// key, err := s.db.GetAPIKeyByHash(hashStr)
	// if err != nil { return nil, ErrInvalidKey }
	// if key.Revoked { return nil, ErrRevokedKey }
	// if time.Now().After(key.ExpiresAt) { return nil, ErrExpiredKey }

	// Update last used
	// s.db.UpdateLastUsed(key.ID, time.Now())

	return nil, nil // Placeholder
}

// RotateKey creates a new key and revokes the old one.
func (s *APIKeyStore) RotateKey(keyID string) (string, *APIKey, error) {
	// 1. Get existing key
	// 2. Create new key with same tenant/user/role
	// 3. Revoke old key
	// 4. Return new plaintext
	return "", nil, nil // Placeholder
}

// RevokeKey revokes an API key immediately.
func (s *APIKeyStore) RevokeKey(keyID string) error {
	// s.db.RevokeAPIKey(keyID, time.Now())
	return nil
}
```

### 3.3 Audit Logging

**File:** `cluster/internal/security/audit.go` (~200 LOC)

```go
// Audit logging for compliance (SOC2, HIPAA, GDPR).
//
// LOGS:
//   - All API requests (method, path, tenant, user, IP, status, duration)
//   - All admin actions (user creation, key rotation, settings changes)
//   - All data access patterns (who queried what, when)
//   - All authentication events (login, logout, failed attempts)
//
// STORAGE:
//   - Written to append-only audit log file (JSON lines)
//   - Also published to Redpanda topic: paryty.audit
//   - Retained for compliance period (default: 7 years)

package security

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	Timestamp   time.Time         `json:"timestamp"`
	Action      string            `json:"action"`
	Resource    string            `json:"resource,omitempty"`
	TenantID    string            `json:"tenant_id,omitempty"`
	UserID      string            `json:"user_id,omitempty"`
	IPAddress   string            `json:"ip_address,omitempty"`
	StatusCode  int               `json:"status_code,omitempty"`
	Duration    time.Duration      `json:"duration,omitempty"`
	Details     map[string]string `json:"details,omitempty"`
	EventType   AuditEventType    `json:"event_type"`
}

// AuditEventType categorizes audit events.
type AuditEventType string

const (
	AuditEventAPI         AuditEventType = "api_request"
	AuditEventAuth        AuditEventType = "authentication"
	AuditEventAdmin       AuditEventType = "admin_action"
	AuditEvent DataAccess AuditEventType = "data_access"
	AuditEventSecurity    AuditEventType = "security_event"
)

// AuditLogger writes audit logs.
type AuditLogger struct {
	mu     sync.Mutex
	file   *os.File
	enc    *json.Encoder
}

// NewAuditLogger creates a new audit logger.
func NewAuditLogger(path string) (*AuditLogger, error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}

	return &AuditLogger{
		file: file,
		enc:  json.NewEncoder(file),
	}, nil
}

// Log writes an audit entry.
func (l *AuditLogger) Log(entry AuditEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	l.enc.Encode(entry) //nolint:errcheck
}

// Close closes the audit logger.
func (l *AuditLogger) Close() error {
	return l.file.Close()
}
```

### 3.4 Secrets Management

**File:** `cluster/internal/security/secrets.go` (~150 LOC)

```go
// Secrets management for Paryty.
//
// SUPPORTS:
//   1. Kubernetes Secrets (default for K8s deployments)
//   2. HashiCorp Vault (for enterprise deployments)
//   3. Environment variables (for development)
//   4. File-based secrets (for VM/bare metal)
//
// PRIORITY: Vault > K8s Secrets > File > Env

package security

import (
	"fmt"
	"os"
)

// SecretSource defines where secrets come from.
type SecretSource string

const (
	SecretSourceVault SecretSource = "vault"
	SecretSourceK8s   SecretSource = "kubernetes"
	SecretSourceFile  SecretSource = "file"
	SecretSourceEnv   SecretSource = "env"
)

// SecretManager retrieves secrets from various sources.
type SecretManager struct {
	source   SecretSource
	vaultAddr string
	vaultToken string
}

// GetSecret retrieves a secret by key.
func (m *SecretManager) GetSecret(key string) (string, error) {
	switch m.source {
	case SecretSourceVault:
		return m.getFromVault(key)
	case SecretSourceK8s:
		return m.getFromK8s(key)
	case SecretSourceFile:
		return m.getFromFile(key)
	case SecretSourceEnv:
		return m.getFromEnv(key)
	default:
		return "", fmt.Errorf("unknown secret source: %s", m.source)
	}
}

func (m *SecretManager) getFromEnv(key string) (string, error) {
	val := os.Getenv(key)
	if val == "" {
		return "", fmt.Errorf("secret %s not found in environment", key)
	}
	return val, nil
}

func (m *SecretManager) getFromFile(key string) (string, error) {
	path := fmt.Sprintf("/etc/paryty/secrets/%s", key)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("secret %s not found at %s: %w", key, path, err)
	}
	return string(data), nil
}

func (m *SecretManager) getFromK8s(key string) (string, error) {
	// Read from mounted Kubernetes Secret
	path := fmt.Sprintf("/var/run/secrets/paryty/%s", key)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("secret %s not found in K8s: %w", key, err)
	}
	return string(data), nil
}

func (m *SecretManager) getFromVault(key string) (string, error) {
	// Read from Vault KV v2 secrets engine
	// vault kv get -field=<key> secret/data/paryty
	return "", fmt.Errorf("vault integration not yet implemented")
}
```

---

## 4. Layer 28: Observability (Dogfooding)

### 4.1 Design Philosophy

**Paryty monitors itself.** No Prometheus. No Grafana. No Datadog. No competitor tools.

When a customer asks "how do you monitor your own system?", the answer is: "We use Paryty. If it's good enough for you, it's good enough for us."

**Fallback:** A custom lightweight health checker runs independently of Paryty's main pipeline. It checks critical infrastructure (Redpanda, QuestDB, Dragonfly) every 10 seconds and alerts directly (webhook/email) if something is critically broken. This is NOT Prometheus — it's a simple Go binary that does TCP/HTTP checks.

### 4.2 Self-Monitoring Implementation

**File:** `cluster/internal/monitoring/self_monitor.go` (~400 LOC)

```go
// Self-monitoring for Paryty services.
//
// Instruments all Paryty Go services with the Paryty Go SDK.
// Each service reports its own metrics, traces, and health.
//
// MONITORED METRICS:
//   - Ingestion: rate, latency, error rate, queue depth
//   - Processing: aggregation latency, correlation lag, enrichment time
//   - Storage: write latency, read latency, disk usage, replication lag
//   - Query: query latency (p50/p95/p99), result size, cache hit rate
//   - Streaming: consumer lag, producer throughput, partition count
//   - Intelligence: forecast latency, anomaly detection time, model accuracy
//
// SLOs:
//   - Ingestion latency: < 100ms (p99)
//   - Query latency: < 500ms (p95)
//   - Storage write: < 50ms (p99)
//   - Availability: 99.9%

package monitoring

import (
	"context"
	"time"

	"github.com/paryty/paryty-v1.0/agent/go_sdk/paryty"
)

// ServiceType identifies the Paryty service being monitored.
type ServiceType string

const (
	ServiceIngestion   ServiceType = "ingestion"
	ServiceAggregator  ServiceType = "aggregator"
	ServiceCorrelator  ServiceType = "correlator"
	ServiceEnricher    ServiceType = "enricher"
	ServiceQuery       ServiceType = "query"
	ServiceIntelligence ServiceType = "intelligence"
)

// SelfMonitor instruments a Paryty service with its own SDK.
type SelfMonitor struct {
	client  *paryty.Client
	service ServiceType

	// Standard metrics (every service reports these)
	requestCounter   *paryty.Counter
	requestDuration  *paryty.Histogram
	errorCounter     *paryty.Counter
	activeGoroutines *paryty.Gauge
	memoryUsage      *paryty.Gauge

	// Service-specific metrics
	serviceMetrics map[string]interface{}
}

// NewSelfMonitor creates a self-monitoring instance for a service.
//
// USAGE:
//
//	mon := monitoring.NewSelfMonitor(monitoring.ServiceIngestion, "localhost:9090")
//	defer mon.Close()
//
//	// Record request
//	mon.RecordRequest(duration, err == nil)
//
//	// Service-specific metric
//	mon.RecordGauge("ingestion.queue_depth", float64(queue.Len()))
func NewSelfMonitor(service ServiceType, agentAddr string) *SelfMonitor {
	client, err := paryty.NewClientWithConfig(paryty.Config{
		AgentAddr:    agentAddr,
		ServiceName:  "paryty-" + string(service),
		MetricFlushInterval: 5 * time.Second,
	})
	if err != nil {
		// Log error but don't crash — monitoring is optional
		return nil
	}

	registry := client.Registry()

	return &SelfMonitor{
		client:  client,
		service: service,
		requestCounter: registry.RegisterCounter(
			"paryty_service_requests_total",
			paryty.WithDescription("Total requests handled by this service"),
		),
		requestDuration: registry.RegisterHistogram(
			"paryty_service_request_duration_seconds",
			paryty.WithDescription("Request handling duration"),
			paryty.WithBuckets([]float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0}),
		),
		errorCounter: registry.RegisterCounter(
			"paryty_service_errors_total",
			paryty.WithDescription("Total errors from this service"),
		),
		activeGoroutines: registry.RegisterGauge(
			"paryty_service_goroutines",
			paryty.WithDescription("Number of active goroutines"),
		),
		memoryUsage: registry.RegisterGauge(
			"paryty_service_memory_bytes",
			paryty.WithDescription("Memory usage in bytes"),
		),
		serviceMetrics: make(map[string]interface{}),
	}
}

// RecordRequest records a request with duration and success status.
func (m *SelfMonitor) RecordRequest(duration time.Duration, success bool) {
	if m == nil {
		return
	}
	m.requestCounter.Inc()
	m.requestDuration.Observe(duration.Seconds())
	if !success {
		m.errorCounter.Inc()
	}
}

// RecordGauge records a service-specific gauge metric.
func (m *SelfMonitor) RecordGauge(name string, value float64) {
	if m == nil {
		return
	}
	gauge, ok := m.serviceMetrics[name].(*paryty.Gauge)
	if !ok {
		gauge = m.client.Registry().RegisterGauge(name)
		m.serviceMetrics[name] = gauge
	}
	gauge.Set(value)
}

// RecordCounter records a service-specific counter metric.
func (m *SelfMonitor) RecordCounter(name string, delta uint64) {
	if m == nil {
		return
	}
	counter, ok := m.serviceMetrics[name].(*paryty.Counter)
	if !ok {
		counter = m.client.Registry().RegisterCounter(name)
		m.serviceMetrics[name] = counter
	}
	counter.Add(delta)
}

// StartSpan starts a trace span for this service.
func (m *SelfMonitor) StartSpan(ctx context.Context, name string) (context.Context, *paryty.Span) {
	if m == nil {
		return ctx, nil
	}
	return m.client.StartSpan(ctx, name)
}

// Close closes the self-monitoring client.
func (m *SelfMonitor) Close() {
	if m != nil && m.client != nil {
		m.client.Close()
	}
}
```

### 4.3 Custom Health Fallback

**File:** `cluster/internal/monitoring/health_fallback.go` (~300 LOC)

```go
// Custom lightweight health checker — independent of Paryty pipeline.
//
// PURPOSE: If Paryty's main monitoring pipeline is broken, this fallback
// catches critical infrastructure failures and alerts directly.
//
// CHECKS:
//   - Redpanda: TCP connectivity + topic list
//   - QuestDB: HTTP health endpoint
//   - Dragonfly: PING command
//   - SeaweedFS: HTTP master status
//   - Intelligence service: gRPC health check
//
// ALERTING:
//   - Webhook (Slack, Discord, custom)
//   - Email (SMTP)
//   - PagerDuty integration
//   - Log file (always)
//
// This is NOT Prometheus. It's a simple Go binary that does
// TCP/HTTP checks and sends alerts. No scraping, no TSDB,
// no query language. Just health checks + alerts.

package monitoring

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// HealthTarget represents a service to check.
type HealthTarget struct {
	Name     string        `json:"name"`
	Type     CheckType     `json:"type"`
	Address  string        `json:"address"`
	Interval time.Duration `json:"interval"`
	Timeout  time.Duration `json:"timeout"`
}

// CheckType defines the type of health check.
type CheckType string

const (
	CheckTypeTCP  CheckType = "tcp"
	CheckTypeHTTP CheckType = "http"
	CheckTypePing CheckType = "ping"
)

// HealthStatus represents the health of a target.
type HealthStatus struct {
	Target    string        `json:"target"`
	Healthy   bool          `json:"healthy"`
	Message   string        `json:"message,omitempty"`
	Latency   time.Duration `json:"latency"`
	CheckedAt time.Time     `json:"checked_at"`
}

// AlertChannel sends alerts when services go down.
type AlertChannel interface {
	SendAlert(ctx context.Context, status HealthStatus) error
}

// HealthFallback is the independent health checker.
type HealthFallback struct {
	targets   []HealthTarget
	alerts    []AlertChannel
	statuses  map[string]*HealthStatus
	mu        sync.RWMutex
	stopCh    chan struct{}
}

// NewHealthFallback creates a new health fallback checker.
func NewHealthFallback(targets []HealthTarget, alerts []AlertChannel) *HealthFallback {
	return &HealthFallback{
		targets:  targets,
		alerts:   alerts,
		statuses: make(map[string]*HealthStatus),
		stopCh:   make(chan struct{}),
	}
}

// Start begins periodic health checking.
func (h *HealthFallback) Start(ctx context.Context) {
	for _, target := range h.targets {
		go h.checkLoop(ctx, target)
	}
}

func (h *HealthFallback) checkLoop(ctx context.Context, target HealthTarget) {
	ticker := time.NewTicker(target.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopCh:
			return
		case <-ticker.C:
			status := h.check(ctx, target)

			h.mu.Lock()
			previous := h.statuses[target.Name]
			h.statuses[target.Name] = status
			h.mu.Unlock()

			// Alert on state change (healthy → unhealthy)
			if previous != nil && previous.Healthy && !status.Healthy {
				for _, alert := range h.alerts {
					alert.SendAlert(ctx, *status)
				}
			}

			// Alert on recovery (unhealthy → healthy)
			if previous != nil && !previous.Healthy && status.Healthy {
				for _, alert := range h.alerts {
					alert.SendAlert(ctx, *status)
				}
			}
		}
	}
}

func (h *HealthFallback) check(ctx context.Context, target HealthTarget) *HealthStatus {
	start := time.Now()
	status := &HealthStatus{
		Target:    target.Name,
		CheckedAt: start,
	}

	var err error
	switch target.Type {
	case CheckTypeTCP:
		err = h.checkTCP(ctx, target)
	case CheckTypeHTTP:
		err = h.checkHTTP(ctx, target)
	case CheckTypePing:
		err = h.checkPing(ctx, target)
	}

	status.Latency = time.Since(start)
	if err != nil {
		status.Healthy = false
		status.Message = err.Error()
	} else {
		status.Healthy = true
	}

	return status
}

func (h *HealthFallback) checkTCP(ctx context.Context, target HealthTarget) error {
	dialer := &net.Dialer{Timeout: target.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", target.Address)
	if err != nil {
		return fmt.Errorf("TCP connection failed: %w", err)
	}
	conn.Close()
	return nil
}

func (h *HealthFallback) checkHTTP(ctx context.Context, target HealthTarget) error {
	client := &http.Client{Timeout: target.Timeout}
	req, err := http.NewRequestWithContext(ctx, "GET", target.Address, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP check failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (h *HealthFallback) checkPing(ctx context.Context, target HealthTarget) error {
	// Redis PING for Dragonfly
	conn, err := net.DialTimeout("tcp", target.Address, target.Timeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// GetStatuses returns current health statuses.
func (h *HealthFallback) GetStatuses() map[string]*HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make(map[string]*HealthStatus)
	for k, v := range h.statuses {
		result[k] = v
	}
	return result
}

// Stop stops the health checker.
func (h *HealthFallback) Stop() {
	close(h.stopCh)
}
```

### 4.4 Webhook Alert Channel

**File:** `cluster/internal/monitoring/alert_webhook.go` (~100 LOC)

```go
// Webhook alert channel for health fallback.
//
// Sends JSON payload to configured webhook URL (Slack, Discord, custom).
//
// PAYLOAD:
//   {
//     "text": "ALERT: redpanda is DOWN (TCP connection refused)",
//     "target": "redpanda",
//     "healthy": false,
//     "latency": "5s",
//     "checked_at": "2026-06-01T12:00:00Z"
//   }

package monitoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookAlert sends alerts to a webhook URL.
type WebhookAlert struct {
	URL    string
	client *http.Client
}

// NewWebhookAlert creates a new webhook alert channel.
func NewWebhookAlert(url string) *WebhookAlert {
	return &WebhookAlert{
		URL: url,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// SendAlert sends a health status alert to the webhook.
func (w *WebhookAlert) SendAlert(ctx context.Context, status HealthStatus) error {
	state := "UP"
	if !status.Healthy {
		state = "DOWN"
	}

	payload := map[string]interface{}{
		"text":       fmt.Sprintf("ALERT: %s is %s (%s)", status.Target, state, status.Message),
		"target":     status.Target,
		"healthy":    status.Healthy,
		"latency":    status.Latency.String(),
		"checked_at": status.CheckedAt.Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	return nil
}
```

### 4.5 SLO Tracker

**File:** `cluster/internal/monitoring/slo_tracker.go` (~200 LOC)

```go
// SLO (Service Level Objective) tracking for Paryty.
//
// SLOs:
//   - Ingestion latency: < 100ms (p99)
//   - Query latency: < 500ms (p95)
//   - Storage write: < 50ms (p99)
//   - Availability: 99.9%
//   - Agent reconnection: < 5s
//
// Tracks error budgets. If error budget is exhausted,
// alerts the team and blocks deployments.

package monitoring

import (
	"sync"
	"time"
)

// SLO defines a service level objective.
type SLO struct {
	Name        string        `json:"name"`
	Target      float64       `json:"target"`       // e.g., 0.999 for 99.9%
	Window      time.Duration `json:"window"`       // e.g., 30 days
	Description string        `json:"description"`
}

// DefaultSLOs returns the default Paryty SLOs.
func DefaultSLOs() []SLO {
	return []SLO{
		{Name: "ingestion_latency_p99", Target: 0.999, Window: 30 * 24 * time.Hour,
			Description: "99.9% of ingestion requests complete in < 100ms"},
		{Name: "query_latency_p95", Target: 0.995, Window: 30 * 24 * time.Hour,
			Description: "99.5% of queries complete in < 500ms"},
		{Name: "storage_write_p99", Target: 0.999, Window: 30 * 24 * time.Hour,
			Description: "99.9% of storage writes complete in < 50ms"},
		{Name: "availability", Target: 0.999, Window: 30 * 24 * time.Hour,
			Description: "System is available 99.9% of the time"},
		{Name: "agent_reconnect", Target: 0.99, Window: 30 * 24 * time.Hour,
			Description: "99% of agents reconnect within 5s"},
	}
}

// SLOTracker tracks SLO compliance and error budgets.
type SLOTracker struct {
	mu      sync.RWMutex
	slos    []SLO
	windows map[string]*SLOWindow
}

// SLOWindow tracks a rolling window of SLO measurements.
type SLOWindow struct {
	Total     int64   `json:"total"`
	Violated  int64   `json:"violated"`
	Budget    float64 `json:"budget"`     // Remaining error budget (0.0 - 1.0)
	Compliant bool    `json:"compliant"`
}

// NewSLOTracker creates a new SLO tracker.
func NewSLOTracker(slos []SLO) *SLOTracker {
	t := &SLOTracker{
		slos:    slos,
		windows: make(map[string]*SLOWindow),
	}
	for _, slo := range slos {
		t.windows[slo.Name] = &SLOWindow{
			Budget:    1.0,
			Compliant: true,
		}
	}
	return t
}

// Record records a measurement for an SLO.
func (t *SLOTracker) Record(sloName string, withinTarget bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	window, ok := t.windows[sloName]
	if !ok {
		return
	}

	window.Total++
	if !withinTarget {
		window.Violated++
	}

	// Calculate remaining error budget
	if window.Total > 0 {
		errorRate := float64(window.Violated) / float64(window.Total)
		slo := t.getSLO(sloName)
		if slo != nil {
			allowedError := 1.0 - slo.Target
			if allowedError > 0 {
				window.Budget = 1.0 - (errorRate / allowedError)
			}
		}
		window.Compliant = window.Budget > 0
	}
}

func (t *SLOTracker) getSLO(name string) *SLO {
	for _, slo := range t.slos {
		if slo.Name == name {
			return &slo
		}
	}
	return nil
}

// GetStatus returns the current SLO status.
func (t *SLOTracker) GetStatus() map[string]*SLOWindow {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make(map[string]*SLOWindow)
	for k, v := range t.windows {
		result[k] = v
	}
	return result
}
```

---

## 5. Layer 29: Deployment & CI/CD

### 5.1 Helm Umbrella Chart

**File:** `deploy/helm/paryty/Chart.yaml`

```yaml
apiVersion: v2
name: paryty
description: Paryty — Distributed Observability Operating System
type: application
version: 1.0.0
appVersion: "1.0.0"

dependencies:
  - name: paryty-agent
    version: "1.0.0"
    repository: "file://../paryty-agent"
    condition: paryty-agent.enabled
  - name: paryty-cluster
    version: "1.0.0"
    repository: "file://../paryty-cluster"
    condition: paryty-cluster.enabled
  - name: paryty-frontend
    version: "1.0.0"
    repository: "file://../paryty-frontend"
    condition: paryty-frontend.enabled
  - name: paryty-security
    version: "1.0.0"
    repository: "file://../paryty-security"
    condition: paryty-security.enabled
  - name: paryty-monitoring
    version: "1.0.0"
    repository: "file://../paryty-monitoring"
    condition: paryty-monitoring.enabled
  - name: paryty-storage
    version: "1.0.0"
    repository: "file://../paryty-storage"
    condition: paryty-storage.enabled
```

**File:** `deploy/helm/paryty/values.yaml` (global defaults)

```yaml
# Global configuration
global:
  namespace: paryty
  environment: production
  imageRegistry: ghcr.io/paryty
  imageTag: "1.0.0"
  imagePullPolicy: IfNotPresent

  # TLS configuration
  tls:
    enabled: true
    mode: cert-manager  # cert-manager | self-signed | disabled
    certIssuer: letsencrypt-prod

  # RBAC
  rbac:
    enabled: true
    defaultRole: viewer

  # Monitoring
  monitoring:
    enabled: true
    selfMonitor:
      enabled: true
      agentAddr: "paryty-agent:9090"
    healthFallback:
      enabled: true
      checkInterval: 10s
      webhookUrl: ""

# Subchart defaults
paryty-agent:
  enabled: true
  replicaCount: 1  # DaemonSet — one per node
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi

paryty-cluster:
  enabled: true
  services:
    ingestion:
      replicaCount: 3
      resources:
        requests: { cpu: 200m, memory: 256Mi }
        limits: { cpu: 1, memory: 1Gi }
    aggregator:
      replicaCount: 2
    correlator:
      replicaCount: 2
    enricher:
      replicaCount: 2
    query:
      replicaCount: 3

paryty-frontend:
  enabled: true
  replicaCount: 2
  ingress:
    enabled: true
    host: paryty.example.com
    tls: true

paryty-security:
  enabled: true
  certManager:
    enabled: true
    issuerName: letsencrypt-prod
  audit:
    enabled: true
    retentionDays: 2555  # 7 years

paryty-monitoring:
  enabled: true
  healthFallback:
    targets:
      - name: redpanda
        type: tcp
        address: "redpanda:9092"
        interval: 10s
      - name: questdb
        type: http
        address: "http://questdb:9000/health"
        interval: 10s
      - name: dragonfly
        type: tcp
        address: "dragonfly:6379"
        interval: 10s

paryty-storage:
  enabled: false  # Use external storage by default
  dragonfly:
    enabled: false
  questdb:
    enabled: false
  redpanda:
    enabled: false
  seaweedfs:
    enabled: false
```

### 5.2 Helm Subchart Example (Agent)

**File:** `deploy/helm/paryty-agent/Chart.yaml`

```yaml
apiVersion: v2
name: paryty-agent
description: Paryty Agent — DaemonSet for node-level metric collection
type: application
version: 1.0.0
```

**File:** `deploy/helm/paryty-agent/templates/daemonset.yaml`

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: {{ include "paryty-agent.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "paryty-agent.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "paryty-agent.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "paryty-agent.selectorLabels" . | nindent 8 }}
    spec:
      serviceAccountName: {{ include "paryty-agent.serviceAccountName" . }}
      hostPID: true
      hostNetwork: true
      containers:
        - name: agent
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          securityContext:
            privileged: true  # Required for eBPF
            capabilities:
              add:
                - SYS_ADMIN
                - SYS_PTRACE
                - NET_ADMIN
          ports:
            - containerPort: 9090
              name: grpc
            - containerPort: 9091
              name: http
          env:
            - name: RUST_LOG
              value: {{ .Values.logLevel | default "info" | quote }}
            - name: PARYTY_CLUSTER_ADDR
              value: {{ .Values.clusterAddr | quote }}
            - name: PARYTY_COLLECT_INTERVAL
              value: {{ .Values.collectInterval | default "10s" | quote }}
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          volumeMounts:
            - name: certs
              mountPath: /etc/paryty/certs
              readOnly: true
            - name: proc
              mountPath: /host/proc
              readOnly: true
            - name: sys
              mountPath: /host/sys
              readOnly: true
      volumes:
        - name: certs
          secret:
            secretName: paryty-agent-tls
        - name: proc
          hostPath:
            path: /proc
        - name: sys
          hostPath:
            path: /sys
```

### 5.3 CI/CD Enhancement

**File:** `.github/workflows/cd.yml` (enhanced)

```yaml
# Enhanced CD pipeline with container scanning and SLSA provenance
name: CD

on:
  push:
    tags: ['v*']

jobs:
  # Build and push container images
  build:
    strategy:
      matrix:
        service: [agent, cluster, frontend]
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
      id-token: write  # For SLSA provenance
    steps:
      - uses: actions/checkout@v4

      - name: Build image
        run: |
          docker build -f deploy/docker/Dockerfile.${{ matrix.service }} \
            -t ghcr.io/paryty/${{ matrix.service }}:${{ github.ref_name }} .

      - name: Scan for vulnerabilities
        uses: aquasecurity/trivy-action@master
        with:
          image-ref: ghcr.io/paryty/${{ matrix.service }}:${{ github.ref_name }}
          format: 'sarif'
          output: 'trivy-results.sarif'
          severity: 'CRITICAL,HIGH'

      - name: Push to GHCR
        run: |
          echo "${{ secrets.GITHUB_TOKEN }}" | docker login ghcr.io -u ${{ github.actor }} --password-stdin
          docker push ghcr.io/paryty/${{ matrix.service }}:${{ github.ref_name }}

  # Publish Helm chart
  helm:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Package Helm chart
        run: helm package deploy/helm/paryty
      - name: Publish to GHCR OCI
        run: helm push paryty-*.tgz oci://ghcr.io/paryty/charts

  # SLSA provenance
  provenance:
    needs: build
    uses: slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@v1.9.0
    with:
      base-image: debian:bookworm-slim
      image: ghcr.io/paryty
      digest: ${{ needs.build.outputs.digest }}
      registry-username: ${{ github.actor }}
    secrets:
      registry-password: ${{ secrets.GITHUB_TOKEN }}
```

---

## 6. Layer 30: Testing & Validation

### 6.1 Integration Tests

**File:** `tests/integration/pipeline_test.go` (~400 LOC)

```go
// End-to-end pipeline integration tests.
//
// TESTS:
//   1. Agent → Ingestion → Redpanda → Aggregator → QuestDB
//   2. Query API → QuestDB → Response
//   3. Intelligence service → Forecasting → Redpanda
//   4. Agent reconnection after network interruption
//   5. Buffer replay after reconnection

package integration

import (
	"context"
	"testing"
	"time"
)

// TestFullPipeline tests the complete data pipeline.
func TestFullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. Start test infrastructure (Docker Compose)
	env := SetupTestEnvironment(ctx, t)
	defer env.Teardown()

	// 2. Send metrics via SDK
	client := env.SDKClient(t)
	counter := client.Counter("test_requests_total")
	for i := 0; i < 100; i++ {
		counter.Inc()
	}
	client.Flush(ctx)

	// 3. Wait for pipeline processing
	time.Sleep(5 * time.Second)

	// 4. Query from QuestDB
	queryClient := env.QueryClient(t)
	result, err := queryClient.Query(ctx, "SELECT count(*) FROM paryty.metrics WHERE name = 'test_requests_total'")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Rows) == 0 {
		t.Fatal("No metrics found in QuestDB")
	}

	t.Logf("Pipeline test passed: found %d rows", len(result.Rows))
}

// TestAgentReconnection tests agent reconnection after network failure.
func TestAgentReconnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := SetupTestEnvironment(ctx, t)
	defer env.Teardown()

	// 1. Send metrics
	client := env.SDKClient(t)
	client.Counter("reconnect_test").Inc()
	client.Flush(ctx)

	// 2. Simulate network interruption (block agent→cluster connection)
	env.BlockNetwork("agent", "cluster")
	time.Sleep(10 * time.Second)

	// 3. Send more metrics during outage (should be buffered)
	client.Counter("reconnect_test").Inc()
	client.Flush(ctx)

	// 4. Restore network
	env.RestoreNetwork("agent", "cluster")
	time.Sleep(15 * time.Second)

	// 5. Verify all metrics arrived
	queryClient := env.QueryClient(t)
	result, err := queryClient.Query(ctx, "SELECT count(*) FROM paryty.metrics WHERE name = 'reconnect_test'")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Should have 2 metrics (one before outage, one after replay)
	count := result.Rows[0][0].(int64)
	if count < 2 {
		t.Errorf("Expected at least 2 metrics, got %d", count)
	}
}

// TestQueryPerformance tests query latency under normal conditions.
func TestQueryPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env := SetupTestEnvironment(ctx, t)
	defer env.Teardown()

	// Seed data
	env.SeedMetrics(ctx, 10000)

	queryClient := env.QueryClient(t)

	// Test simple query latency
	start := time.Now()
	_, err := queryClient.Query(ctx, "SELECT * FROM paryty.metrics LIMIT 100")
	latency := time.Since(start)

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if latency > 500*time.Millisecond {
		t.Errorf("Query latency %v exceeds 500ms SLO", latency)
	}

	t.Logf("Query latency: %v (SLO: <500ms)", latency)
}
```

### 6.2 Load Tests

**File:** `tests/load/agent_load_test.go` (~300 LOC)

```go
// Load tests for Paryty under high agent count.
//
// SCENARIOS:
//   1. 1,000 agents sending metrics every 10s
//   2. 10,000 agents sending metrics every 10s
//   3. Burst: 1,000 agents connect simultaneously
//   4. Sustained: 10,000 agents for 1 hour
//
// MEASUREMENTS:
//   - Ingestion throughput (metrics/sec)
//   - Ingestion latency (p50, p95, p99)
//   - Redpanda consumer lag
//   - QuestDB write latency
//   - Memory usage (all services)
//   - CPU usage (all services)

package load

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLoad1KAgents simulates 1,000 agents.
func TestLoad1KAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	runAgentLoadTest(t, 1000, 10*time.Second, 5*time.Minute)
}

// TestLoad10KAgents simulates 10,000 agents.
func TestLoad10KAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	runAgentLoadTest(t, 10000, 10*time.Second, 10*time.Minute)
}

func runAgentLoadTest(t *testing.T, agentCount int, sendInterval time.Duration, duration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	env := SetupLoadTestEnvironment(ctx, t)
	defer env.Teardown()

	var (
		totalSent     atomic.Int64
		totalErrors   atomic.Int64
		totalLatency  atomic.Int64
	)

	var wg sync.WaitGroup
	for i := 0; i < agentCount; i++ {
		wg.Add(1)
		go func(agentID int) {
			defer wg.Done()
			client := env.SimulatedAgent(t, fmt.Sprintf("agent-%d", agentID))
			counter := client.Counter("load_test_metric")

			ticker := time.NewTicker(sendInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					start := time.Now()
					counter.Inc()
					if err := client.Flush(ctx); err != nil {
						totalErrors.Add(1)
					} else {
						totalLatency.Add(time.Since(start).Milliseconds())
					}
					totalSent.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	sent := totalSent.Load()
	errors := totalErrors.Load()
	avgLatency := time.Duration(totalLatency.Load()/max(sent, 1)) * time.Millisecond

	t.Logf("Load test results:")
	t.Logf("  Agents: %d", agentCount)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Total sent: %d", sent)
	t.Logf("  Errors: %d (%.2f%%)", errors, float64(errors)/float64(sent)*100)
	t.Logf("  Avg latency: %v", avgLatency)

	// Validate SLOs
	if avgLatency > 100*time.Millisecond {
		t.Errorf("Average latency %v exceeds 100ms SLO", avgLatency)
	}
	errorRate := float64(errors) / float64(sent)
	if errorRate > 0.001 { // 0.1% error rate threshold
		t.Errorf("Error rate %.2f%% exceeds 0.1%% threshold", errorRate*100)
	}
}
```

### 6.3 Chaos Tests

**File:** `tests/chaos/agent_reconnect_test.go` (~200 LOC)

```go
// Chaos tests for agent reconnection and data durability.
//
// SCENARIOS:
//   1. Agent network partition (30s) → verify buffer replay
//   2. Redpanda node failure → verify no data loss
//   3. QuestDB restart → verify write recovery
//   4. Dragonfly failover → verify hot store continuity
//   5. Cluster service crash → verify agent reconnection

package chaos

import (
	"context"
	"testing"
	"time"
)

// TestNetworkPartition tests agent behavior during network partition.
func TestNetworkPartition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	env := SetupChaosEnvironment(ctx, t)
	defer env.Teardown()

	// 1. Start agent, send baseline metrics
	agent := env.StartAgent(t)
	agent.SendMetric("chaos_test", 1)

	// 2. Partition network for 30 seconds
	env.PartitionNetwork("agent", "cluster", 30*time.Second)

	// 3. Agent should buffer metrics locally
	agent.SendMetric("chaos_test", 2)
	agent.SendMetric("chaos_test", 3)

	// 4. Wait for partition to heal
	time.Sleep(35 * time.Second)

	// 5. Verify all metrics arrived
	count := env.QueryMetricCount("chaos_test")
	if count < 3 {
		t.Errorf("Expected 3 metrics after partition, got %d", count)
	}
}

// TestRedpandaNodeDown tests data durability during Redpanda failure.
func TestRedpandaNodeDown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	env := SetupChaosEnvironment(ctx, t)
	defer env.Teardown()

	// 1. Send metrics
	env.SendMetrics(1000)

	// 2. Kill one Redpanda node (3-node cluster)
	env.KillService("redpanda-1")

	// 3. Send more metrics (should still work with 2/3 nodes)
	env.SendMetrics(1000)

	// 4. Restart Redpanda node
	env.RestartService("redpanda-1")
	time.Sleep(30 * time.Second)

	// 5. Verify all metrics survived
	count := env.QueryMetricCount("total")
	if count < 2000 {
		t.Errorf("Expected 2000 metrics, got %d (data loss detected)", count)
	}
}
```

---

## 7. Verification Gates

### Gate 1: mTLS (Automated)

```bash
# Test: All services communicate over TLS 1.3
# 1. Deploy with tls.mode=cert-manager
# 2. Check TLS version on all service ports
openssl s_client -connect paryty-agent:9090 -tls1_3
# 3. Verify mutual TLS (client cert required)
# 4. Verify cert rotation (delete cert, wait for renewal)

# Test: Self-signed fallback
# 1. Deploy with tls.mode=self-signed
# 2. Run generate-certs.sh
# 3. Verify services start with self-signed certs
# 4. Verify mTLS still works
```

### Gate 2: RBAC (Automated)

```bash
# Test: Role-based access
# 1. Create API key with viewer role
# 2. GET /api/v1/metrics → 200 OK
# 3. POST /api/v1/simulations → 403 Forbidden
# 4. DELETE /api/v1/settings → 403 Forbidden

# Test: Admin access
# 1. Create API key with admin role
# 2. All operations → 200 OK

# Test: Tenant isolation
# 1. Create key for tenant A
# 2. Query tenant B data → empty results
```

### Gate 3: Self-Monitoring (Manual)

```
# Test: Paryty monitors itself
# 1. Deploy Paryty
# 2. Open Paryty frontend
# 3. Verify: Paryty services appear in topology
# 4. Verify: Paryty metrics appear in dashboards
# 5. Verify: SLO tracking shows compliance

# Test: Health fallback
# 1. Stop Paryty monitoring pipeline
# 2. Kill Redpanda
# 3. Verify: Webhook alert received within 30s
```

### Gate 4: Helm Chart (Automated)

```bash
# Test: Fresh install
helm install paryty deploy/helm/paryty -n paryty --create-namespace
# Verify: All pods Running, all services Ready

# Test: Upgrade
helm upgrade paryty deploy/helm/paryty -n paryty
# Verify: Rolling update, no downtime

# Test: Uninstall
helm uninstall paryty -n paryty
# Verify: All resources cleaned up

# Test: Dev values
helm install paryty deploy/helm/paryty -f deploy/helm/paryty/values-dev.yaml
# Verify: Reduced replicas, TLS disabled
```

### Gate 5: Integration Tests (Automated)

```bash
# Run full integration test suite
cd tests/integration
go test -v -timeout 10m ./...
# Expected: All tests pass

# Run load tests
cd tests/load
go test -v -timeout 30m -run TestLoad10KAgents ./...
# Expected: <100ms avg latency, <0.1% error rate

# Run chaos tests
cd tests/chaos
go test -v -timeout 15m ./...
# Expected: All tests pass (data durability verified)
```

---

## 8. Performance Targets

| Metric | Target | Measurement |
|--------|--------|-------------|
| TLS handshake | < 5ms | mTLS connection establishment |
| RBAC check | < 0.1ms | Per-request permission check |
| Audit log write | < 1ms | Per audit entry |
| API key validation | < 1ms | SHA-256 hash lookup |
| Health check cycle | < 100ms | All infrastructure checks |
| SLO calculation | < 10ms | Per SLO window update |
| Helm install time | < 5 minutes | Full stack deployment |
| Integration test suite | < 10 minutes | All integration tests |
| Load test (10K agents) | < 100ms p99 | Ingestion latency under load |

---

## 9. Contingency & Rollback

### 9.1 Rollback Strategy

**Scenario 1: cert-manager not available in cluster**
- Switch to `tls.mode: self-signed`
- Run `scripts/security/generate-certs.sh`
- Services load certs from filesystem

**Scenario 2: RBAC too restrictive**
- Set `rbac.enabled: false` temporarily
- All requests treated as admin
- Fix policies, re-enable

**Scenario 3: Self-monitoring causes performance issues**
- Set `monitoring.selfMonitor.enabled: false`
- Health fallback continues working
- Investigate and fix monitoring overhead

**Scenario 4: Helm chart deployment fails**
- Fall back to raw K8s manifests (`deploy/kubernetes/base/`)
- Kustomize overlays for environment-specific config

---

## 10. Appendices

### Appendix A: Files to Create/Modify

| File | Action | LOC | Language |
|------|--------|-----|----------|
| `cluster/internal/security/tls.go` | CREATE | ~300 | Go |
| `cluster/internal/security/rbac.go` | CREATE | ~300 | Go |
| `cluster/internal/security/apikeys.go` | CREATE | ~200 | Go |
| `cluster/internal/security/audit.go` | CREATE | ~200 | Go |
| `cluster/internal/security/tenant.go` | CREATE | ~150 | Go |
| `cluster/internal/security/secrets.go` | CREATE | ~150 | Go |
| `cluster/internal/security/middleware.go` | CREATE | ~200 | Go |
| `cluster/internal/monitoring/self_monitor.go` | CREATE | ~400 | Go |
| `cluster/internal/monitoring/health_fallback.go` | CREATE | ~300 | Go |
| `cluster/internal/monitoring/alert_webhook.go` | CREATE | ~100 | Go |
| `cluster/internal/monitoring/alert_email.go` | CREATE | ~100 | Go |
| `cluster/internal/monitoring/slo_tracker.go` | CREATE | ~200 | Go |
| `cluster/internal/monitoring/dashboard.go` | CREATE | ~200 | Go |
| `deploy/helm/paryty/Chart.yaml` | CREATE | ~30 | YAML |
| `deploy/helm/paryty/values.yaml` | CREATE | ~200 | YAML |
| `deploy/helm/paryty/values-dev.yaml` | CREATE | ~50 | YAML |
| `deploy/helm/paryty/values-staging.yaml` | CREATE | ~50 | YAML |
| `deploy/helm/paryty/values-prod.yaml` | CREATE | ~80 | YAML |
| `deploy/helm/paryty/templates/_helpers.tpl` | CREATE | ~100 | Go Template |
| `deploy/helm/paryty-agent/Chart.yaml` | CREATE | ~10 | YAML |
| `deploy/helm/paryty-agent/values.yaml` | CREATE | ~50 | YAML |
| `deploy/helm/paryty-agent/templates/daemonset.yaml` | CREATE | ~80 | YAML |
| `deploy/helm/paryty-agent/templates/serviceaccount.yaml` | CREATE | ~15 | YAML |
| `deploy/helm/paryty-cluster/Chart.yaml` | CREATE | ~10 | YAML |
| `deploy/helm/paryty-cluster/values.yaml` | CREATE | ~80 | YAML |
| `deploy/helm/paryty-cluster/templates/` | CREATE | ~200 | YAML |
| `deploy/helm/paryty-frontend/Chart.yaml` | CREATE | ~10 | YAML |
| `deploy/helm/paryty-frontend/values.yaml` | CREATE | ~50 | YAML |
| `deploy/helm/paryty-frontend/templates/` | CREATE | ~100 | YAML |
| `deploy/helm/paryty-security/Chart.yaml` | CREATE | ~10 | YAML |
| `deploy/helm/paryty-security/values.yaml` | CREATE | ~50 | YAML |
| `deploy/helm/paryty-security/templates/` | CREATE | ~150 | YAML |
| `deploy/helm/paryty-monitoring/Chart.yaml` | CREATE | ~10 | YAML |
| `deploy/helm/paryty-monitoring/values.yaml` | CREATE | ~50 | YAML |
| `deploy/helm/paryty-monitoring/templates/` | CREATE | ~100 | YAML |
| `deploy/helm/paryty-storage/Chart.yaml` | CREATE | ~10 | YAML |
| `deploy/helm/paryty-storage/values.yaml` | CREATE | ~80 | YAML |
| `deploy/helm/paryty-storage/templates/` | CREATE | ~100 | YAML |
| `tests/integration/pipeline_test.go` | CREATE | ~400 | Go |
| `tests/integration/query_test.go` | CREATE | ~200 | Go |
| `tests/integration/intelligence_test.go` | CREATE | ~200 | Go |
| `tests/load/agent_load_test.go` | CREATE | ~300 | Go |
| `tests/load/query_load_test.go` | CREATE | ~100 | Go |
| `tests/load/storage_load_test.go` | CREATE | ~100 | Go |
| `tests/chaos/agent_reconnect_test.go` | CREATE | ~200 | Go |
| `tests/chaos/redpanda_failure_test.go` | CREATE | ~100 | Go |
| `tests/chaos/storage_failover_test.go` | CREATE | ~100 | Go |
| `scripts/security/generate-certs.sh` | CREATE | ~100 | Shell |
| `scripts/security/rotate-certs.sh` | CREATE | ~50 | Shell |
| `.github/workflows/cd.yml` | ENHANCE | +200 | YAML |
| **TOTAL** | | **~7,000** | |

---

**END OF PHASE 8 HARDENED SPECIFICATION**

---

# ALL 8 PHASES COMPLETE

| Phase | Spec | Lines | LOC Target |
|-------|------|-------|------------|
| 1 | `docs/phase-1-hardened-spec.md` | ~1,500 | ~15,000 |
| 2 | `docs/phase-2-hardened-spec.md` | ~1,200 | ~10,000 |
| 3 | `docs/phase-3-hardened-spec.md` | ~1,000 | ~8,000 |
| 4 | `docs/phase-4-hardened-spec.md` | ~1,100 | ~9,000 |
| 5 | `docs/phase-5-hardened-spec.md` | 1,884 | ~10,400 |
| 6 | `docs/phase-6-hardened-spec.md` | 3,165 | ~14,200 |
| 7 | `docs/phase-7-hardened-spec.md` | 1,482 | ~4,200 |
| 8 | `docs/phase-8-hardened-spec.md` | ~2,000 | ~7,000 |
| **Total** | | **~13,000** | **~77,800** |

Combined with existing codebase (~29K LOC), total reaches **~107K LOC** — within the target range.
