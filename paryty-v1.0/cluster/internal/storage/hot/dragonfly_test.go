package hot

import (
	"testing"
	"time"
)

// ---- Key generation tests ----

func TestMetricsKey(t *testing.T) {
	tests := []struct {
		name    string
		tenant  string
		agentID string
		want    string
	}{
		{
			name:    "standard tenant and agent",
			tenant:  "acme-corp",
			agentID: "agent-42",
			want:    "paryty:acme-corp:metrics:agent-42:latest",
		},
		{
			name:    "default tenant",
			tenant:  "default",
			agentID: "agent-1",
			want:    "paryty:default:metrics:agent-1:latest",
		},
		{
			name:    "tenant with special chars",
			tenant:  "org-123_test",
			agentID: "host-abc",
			want:    "paryty:org-123_test:metrics:host-abc:latest",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := metricsKey(tc.tenant, tc.agentID)
			if got != tc.want {
				t.Errorf("metricsKey(%q, %q) = %q, want %q", tc.tenant, tc.agentID, got, tc.want)
			}
		})
	}
}

func TestAgentStateKey(t *testing.T) {
	tests := []struct {
		name    string
		tenant  string
		agentID string
		want    string
	}{
		{
			name:    "standard",
			tenant:  "acme-corp",
			agentID: "agent-42",
			want:    "paryty:acme-corp:agent:agent-42",
		},
		{
			name:    "empty agent",
			tenant:  "default",
			agentID: "",
			want:    "paryty:default:agent:",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := agentStateKey(tc.tenant, tc.agentID)
			if got != tc.want {
				t.Errorf("agentStateKey(%q, %q) = %q, want %q", tc.tenant, tc.agentID, got, tc.want)
			}
		})
	}
}

func TestTopologyKey(t *testing.T) {
	tests := []struct {
		name   string
		tenant string
		want   string
	}{
		{
			name:   "standard tenant",
			tenant: "acme-corp",
			want:   "paryty:acme-corp:topology:current",
		},
		{
			name:   "default tenant",
			tenant: "default",
			want:   "paryty:default:topology:current",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := topologyKey(tc.tenant)
			if got != tc.want {
				t.Errorf("topologyKey(%q) = %q, want %q", tc.tenant, got, tc.want)
			}
		})
	}
}

func TestAlertsKey(t *testing.T) {
	got := alertsKey("tenant-a")
	want := "paryty:tenant-a:alerts:active"
	if got != want {
		t.Errorf("alertsKey(%q) = %q, want %q", "tenant-a", got, want)
	}
}

func TestHealthKey(t *testing.T) {
	got := healthKey("tenant-a", "agent-7")
	want := "paryty:tenant-a:health:agent-7"
	if got != want {
		t.Errorf("healthKey(%q, %q) = %q, want %q", "tenant-a", "agent-7", got, want)
	}
}

func TestAgentStatePattern(t *testing.T) {
	got := agentStatePattern("tenant-a")
	want := "paryty:tenant-a:agent:*"
	if got != want {
		t.Errorf("agentStatePattern(%q) = %q, want %q", "tenant-a", got, want)
	}
}

// ---- TTL configuration tests ----

func TestNew_DefaultTTL(t *testing.T) {
	cfg := Config{
		Addr: "localhost:6379",
		// TTL intentionally omitted — should default to 5 minutes.
	}

	c := New(cfg)

	if c.cfg.TTL != 5*time.Minute {
		t.Errorf("default TTL = %v, want %v", c.cfg.TTL, 5*time.Minute)
	}
}

func TestNew_CustomTTL(t *testing.T) {
	custom := 10 * time.Minute
	cfg := Config{
		Addr: "localhost:6379",
		TTL:  custom,
	}

	c := New(cfg)

	if c.cfg.TTL != custom {
		t.Errorf("custom TTL = %v, want %v", c.cfg.TTL, custom)
	}
}

func TestNew_ZeroTTL_DefaultsToFiveMinutes(t *testing.T) {
	cfg := Config{
		Addr: "localhost:6379",
		TTL:  0,
	}

	c := New(cfg)

	if c.cfg.TTL != defaultTTL {
		t.Errorf("zero TTL should default to %v, got %v", defaultTTL, c.cfg.TTL)
	}
}

// ---- Tenant isolation tests ----

