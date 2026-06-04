package hot

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ---- Mock redis client ----

// mockRedis implements redisCmdable with an in-memory store for testing.
type mockRedis struct {
	store map[string]string
}

func newMockRedis() *mockRedis {
	return &mockRedis{store: make(map[string]string)}
}

func (m *mockRedis) Get(_ context.Context, key string) *redis.StringCmd {
	val, ok := m.store[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(val, nil)
}

func (m *mockRedis) Set(_ context.Context, key string, value interface{}, _ time.Duration) *redis.StatusCmd {
	m.store[key] = toStr(value)
	return redis.NewStatusResult("OK", nil)
}

func (m *mockRedis) SetNX(_ context.Context, key string, value interface{}, _ time.Duration) *redis.BoolCmd {
	if _, exists := m.store[key]; exists {
		return redis.NewBoolResult(false, nil)
	}
	m.store[key] = toStr(value)
	return redis.NewBoolResult(true, nil)
}

func (m *mockRedis) Del(_ context.Context, keys ...string) *redis.IntCmd {
	var count int64
	for _, key := range keys {
		if _, ok := m.store[key]; ok {
			delete(m.store, key)
			count++
		}
	}
	return redis.NewIntResult(count, nil)
}

func (m *mockRedis) Scan(_ context.Context, _ uint64, match string, _ int64) *redis.ScanCmd {
	// Convert Redis glob pattern to regex: * → [^:]+ (match within segment).
	pattern := "^" + regexp.QuoteMeta(match)
	pattern = strings.ReplaceAll(pattern, `\*`, `[^:]+`)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return redis.NewScanCmdResult(nil, 0, err)
	}

	var keys []string
	for k := range m.store {
		if re.MatchString(k) {
			keys = append(keys, k)
		}
	}

	// Return all keys in a single page (cursor=0 means iteration complete).
	return redis.NewScanCmdResult(keys, 0, nil)
}

func (m *mockRedis) Pipeline() alertPipeline {
	return &mockPipeline{store: m.store}
}

// ---- Mock pipeline ----

// mockPipeline implements alertPipeline with the in-memory store.
type mockPipeline struct {
	store map[string]string
}

func (p *mockPipeline) RPush(_ context.Context, key string, values ...interface{}) *redis.IntCmd {
	var parts []string
	if existing, ok := p.store[key]; ok && existing != "" {
		parts = append(parts, existing)
	}
	for _, v := range values {
		parts = append(parts, toStr(v))
	}
	p.store[key] = strings.Join(parts, "|")
	return redis.NewIntResult(int64(len(parts)), nil)
}

func (p *mockPipeline) Expire(_ context.Context, _ string, _ time.Duration) *redis.BoolCmd {
	return redis.NewBoolResult(true, nil)
}

func (p *mockPipeline) Exec(_ context.Context) ([]redis.Cmder, error) {
	return nil, nil
}

// ---- Helper ----

func toStr(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// ---- Test helpers ----

func newTestAlertOps() (*mockRedis, *AlertOps) {
	m := newMockRedis()
	logger := zap.NewNop()
	ops := NewAlertOps(m, logger)
	return m, ops
}

func seedAlertState(m *mockRedis, tenant, alertID string, state AlertState) {
	m.store[alertStateKey(tenant, alertID)] = string(state)
}

func seedAlertMeta(m *mockRedis, tenant string, alert *models.Alert) {
	data, _ := json.Marshal(alert)
	m.store[alertMetaKey(tenant, alert.ID)] = string(data)
}

func seedFingerprint(m *mockRedis, tenant, hash, alertID string) {
	m.store[alertFingerprintKey(tenant, hash)] = alertID
}

// ---- Tests ----

func TestAlertOps_ValidTransition(t *testing.T) {
	ctx := context.Background()
	tenant := "test-tenant"

	tests := []struct {
		name   string
		from   AlertState
		to     AlertState
		reason string
	}{
		{
			name:   "firing to resolved",
			from:   AlertStateFiring,
			to:     AlertStateResolved,
			reason: "condition cleared",
		},
		{
			name:   "firing to acknowledged",
			from:   AlertStateFiring,
			to:     AlertStateAcknowledged,
			reason: "on-call acknowledged",
		},
		{
			name:   "firing to silenced",
			from:   AlertStateFiring,
			to:     AlertStateSilenced,
			reason: "maintenance window",
		},
		{
			name:   "acknowledged to firing",
			from:   AlertStateAcknowledged,
			to:     AlertStateFiring,
			reason: "condition re-triggered",
		},
		{
			name:   "acknowledged to resolved",
			from:   AlertStateAcknowledged,
			to:     AlertStateResolved,
			reason: "condition cleared",
		},
		{
			name:   "silenced to firing",
			from:   AlertStateSilenced,
			to:     AlertStateFiring,
			reason: "silence expired",
		},
		{
			name:   "silenced to resolved",
			from:   AlertStateSilenced,
			to:     AlertStateResolved,
			reason: "condition cleared",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, ops := newTestAlertOps()
			alertID := "alert-" + string(tc.from) + "-" + string(tc.to)

			// Pre-seed the alert in the starting state.
			seedAlertState(m, tenant, alertID, tc.from)

			err := ops.TransitionAlert(ctx, tenant, alertID, tc.to, tc.reason)
			if err != nil {
				t.Fatalf("TransitionAlert(%s → %s) returned error: %v", tc.from, tc.to, err)
			}

			// Verify the state was updated.
			state, err := ops.GetAlertState(ctx, tenant, alertID)
			if err != nil {
				t.Fatalf("GetAlertState returned error: %v", err)
			}
			if state != tc.to {
				t.Errorf("expected state %q, got %q", tc.to, state)
			}
		})
	}
}

