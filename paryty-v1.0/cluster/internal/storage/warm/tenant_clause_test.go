package warm

import (
	"testing"
	"time"
)

// =============================================================================
// networkEventClause regression tests
//
// Pins the tenant-isolation contract for warm-tier network event queries
// (audit finding: reads lacked tenant filtering, allowing any authenticated
// tenant to read another tenant's events by guessing agent IDs).
// =============================================================================

func clauseTestWindow() (time.Time, time.Time) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	return start, start.Add(time.Hour)
}

func TestNetworkEventClause_TenantAndAgent(t *testing.T) {
	start, end := clauseTestWindow()
	clause, args := networkEventClause("tenant-a", "agent-1", start, end, 50)

	want := "WHERE timestamp >= $1 AND timestamp <= $2 AND tenant_id = $3 AND agent_id = $4 ORDER BY timestamp DESC LIMIT $5"
	if clause != want {
		t.Fatalf("clause mismatch:\n got: %s\nwant: %s", clause, want)
	}
	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d: %v", len(args), args)
	}
	if args[2] != "tenant-a" || args[3] != "agent-1" || args[4] != 50 {
		t.Fatalf("arg values wrong: %v", args)
	}
}

func TestNetworkEventClause_TenantOnly(t *testing.T) {
	start, end := clauseTestWindow()
	clause, args := networkEventClause("tenant-a", "", start, end, 100)

	want := "WHERE timestamp >= $1 AND timestamp <= $2 AND tenant_id = $3 ORDER BY timestamp DESC LIMIT $4"
	if clause != want {
		t.Fatalf("clause mismatch:\n got: %s\nwant: %s", clause, want)
	}
	if len(args) != 4 || args[2] != "tenant-a" {
		t.Fatalf("args wrong: %v", args)
	}
}

func TestNetworkEventClause_InternalWildcard(t *testing.T) {
	// Empty tenant is the documented internal-maintenance wildcard.
	start, end := clauseTestWindow()
	clause, args := networkEventClause("", "agent-1", start, end, 10)

	want := "WHERE timestamp >= $1 AND timestamp <= $2 AND agent_id = $3 ORDER BY timestamp DESC LIMIT $4"
	if clause != want {
		t.Fatalf("clause mismatch:\n got: %s\nwant: %s", clause, want)
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}
}

func TestNetworkEventClause_TenantIsParameterized(t *testing.T) {
	// The tenant value must be passed as a bind parameter, never
	// concatenated into SQL (injection defense).
	start, end := clauseTestWindow()
	malicious := "x'; DROP TABLE tcp_events; --"
	clause, args := networkEventClause(malicious, "", start, end, 10)

	for _, c := range []byte(clause) {
		if c == ';' {
			t.Fatalf("clause must not contain user input: %s", clause)
		}
	}
	if args[2] != malicious {
		t.Fatal("tenant must be passed verbatim as a bind parameter")
	}
}