func TestTenantIsolation_DifferentTenantsProduceDifferentKeys(t *testing.T) {
	tenants := []string{"tenant-a", "tenant-b", "tenant-c"}
	agentID := "agent-1"

	// Collect keys for each tenant across all key functions.
	type keyFn struct {
		name string
		fn   func(string) string
	}

	singleTenantFns := []keyFn{
		{"topologyKey", func(t string) string { return topologyKey(t) }},
		{"alertsKey", func(t string) string { return alertsKey(t) }},
	}

	agentFns := []struct {
		name string
		fn   func(string, string) string
	}{
		{"metricsKey", metricsKey},
		{"agentStateKey", agentStateKey},
		{"healthKey", healthKey},
	}

	// Verify single-tenant key functions produce unique keys per tenant.
	for _, kf := range singleTenantFns {
		seen := make(map[string]string, len(tenants))
		for _, tenant := range tenants {
			key := kf.fn(tenant)
			if prev, exists := seen[key]; exists {
				t.Errorf("%s: tenants %q and %q produced same key %q", kf.name, prev, tenant, key)
			}
			seen[key] = tenant
		}
	}

	// Verify agent-scoped key functions produce unique keys per tenant.
	for _, af := range agentFns {
		seen := make(map[string]string, len(tenants))
		for _, tenant := range tenants {
			key := af.fn(tenant, agentID)
			if prev, exists := seen[key]; exists {
				t.Errorf("%s: tenants %q and %q produced same key %q for agent %q", af.name, prev, tenant, key, agentID)
			}
			seen[key] = tenant
		}
	}
}

func TestTenantIsolation_KeysContainTenantSegment(t *testing.T) {
	tenant := "acme-corp"

	keyFuncs := []struct {
		name string
		key  string
	}{
		{"metricsKey", metricsKey(tenant, "agent-1")},
		{"agentStateKey", agentStateKey(tenant, "agent-1")},
		{"topologyKey", topologyKey(tenant)},
		{"alertsKey", alertsKey(tenant)},
		{"healthKey", healthKey(tenant, "agent-1")},
	}

	for _, kf := range keyFuncs {
		// Every key must contain "paryty:<tenant>:" prefix.
		expectedPrefix := "paryty:" + tenant + ":"
		if len(kf.key) < len(expectedPrefix) || kf.key[:len(expectedPrefix)] != expectedPrefix {
			t.Errorf("%s = %q does not have expected prefix %q", kf.name, kf.key, expectedPrefix)
		}
	}
}

func TestTenantIsolation_SameAgentDifferentTenantsNeverCollide(t *testing.T) {
	agentID := "shared-agent-id"

	keyPairs := []struct {
		name string
		fn   func(string) string
	}{
		{"metricsKey", func(t string) string { return metricsKey(t, agentID) }},
		{"agentStateKey", func(t string) string { return agentStateKey(t, agentID) }},
		{"healthKey", func(t string) string { return healthKey(t, agentID) }},
	}

	for _, kp := range keyPairs {
		keyA := kp.fn("tenant-alpha")
		keyB := kp.fn("tenant-beta")
		if keyA == keyB {
			t.Errorf("%s: same agent across different tenants produced colliding key %q", kp.name, keyA)
		}
	}
}

// ---- Key prefix format tests ----

func TestKeyPrefixConsistency(t *testing.T) {
	// All keys must start with "paryty:".
	allKeys := []struct {
		name string
		key  string
	}{
		{"metricsKey", metricsKey("t", "a")},
		{"agentStateKey", agentStateKey("t", "a")},
		{"topologyKey", topologyKey("t")},
		{"alertsKey", alertsKey("t")},
		{"healthKey", healthKey("t", "a")},
	}

	const prefix = "paryty:"
	for _, ak := range allKeys {
		if len(ak.key) < len(prefix) || ak.key[:len(prefix)] != prefix {
			t.Errorf("%s = %q does not start with %q", ak.name, ak.key, prefix)
		}
	}
}

func TestAgentStatePattern_MatchesKeyPrefix(t *testing.T) {
	// The SCAN pattern should be the agent state prefix + "*".
	tenant := "test-tenant"
	pattern := agentStatePattern(tenant)
	key := agentStateKey(tenant, "some-agent")

	// The pattern without the trailing "*" should be a prefix of the key.
	prefix := pattern[:len(pattern)-1] // remove trailing "*"
	if len(key) < len(prefix) || key[:len(prefix)] != prefix {
		t.Errorf("agentStatePattern(%q) = %q is not a prefix of agentStateKey(%q, ...) = %q",
			tenant, pattern, tenant, key)
	}
}