func TestAlertOps_InvalidTransition(t *testing.T) {
	ctx := context.Background()
	tenant := "test-tenant"

	tests := []struct {
		name string
		from AlertState
		to   AlertState
	}{
		{
			name: "resolved to acknowledged (terminal)",
			from: AlertStateResolved,
			to:   AlertStateAcknowledged,
		},
		{
			name: "resolved to firing (terminal)",
			from: AlertStateResolved,
			to:   AlertStateFiring,
		},
		{
			name: "resolved to silenced (terminal)",
			from: AlertStateResolved,
			to:   AlertStateSilenced,
		},
		{
			name: "silenced to acknowledged (not allowed)",
			from: AlertStateSilenced,
			to:   AlertStateAcknowledged,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, ops := newTestAlertOps()
			alertID := "alert-invalid-" + string(tc.from) + "-" + string(tc.to)

			seedAlertState(m, tenant, alertID, tc.from)

			err := ops.TransitionAlert(ctx, tenant, alertID, tc.to, "should fail")
			if err == nil {
				t.Fatalf("TransitionAlert(%s → %s) should have returned error", tc.from, tc.to)
			}

			// Verify the state was NOT changed.
			state, err := ops.GetAlertState(ctx, tenant, alertID)
			if err != nil {
				t.Fatalf("GetAlertState returned error: %v", err)
			}
			if state != tc.from {
				t.Errorf("state should remain %q after invalid transition, got %q", tc.from, state)
			}
		})
	}
}

