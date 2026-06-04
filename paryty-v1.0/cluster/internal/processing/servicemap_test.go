package processing

import (
	"sync"
	"testing"
)

// =============================================================================
// TestServiceMap_DefaultRules
// =============================================================================

func TestServiceMap_DefaultRules(t *testing.T) {
	sm, err := NewServiceMap(nil, newTestLogger())
	if err != nil {
		t.Fatalf("NewServiceMap(nil) returned error: %v", err)
	}

	tests := []struct {
		name     string
		process  string
		expected string
	}{
		{name: "nginx worker", process: "nginx-worker", expected: "nginx"},
		{name: "nginx exact", process: "nginx", expected: "nginx"},
		{name: "postgres exact", process: "postgres", expected: "postgresql"},
		{name: "postgres process", process: "postgres-1234", expected: "postgresql"},
		{name: "redis-server exact", process: "redis-server", expected: "redis"},
		{name: "redis-server process", process: "redis-server-5678", expected: "redis"},
		{name: "mongod", process: "mongod", expected: "mongodb"},
		{name: "node", process: "node", expected: "nodejs"},
		{name: "node process", process: "node-worker-1", expected: "nodejs"},
		{name: "python3", process: "python3", expected: "python"},
		{name: "python3 process", process: "python3.11", expected: "python"},
		{name: "java", process: "java", expected: "java"},
		{name: "java process", process: "java-17-server", expected: "java"},
		{name: "etcd", process: "etcd", expected: "etcd"},
		{name: "kube-apiserver", process: "kube-apiserver", expected: "kubernetes-api"},
		{name: "kubelet", process: "kubelet", expected: "kubelet"},
		{name: "unknown app fallback", process: "my-custom-app", expected: "my-custom-app"},
		{name: "unknown service fallback", process: "some-random-daemon", expected: "some-random-daemon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sm.Resolve(tt.process)
			if got != tt.expected {
				t.Errorf("Resolve(%q) = %q, want %q", tt.process, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// TestServiceMap_CustomOverride
// =============================================================================

func TestServiceMap_CustomOverride(t *testing.T) {
	sm, err := NewServiceMap(nil, newTestLogger())
	if err != nil {
		t.Fatalf("NewServiceMap(nil) returned error: %v", err)
	}

	// Before custom override, nginx-worker resolves via prefix rule.
	if got := sm.Resolve("nginx-worker"); got != "nginx" {
		t.Fatalf("before override: Resolve(%q) = %q, want %q", "nginx-worker", got, "nginx")
	}

	// Add custom override for "nginx" → "my-nginx-gateway".
	sm.AddCustom("nginx", "my-nginx-gateway")

	tests := []struct {
		name     string
		process  string
		expected string
	}{
		{name: "nginx exact override", process: "nginx", expected: "my-nginx-gateway"},
		{name: "nginx worker override", process: "nginx-worker", expected: "my-nginx-gateway"},
		{name: "nginx worker 1234 override", process: "nginx-worker-1234", expected: "my-nginx-gateway"},
		{name: "postgres still uses rule", process: "postgres", expected: "postgresql"},
		{name: "unrelated fallback unchanged", process: "my-custom-app", expected: "my-custom-app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sm.Resolve(tt.process)
			if got != tt.expected {
				t.Errorf("Resolve(%q) = %q, want %q", tt.process, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// TestServiceMap_RegexRule
// =============================================================================

func TestServiceMap_RegexRule(t *testing.T) {
	rules := []ServiceMappingRule{
		{Match: `app-.*-service`, ServiceName: "app-service", IsRegex: true},
	}

	sm, err := NewServiceMap(rules, newTestLogger())
	if err != nil {
		t.Fatalf("NewServiceMap returned error: %v", err)
	}

	tests := []struct {
		name     string
		process  string
		expected string
	}{
		{name: "auth service", process: "app-auth-service", expected: "app-service"},
		{name: "payment service", process: "app-payment-service", expected: "app-service"},
		{name: "user service", process: "app-user-service", expected: "app-service"},
		{name: "non-matching prefix", process: "svc-auth-service", expected: "svc-auth-service"},
		{name: "partial match no suffix", process: "app-auth", expected: "app-auth"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sm.Resolve(tt.process)
			if got != tt.expected {
				t.Errorf("Resolve(%q) = %q, want %q", tt.process, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// TestServiceMap_RemoveCustom
// =============================================================================

func TestServiceMap_RemoveCustom(t *testing.T) {
	sm, err := NewServiceMap(nil, newTestLogger())
	if err != nil {
		t.Fatalf("NewServiceMap(nil) returned error: %v", err)
	}

	// Add custom override.
	sm.AddCustom("nginx", "custom-nginx")

	if got := sm.Resolve("nginx"); got != "custom-nginx" {
		t.Fatalf("after AddCustom: Resolve(%q) = %q, want %q", "nginx", got, "custom-nginx")
	}

	if got := sm.Resolve("nginx-worker"); got != "custom-nginx" {
		t.Fatalf("after AddCustom: Resolve(%q) = %q, want %q", "nginx-worker", got, "custom-nginx")
	}

	// Remove custom override.
	sm.RemoveCustom("nginx")

	// Should fall back to prefix rule.
	if got := sm.Resolve("nginx"); got != "nginx" {
		t.Errorf("after RemoveCustom: Resolve(%q) = %q, want %q", "nginx", got, "nginx")
	}

	if got := sm.Resolve("nginx-worker"); got != "nginx" {
		t.Errorf("after RemoveCustom: Resolve(%q) = %q, want %q", "nginx-worker", got, "nginx")
	}

	// RemoveCustom on non-existent key is a no-op.
	sm.RemoveCustom("nonexistent-process")

	// Fallback still works.
	if got := sm.Resolve("my-custom-app"); got != "my-custom-app" {
		t.Errorf("Resolve(%q) = %q, want %q", "my-custom-app", got, "my-custom-app")
	}
}

// =============================================================================
// TestServiceMap_InvalidRegex
// =============================================================================

func TestServiceMap_InvalidRegex(t *testing.T) {
	tests := []struct {
		name  string
		rules []ServiceMappingRule
	}{
		{
			name: "unmatched bracket",
			rules: []ServiceMappingRule{
				{Match: "[invalid", ServiceName: "bad", IsRegex: true},
			},
		},
		{
			name: "unmatched parenthesis",
			rules: []ServiceMappingRule{
				{Match: "(unclosed", ServiceName: "bad", IsRegex: true},
			},
		},
		{
			name: "invalid quantifier",
			rules: []ServiceMappingRule{
				{Match: "*invalid", ServiceName: "bad", IsRegex: true},
			},
		},
		{
			name: "mixed valid and invalid",
			rules: []ServiceMappingRule{
				{Match: `app-.*`, ServiceName: "app", IsRegex: true},
				{Match: "[broken", ServiceName: "bad", IsRegex: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, err := NewServiceMap(tt.rules, newTestLogger())
			if err == nil {
				t.Fatal("NewServiceMap with invalid regex should return error, got nil")
			}
			if sm != nil {
				t.Fatal("NewServiceMap with invalid regex should return nil ServiceMap")
			}
		})
	}
}

// =============================================================================
// TestServiceMap_EmptyProcessName
// =============================================================================

func TestServiceMap_EmptyProcessName(t *testing.T) {
	sm, err := NewServiceMap(nil, newTestLogger())
	if err != nil {
		t.Fatalf("NewServiceMap(nil) returned error: %v", err)
	}

	got := sm.Resolve("")
	if got != "" {
		t.Errorf("Resolve(%q) = %q, want %q", "", got, "")
	}
}

// =============================================================================
// TestServiceMap_ConcurrentResolve
// =============================================================================

func TestServiceMap_ConcurrentResolve(t *testing.T) {
	rules := []ServiceMappingRule{
		{Match: `app-.*-service`, ServiceName: "app-service", IsRegex: true},
		{Match: "nginx", ServiceName: "nginx"},
		{Match: "postgres", ServiceName: "postgresql"},
	}

	sm, err := NewServiceMap(rules, newTestLogger())
	if err != nil {
		t.Fatalf("NewServiceMap returned error: %v", err)
	}

	sm.AddCustom("nginx", "custom-nginx")

	processNames := []string{
		"nginx-worker", "postgres", "app-auth-service", "my-custom-app",
		"redis-server", "app-payment-service", "node", "etcd",
		"kubelet", "java", "python3.11", "",
	}

	const goroutines = 20
	const iterations = 500

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				for _, p := range processNames {
					_ = sm.Resolve(p)
				}
			}
		}()
	}

	// Concurrent writers alongside readers.
	wg.Add(5)
	for g := 0; g < 5; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				sm.AddCustom("concurrent-proc", "concurrent-svc")
				_ = sm.Resolve("concurrent-proc")
				sm.RemoveCustom("concurrent-proc")
				_ = sm.Resolve("concurrent-proc")
			}
		}(g)
	}

	wg.Wait()

	// Verify correctness after concurrent access.
	if got := sm.Resolve("nginx-worker"); got != "custom-nginx" {
		t.Errorf("after concurrent access: Resolve(%q) = %q, want %q", "nginx-worker", got, "custom-nginx")
	}
	if got := sm.Resolve("my-custom-app"); got != "my-custom-app" {
		t.Errorf("after concurrent access: Resolve(%q) = %q, want %q", "my-custom-app", got, "my-custom-app")
	}
}

// =============================================================================
// TestServiceMap_NilRulesUsesDefaults
// =============================================================================

func TestServiceMap_NilRulesUsesDefaults(t *testing.T) {
	sm, err := NewServiceMap(nil, newTestLogger())
	if err != nil {
		t.Fatalf("NewServiceMap(nil) returned error: %v", err)
	}

	// Verify all 10 default rules resolve correctly.
	defaults := []struct {
		process  string
		expected string
	}{
		{"nginx", "nginx"},
		{"postgres", "postgresql"},
		{"redis-server", "redis"},
		{"mongod", "mongodb"},
		{"node", "nodejs"},
		{"python3", "python"},
		{"java", "java"},
		{"etcd", "etcd"},
		{"kube-apiserver", "kubernetes-api"},
		{"kubelet", "kubelet"},
	}

	if len(DefaultServiceMappingRules) != 10 {
		t.Fatalf("DefaultServiceMappingRules has %d entries, want 10", len(DefaultServiceMappingRules))
	}

	for _, tt := range defaults {
		t.Run(tt.process, func(t *testing.T) {
			got := sm.Resolve(tt.process)
			if got != tt.expected {
				t.Errorf("Resolve(%q) = %q, want %q", tt.process, got, tt.expected)
			}
		})
	}
}
