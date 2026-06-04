package hot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
)

// ---- Key generation tests ----

func TestConnectionKey(t *testing.T) {
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
			want:    "paryty:acme-corp:connections:agent-42",
		},
		{
			name:    "default tenant",
			tenant:  "default",
			agentID: "agent-1",
			want:    "paryty:default:connections:agent-1",
		},
		{
			name:    "tenant with special chars",
			tenant:  "org-123_test",
			agentID: "host-abc",
			want:    "paryty:org-123_test:connections:host-abc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := connectionKey(tc.tenant, tc.agentID)
			if got != tc.want {
				t.Errorf("connectionKey(%q, %q) = %q, want %q", tc.tenant, tc.agentID, got, tc.want)
			}
		})
	}
}

func TestConnectionPattern(t *testing.T) {
	got := connectionPattern("tenant-a")
	want := "paryty:tenant-a:connections:*"
	if got != want {
		t.Errorf("connectionPattern(%q) = %q, want %q", "tenant-a", got, want)
	}
}

func TestConnectionKey_TenantIsolation(t *testing.T) {
	agentID := "shared-agent"
	keyA := connectionKey("tenant-alpha", agentID)
	keyB := connectionKey("tenant-beta", agentID)
	if keyA == keyB {
		t.Errorf("same agent across different tenants produced colliding key %q", keyA)
	}
}

func TestConnectionKey_PrefixConsistency(t *testing.T) {
	key := connectionKey("any-tenant", "any-agent")
	const prefix = "paryty:"
	if len(key) < len(prefix) || key[:len(prefix)] != prefix {
		t.Errorf("connectionKey = %q does not start with %q", key, prefix)
	}
}

// ---- Integration tests ----
//
// These tests require a running Dragonfly/Redis instance on localhost:6379.
// They are skipped automatically when the instance is unavailable.

// testTenantPrefix returns a unique tenant prefix per test to prevent
// cross-test interference from stale keys.
func testTenantPrefix(name string) string {
	return fmt.Sprintf("test-conn-%s-%d", name, time.Now().UnixNano())
}