func TestAlertOps_Deduplicate(t *testing.T) {
	ctx := context.Background()
	tenant := "test-tenant"

	t.Run("new alert is not duplicate", func(t *testing.T) {
		_, ops := newTestAlertOps()

		alert := &models.Alert{
			ID:       "alert-dedup-001",
			Name:     "high_cpu",
			Severity: models.AlertSeverityCritical,
			Labels:   map[string]string{"host": "web-1", "env": "prod"},
		}

		isDupe, err := ops.DeduplicateAlert(ctx, tenant, alert)
		if err != nil {
			t.Fatalf("DeduplicateAlert returned error: %v", err)
		}
		if isDupe {
			t.Error("new alert should not be a duplicate")
		}

		// Verify the alert was stored as firing.
		state, err := ops.GetAlertState(ctx, tenant, alert.ID)
		if err != nil {
			t.Fatalf("GetAlertState returned error: %v", err)
		}
		if state != AlertStateFiring {
			t.Errorf("expected state %q, got %q", AlertStateFiring, state)
		}
	})

	t.Run("firing alert is detected as duplicate", func(t *testing.T) {
		_, ops := newTestAlertOps()

		alert := &models.Alert{
			ID:       "alert-dedup-002",
			Name:     "high_cpu",
			Severity: models.AlertSeverityCritical,
			Labels:   map[string]string{"host": "web-1", "env": "prod"},
		}

		// First submission — not a duplicate.
		isDupe, err := ops.DeduplicateAlert(ctx, tenant, alert)
		if err != nil {
			t.Fatalf("first DeduplicateAlert returned error: %v", err)
		}
		if isDupe {
			t.Error("first alert should not be a duplicate")
		}

		// Second submission with same fingerprint — should be duplicate.
		alert2 := &models.Alert{
			ID:       "alert-dedup-003", // different ID, same fingerprint
			Name:     "high_cpu",
			Severity: models.AlertSeverityCritical,
			Labels:   map[string]string{"host": "web-1", "env": "prod"},
		}

		isDupe, err = ops.DeduplicateAlert(ctx, tenant, alert2)
		if err != nil {
			t.Fatalf("second DeduplicateAlert returned error: %v", err)
		}
		if !isDupe {
			t.Error("second alert with same fingerprint should be a duplicate")
		}
	})

	t.Run("resolved alert allows re-firing", func(t *testing.T) {
		_, ops := newTestAlertOps()

		alert := &models.Alert{
			ID:       "alert-dedup-004",
			Name:     "disk_full",
			Severity: models.AlertSeverityError,
			Labels:   map[string]string{"host": "db-1"},
		}

		// First submission.
		_, err := ops.DeduplicateAlert(ctx, tenant, alert)
		if err != nil {
			t.Fatalf("first DeduplicateAlert returned error: %v", err)
		}

		// Resolve the alert.
		err = ops.TransitionAlert(ctx, tenant, alert.ID, AlertStateResolved, "disk freed")
		if err != nil {
			t.Fatalf("TransitionAlert returned error: %v", err)
		}

		// Re-fire with same fingerprint — should NOT be duplicate.
		alert2 := &models.Alert{
			ID:       "alert-dedup-005",
			Name:     "disk_full",
			Severity: models.AlertSeverityError,
			Labels:   map[string]string{"host": "db-1"},
		}

		isDupe, err := ops.DeduplicateAlert(ctx, tenant, alert2)
		if err != nil {
			t.Fatalf("re-fire DeduplicateAlert returned error: %v", err)
		}
		if isDupe {
			t.Error("re-fire after resolution should not be a duplicate")
		}
	})

	t.Run("different fingerprints are independent", func(t *testing.T) {
		_, ops := newTestAlertOps()

		alert1 := &models.Alert{
			ID:       "alert-dedup-006",
			Name:     "high_cpu",
			Severity: models.AlertSeverityWarning,
			Labels:   map[string]string{"host": "web-1"},
		}
		alert2 := &models.Alert{
			ID:       "alert-dedup-007",
			Name:     "high_cpu",
			Severity: models.AlertSeverityCritical, // different severity → different fingerprint
			Labels:   map[string]string{"host": "web-1"},
		}

		_, err := ops.DeduplicateAlert(ctx, tenant, alert1)
		if err != nil {
			t.Fatalf("first DeduplicateAlert returned error: %v", err)
		}

		isDupe, err := ops.DeduplicateAlert(ctx, tenant, alert2)
		if err != nil {
			t.Fatalf("second DeduplicateAlert returned error: %v", err)
		}
		if isDupe {
			t.Error("alerts with different severities should not be duplicates")
		}
	})
}

func TestAlertOps_GetActiveByGroup(t *testing.T) {
	ctx := context.Background()
	tenant := "test-tenant"
	m, ops := newTestAlertOps()

	// Seed alerts with different severities and states.
	type alertSeed struct {
		id       string
		severity models.AlertSeverity
		state    AlertState
		source   string
	}

	seeds := []alertSeed{
		{"alert-grp-001", models.AlertSeverityCritical, AlertStateFiring, "agent-1"},
		{"alert-grp-002", models.AlertSeverityCritical, AlertStateAcknowledged, "agent-1"},
		{"alert-grp-003", models.AlertSeverityWarning, AlertStateFiring, "agent-2"},
		{"alert-grp-004", models.AlertSeverityError, AlertStateSilenced, "agent-3"},
		{"alert-grp-005", models.AlertSeverityInfo, AlertStateResolved, "agent-1"}, // excluded from active
	}

	for _, s := range seeds {
		seedAlertState(m, tenant, s.id, s.state)
		seedAlertMeta(m, tenant, &models.Alert{
			ID:       s.id,
			Name:     "test-alert",
			Severity: s.severity,
			Source:   s.source,
			Labels:   map[string]string{},
		})
	}

	t.Run("group by severity", func(t *testing.T) {
		grouped, err := ops.GetActiveAlertsByGroup(ctx, tenant, "severity")
		if err != nil {
			t.Fatalf("GetActiveAlertsByGroup returned error: %v", err)
		}

		// Should have 3 severity groups (critical, warning, error).
		// info is excluded because alert-grp-005 is resolved.
		if len(grouped) != 3 {
			t.Errorf("expected 3 severity groups, got %d: %v", len(grouped), groupKeys(grouped))
		}

		// Critical group should have 2 alerts (firing + acknowledged).
		criticalAlerts := grouped[string(models.AlertSeverityCritical)]
		if len(criticalAlerts) != 2 {
			t.Errorf("expected 2 critical alerts, got %d", len(criticalAlerts))
		}

		// Warning group should have 1 alert.
		warningAlerts := grouped[string(models.AlertSeverityWarning)]
		if len(warningAlerts) != 1 {
			t.Errorf("expected 1 warning alert, got %d", len(warningAlerts))
		}

		// Error group should have 1 alert (silenced is active).
		errorAlerts := grouped[string(models.AlertSeverityError)]
		if len(errorAlerts) != 1 {
			t.Errorf("expected 1 error alert, got %d", len(errorAlerts))
		}
	})

	t.Run("group by source", func(t *testing.T) {
		grouped, err := ops.GetActiveAlertsByGroup(ctx, tenant, "source")
		if err != nil {
			t.Fatalf("GetActiveAlertsByGroup returned error: %v", err)
		}

		// Should have 3 source groups (agent-1, agent-2, agent-3).
		if len(grouped) != 3 {
			t.Errorf("expected 3 source groups, got %d: %v", len(grouped), groupKeys(grouped))
		}

		// agent-1 should have 2 alerts (firing critical + acknowledged critical).
		agent1Alerts := grouped["agent-1"]
		if len(agent1Alerts) != 2 {
			t.Errorf("expected 2 agent-1 alerts, got %d", len(agent1Alerts))
		}
	})

	t.Run("invalid groupBy returns error", func(t *testing.T) {
		_, err := ops.GetActiveAlertsByGroup(ctx, tenant, "invalid_field")
		if err == nil {
			t.Error("expected error for invalid groupBy, got nil")
		}
		if !strings.Contains(err.Error(), "unsupported groupBy") {
			t.Errorf("expected 'unsupported groupBy' in error, got: %v", err)
		}
	})
}

func TestAlertOps_Fingerprint_Deterministic(t *testing.T) {
	// Verify that the same alert always produces the same fingerprint.
	alert := &models.Alert{
		Name:     "high_cpu",
		Severity: models.AlertSeverityCritical,
		Labels:   map[string]string{"host": "web-1", "env": "prod"},
	}

	fp1 := Fingerprint(alert)
	fp2 := Fingerprint(alert)

	if fp1 != fp2 {
		t.Errorf("fingerprint should be deterministic: %q != %q", fp1, fp2)
	}

	// Verify different labels produce different fingerprints.
	alert2 := &models.Alert{
		Name:     "high_cpu",
		Severity: models.AlertSeverityCritical,
		Labels:   map[string]string{"host": "web-2", "env": "prod"},
	}

	fp3 := Fingerprint(alert2)
	if fp1 == fp3 {
		t.Error("different labels should produce different fingerprints")
	}

	// Verify different severity produces different fingerprint.
	alert3 := &models.Alert{
		Name:     "high_cpu",
		Severity: models.AlertSeverityWarning,
		Labels:   map[string]string{"host": "web-1", "env": "prod"},
	}

	fp4 := Fingerprint(alert3)
	if fp1 == fp4 {
		t.Error("different severity should produce different fingerprint")
	}
}

func TestAlertOps_KeyGeneration(t *testing.T) {
	tests := []struct {
		name   string
		fn     func() string
		expect string
	}{
		{
			name:   "alertStateKey",
			fn:     func() string { return alertStateKey("t1", "a1") },
			expect: "paryty:t1:alerts:state:a1",
		},
		{
			name:   "alertMetaKey",
			fn:     func() string { return alertMetaKey("t1", "a1") },
			expect: "paryty:t1:alerts:meta:a1",
		},
		{
			name:   "alertHistoryKey",
			fn:     func() string { return alertHistoryKey("t1", "a1") },
			expect: "paryty:t1:alerts:history:a1",
		},
		{
			name:   "alertFingerprintKey",
			fn:     func() string { return alertFingerprintKey("t1", "abc123") },
			expect: "paryty:t1:alerts:fingerprint:abc123",
		},
		{
			name:   "alertStatePattern",
			fn:     func() string { return alertStatePattern("t1") },
			expect: "paryty:t1:alerts:state:*",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.fn()
			if got != tc.expect {
				t.Errorf("%s = %q, want %q", tc.name, got, tc.expect)
			}
		})
	}
}