// newTestConnectionOps creates a ConnectionOps backed by a real Redis connection.
// Skips the test if Redis is not available on localhost:6379.
func newTestConnectionOps(t *testing.T) *ConnectionOps {
	t.Helper()

	client := New(Config{
		Addr: "localhost:6379",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		client.Close()
		t.Skipf("Dragonfly/Redis not available on localhost:6379: %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })

	logger := zap.NewNop()
	return NewConnectionOps(client, logger)
}

// cleanupConnection removes a connection key, ignoring errors.
func cleanupConnection(co *ConnectionOps, tenant, agentID string) {
	_ = co.RemoveConnection(context.Background(), tenant, agentID)
}

func TestConnectionOps_TrackAndRetrieve(t *testing.T) {
	co := newTestConnectionOps(t)
	ctx := context.Background()
	tenant := testTenantPrefix("track")
	agentID := "agent-tr-1"

	t.Cleanup(func() { cleanupConnection(co, tenant, agentID) })

	info := &ConnectionInfo{
		AgentID:       agentID,
		SessionID:     "session-abc",
		RemoteAddr:    "10.0.0.1:54321",
		ConnectedAt:   time.Now().UTC(),
		LastHeartbeat: time.Now().UTC(),
		Capabilities:  []string{"metrics", "traces"},
	}

	// Track the connection.
	if err := co.TrackConnection(ctx, tenant, info); err != nil {
		t.Fatalf("TrackConnection: %v", err)
	}

	// Retrieve via GetActiveConnections.
	conns, err := co.GetActiveConnections(ctx, tenant)
	if err != nil {
		t.Fatalf("GetActiveConnections: %v", err)
	}

	var found *ConnectionInfo
	for i := range conns {
		if conns[i].AgentID == agentID {
			found = &conns[i]
			break
		}
	}

	if found == nil {
		t.Fatalf("agent %s not found in active connections", agentID)
	}

	if found.SessionID != "session-abc" {
		t.Errorf("session_id = %q, want %q", found.SessionID, "session-abc")
	}
	if found.RemoteAddr != "10.0.0.1:54321" {
		t.Errorf("remote_addr = %q, want %q", found.RemoteAddr, "10.0.0.1:54321")
	}
	if found.TenantID != tenant {
		t.Errorf("tenant_id = %q, want %q", found.TenantID, tenant)
	}
	if len(found.Capabilities) != 2 {
		t.Errorf("capabilities count = %d, want 2", len(found.Capabilities))
	}
}

func TestConnectionOps_RefreshHeartbeat(t *testing.T) {
	co := newTestConnectionOps(t)
	ctx := context.Background()
	tenant := testTenantPrefix("refresh")
	agentID := "agent-hb-1"

	t.Cleanup(func() { cleanupConnection(co, tenant, agentID) })

	// Set the original heartbeat 5 minutes in the past.
	originalHB := time.Now().UTC().Add(-5 * time.Minute)
	info := &ConnectionInfo{
		AgentID:       agentID,
		SessionID:     "session-hb",
		RemoteAddr:    "10.0.0.2:12345",
		ConnectedAt:   time.Now().UTC().Add(-5 * time.Minute),
		LastHeartbeat: originalHB,
		Capabilities:  []string{"metrics"},
	}

	if err := co.TrackConnection(ctx, tenant, info); err != nil {
		t.Fatalf("TrackConnection: %v", err)
	}

	// Refresh the heartbeat.
	if err := co.RefreshHeartbeat(ctx, tenant, agentID); err != nil {
		t.Fatalf("RefreshHeartbeat: %v", err)
	}

	// Verify the heartbeat was updated.
	conns, err := co.GetActiveConnections(ctx, tenant)
	if err != nil {
		t.Fatalf("GetActiveConnections: %v", err)
	}

	for _, c := range conns {
		if c.AgentID == agentID {
			// The heartbeat should be recent (within last 2 seconds).
			if time.Since(c.LastHeartbeat) > 2*time.Second {
				t.Errorf("last_heartbeat = %v, expected recent (within 2s)", c.LastHeartbeat)
			}
			// The heartbeat should be strictly after the original.
			if !c.LastHeartbeat.After(originalHB) {
				t.Errorf("last_heartbeat %v should be after original %v", c.LastHeartbeat, originalHB)
			}
			return
		}
	}

	t.Errorf("agent %s not found after heartbeat refresh", agentID)
}

func TestConnectionOps_DetectDisconnected(t *testing.T) {
	co := newTestConnectionOps(t)
	ctx := context.Background()
	tenant := testTenantPrefix("detect")

	agentFresh := "agent-dd-fresh"
	agentStale1 := "agent-dd-stale1"
	agentStale2 := "agent-dd-stale2"

	t.Cleanup(func() {
		cleanupConnection(co, tenant, agentFresh)
		cleanupConnection(co, tenant, agentStale1)
		cleanupConnection(co, tenant, agentStale2)
	})

	now := time.Now().UTC()

	// Track 3 agents: one fresh, two stale (heartbeat > 60s ago).
	infos := []*ConnectionInfo{
		{
			AgentID:       agentStale1,
			SessionID:     "s-stale1",
			ConnectedAt:   now.Add(-10 * time.Minute),
			LastHeartbeat: now.Add(-5 * time.Minute),
			Capabilities:  []string{"metrics"},
		},
		{
			AgentID:       agentFresh,
			SessionID:     "s-fresh",
			ConnectedAt:   now.Add(-2 * time.Minute),
			LastHeartbeat: now.Add(-30 * time.Second),
			Capabilities:  []string{"traces"},
		},
		{
			AgentID:       agentStale2,
			SessionID:     "s-stale2",
			ConnectedAt:   now.Add(-8 * time.Minute),
			LastHeartbeat: now.Add(-3 * time.Minute),
			Capabilities:  []string{"logs"},
		},
	}

	for _, info := range infos {
		if err := co.TrackConnection(ctx, tenant, info); err != nil {
			t.Fatalf("TrackConnection(%s): %v", info.AgentID, err)
		}
	}

	// Detect with 60s threshold.
	disconnected, err := co.DetectDisconnectedAgents(ctx, tenant, 60*time.Second)
	if err != nil {
		t.Fatalf("DetectDisconnectedAgents: %v", err)
	}

	disconnectedIDs := make(map[string]bool, len(disconnected))
	for _, d := range disconnected {
		disconnectedIDs[d.AgentID] = true
	}

	if !disconnectedIDs[agentStale1] {
		t.Errorf("agent %s should be disconnected (heartbeat 5min ago)", agentStale1)
	}
	if !disconnectedIDs[agentStale2] {
		t.Errorf("agent %s should be disconnected (heartbeat 3min ago)", agentStale2)
	}
	if disconnectedIDs[agentFresh] {
		t.Errorf("agent %s should NOT be disconnected (heartbeat 30s ago)", agentFresh)
	}
}

func TestConnectionOps_RemoveConnection(t *testing.T) {
	co := newTestConnectionOps(t)
	ctx := context.Background()
	tenant := testTenantPrefix("remove")
	agentID := "agent-rm-1"

	info := &ConnectionInfo{
		AgentID:       agentID,
		SessionID:     "session-rm",
		RemoteAddr:    "10.0.0.3:9999",
		ConnectedAt:   time.Now().UTC(),
		LastHeartbeat: time.Now().UTC(),
		Capabilities:  []string{"metrics", "logs"},
	}

	if err := co.TrackConnection(ctx, tenant, info); err != nil {
		t.Fatalf("TrackConnection: %v", err)
	}

	// Verify the connection exists.
	conns, err := co.GetActiveConnections(ctx, tenant)
	if err != nil {
		t.Fatalf("GetActiveConnections: %v", err)
	}

	found := false
	for _, c := range conns {
		if c.AgentID == agentID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("agent %s should exist before removal", agentID)
	}

	// Remove the connection.
	if err := co.RemoveConnection(ctx, tenant, agentID); err != nil {
		t.Fatalf("RemoveConnection: %v", err)
	}

	// Verify it is gone.
	conns, err = co.GetActiveConnections(ctx, tenant)
	if err != nil {
		t.Fatalf("GetActiveConnections after remove: %v", err)
	}

	for _, c := range conns {
		if c.AgentID == agentID {
			t.Errorf("agent %s should not exist after removal", agentID)
		}
	}
}

func TestConnectionOps_RemoveConnection_Idempotent(t *testing.T) {
	co := newTestConnectionOps(t)
	ctx := context.Background()
	tenant := testTenantPrefix("remove-idem")
	agentID := "agent-rm-idem"

	// Remove a connection that was never tracked — should not error.
	if err := co.RemoveConnection(ctx, tenant, agentID); err != nil {
		t.Fatalf("RemoveConnection on non-existent key: %v", err)
	}
}

func TestConnectionOps_TrackConnection_Validation(t *testing.T) {
	co := newTestConnectionOps(t)
	ctx := context.Background()

	tests := []struct {
		name string
		tenant string
		info   *ConnectionInfo
	}{
		{
			name:   "empty tenant",
			tenant: "",
			info:   &ConnectionInfo{AgentID: "agent-1"},
		},
		{
			name:   "nil info",
			tenant: "test-tenant",
			info:   nil,
		},
		{
			name:   "empty agent_id",
			tenant: "test-tenant",
			info:   &ConnectionInfo{AgentID: ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := co.TrackConnection(ctx, tc.tenant, tc.info)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestConnectionOps_RefreshHeartbeat_NotFound(t *testing.T) {
	co := newTestConnectionOps(t)
	ctx := context.Background()
	tenant := testTenantPrefix("refresh-nf")
	agentID := "agent-nf-1"

	// Refresh on a non-existent connection should return an error.
	err := co.RefreshHeartbeat(ctx, tenant, agentID)
	if err == nil {
		t.Error("expected error for non-existent connection, got nil")
	}
}