func TestAlertOps_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	m, ops := newTestAlertOps()

	// Store the same alert ID for two different tenants.
	seedAlertState(m, "tenant-a", "shared-id", AlertStateFiring)
	seedAlertState(m, "tenant-b", "shared-id", AlertStateAcknowledged)

	// Each tenant should see their own state.
	stateA, err := ops.GetAlertState(ctx, "tenant-a", "shared-id")
	if err != nil {
		t.Fatalf("GetAlertState(tenant-a) error: %v", err)
	}
	stateB, err := ops.GetAlertState(ctx, "tenant-b", "shared-id")
	if err != nil {
		t.Fatalf("GetAlertState(tenant-b) error: %v", err)
	}

	if stateA != AlertStateFiring {
		t.Errorf("tenant-a state = %q, want %q", stateA, AlertStateFiring)
	}
	if stateB != AlertStateAcknowledged {
		t.Errorf("tenant-b state = %q, want %q", stateB, AlertStateAcknowledged)
	}

	// Transitioning one tenant's alert should not affect the other.
	err = ops.TransitionAlert(ctx, "tenant-a", "shared-id", AlertStateResolved, "cleared")
	if err != nil {
		t.Fatalf("TransitionAlert(tenant-a) error: %v", err)
	}

	stateB, err = ops.GetAlertState(ctx, "tenant-b", "shared-id")
	if err != nil {
		t.Fatalf("GetAlertState(tenant-b) error: %v", err)
	}
	if stateB != AlertStateAcknowledged {
		t.Errorf("tenant-b state changed after tenant-a transition: got %q, want %q",
			stateB, AlertStateAcknowledged)
	}
}

func TestAlertOps_ValidateInputs(t *testing.T) {
	ctx := context.Background()
	_, ops := newTestAlertOps()

	t.Run("empty tenant", func(t *testing.T) {
		err := ops.TransitionAlert(ctx, "", "alert-1", AlertStateFiring, "test")
		if err == nil {
			t.Error("expected error for empty tenant")
		}
	})

	t.Run("empty alertID", func(t *testing.T) {
		err := ops.TransitionAlert(ctx, "tenant-1", "", AlertStateFiring, "test")
		if err == nil {
			t.Error("expected error for empty alertID")
		}
	})

	t.Run("invalid state", func(t *testing.T) {
		err := ops.TransitionAlert(ctx, "tenant-1", "alert-1", AlertState("bogus"), "test")
		if err == nil || !strings.Contains(err.Error(), "invalid alert state") {
			t.Errorf("expected 'invalid alert state' error, got: %v", err)
		}
	})

	t.Run("nil alert for dedup", func(t *testing.T) {
		_, err := ops.DeduplicateAlert(ctx, "tenant-1", nil)
		if err == nil {
			t.Error("expected error for nil alert")
		}
	})

	t.Run("empty groupBy", func(t *testing.T) {
		_, err := ops.GetActiveAlertsByGroup(ctx, "tenant-1", "")
		if err == nil {
			t.Error("expected error for empty groupBy")
		}
	})
}

func TestAlertOps_AlreadyInState(t *testing.T) {
	ctx := context.Background()
	m, ops := newTestAlertOps()
	tenant := "test-tenant"
	alertID := "alert-same-state"

	seedAlertState(m, tenant, alertID, AlertStateFiring)

	// Transitioning to the same state should succeed (idempotent).
	err := ops.TransitionAlert(ctx, tenant, alertID, AlertStateFiring, "no-op")
	if err != nil {
		t.Fatalf("TransitionAlert to same state should succeed: %v", err)
	}

	// State should remain unchanged.
	state, err := ops.GetAlertState(ctx, tenant, alertID)
	if err != nil {
		t.Fatalf("GetAlertState error: %v", err)
	}
	if state != AlertStateFiring {
		t.Errorf("state = %q, want %q", state, AlertStateFiring)
	}
}

func TestAlertOps_NonExistentAlert(t *testing.T) {
	ctx := context.Background()
	_, ops := newTestAlertOps()

	// Transitioning a non-existent alert should fail (current state unknown).
	err := ops.TransitionAlert(ctx, "tenant-1", "nonexistent", AlertStateFiring, "test")
	if err == nil {
		t.Error("expected error for non-existent alert")
	}
	if !errors.Is(err, redis.Nil) {
		// The error should wrap redis.Nil for missing state.
		t.Logf("error type: %v", err)
	}
}

// groupKeys extracts map keys for test failure messages.
func groupKeys(m map[string][]models.Alert) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
