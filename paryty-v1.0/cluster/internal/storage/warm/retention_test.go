package warm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
)

// ---- DefaultRetentionRules Tests ----

func TestDefaultRetentionRules(t *testing.T) {
	expectedTables := map[string]bool{
		"cpu_metrics":        true,
		"memory_metrics":     true,
		"disk_metrics":       true,
		"network_metrics":    true,
		"process_metrics":    true,
		"container_metrics":  true,
		"aggregated_metrics": true,
		"spans":              true,
		"db_queries":         true,
		"topology_snapshots": true,
	}

	if got := len(DefaultRetentionRules); got != len(expectedTables) {
		t.Fatalf("DefaultRetentionRules has %d entries, want %d", got, len(expectedTables))
	}

	seen := make(map[string]bool)
	for _, rule := range DefaultRetentionRules {
		seen[rule.Table] = true

		if rule.MaxAge <= 0 {
			t.Errorf("table %s: MaxAge must be positive, got %v", rule.Table, rule.MaxAge)
		}
		if rule.MaxAge < 24*time.Hour {
			t.Errorf("table %s: MaxAge must be at least 1 day, got %v", rule.Table, rule.MaxAge)
		}
		if rule.Table == "" {
			t.Error("table name must not be empty")
		}
	}

	for table := range expectedTables {
		if !seen[table] {
			t.Errorf("missing retention rule for table: %s", table)
		}
	}
}

func TestDefaultRetentionRules_ArchiveFlags(t *testing.T) {
	expected := map[string]bool{
		"cpu_metrics":        true,
		"memory_metrics":     true,
		"disk_metrics":       true,
		"network_metrics":    true,
		"process_metrics":    false,
		"container_metrics":  false,
		"aggregated_metrics": true,
		"spans":              true,
		"db_queries":         true,
		"topology_snapshots": true,
	}

	for _, rule := range DefaultRetentionRules {
		wantArchive, ok := expected[rule.Table]
		if !ok {
			continue
		}
		if rule.ArchiveToCold != wantArchive {
			t.Errorf("table %s: ArchiveToCold = %v, want %v", rule.Table, rule.ArchiveToCold, wantArchive)
		}
	}
}

func TestDefaultRetentionRules_NoArchivingWithoutColdStore(t *testing.T) {
	// Verify that tables with ArchiveToCold=false don't depend on cold storage.
	noArchive := []string{"process_metrics", "container_metrics"}
	for _, table := range noArchive {
		found := false
		for _, rule := range DefaultRetentionRules {
			if rule.Table == table {
				found = true
				if rule.ArchiveToCold {
					t.Errorf("table %s should not require archiving", table)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected table %s in DefaultRetentionRules", table)
		}
	}
}

// ---- retentionCutoff Tests ----

func TestRetentionCutoff(t *testing.T) {
	tests := []struct {
		name   string
		maxAge time.Duration
	}{
		{"30 days", 30 * 24 * time.Hour},
		{"7 days", 7 * 24 * time.Hour},
		{"14 days", 14 * 24 * time.Hour},
		{"90 days", 90 * 24 * time.Hour},
		{"1 hour", 1 * time.Hour},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cutoff := retentionCutoff(tc.maxAge)
			now := time.Now().UTC()

			if !cutoff.Before(now) {
				t.Errorf("retentionCutoff(%v) = %v, want before now (%v)", tc.maxAge, cutoff, now)
			}

			// Cutoff should be roughly maxAge in the past.
			// Allow 24h tolerance for day truncation.
			diff := now.Sub(cutoff)
			if diff < tc.maxAge || diff > tc.maxAge+24*time.Hour {
				t.Errorf("retentionCutoff(%v): diff from now = %v, want between %v and %v",
					tc.maxAge, diff, tc.maxAge, tc.maxAge+24*time.Hour)
			}
		})
	}
}

func TestRetentionCutoff_IsMidnightUTC(t *testing.T) {
	cutoff := retentionCutoff(30 * 24 * time.Hour)
	if cutoff.Hour() != 0 || cutoff.Minute() != 0 || cutoff.Second() != 0 || cutoff.Nanosecond() != 0 {
		t.Errorf("retentionCutoff should be at midnight UTC, got %v", cutoff)
	}
	if cutoff.Location() != time.UTC {
		t.Errorf("retentionCutoff should be UTC, got %v", cutoff.Location())
	}
}

func TestRetentionCutoff_ZeroAge(t *testing.T) {
	// Zero max age should return today's midnight.
	cutoff := retentionCutoff(0)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	if !cutoff.Equal(today) {
		t.Errorf("retentionCutoff(0) = %v, want %v", cutoff, today)
	}
}

// ---- partitionDate Tests ----

func TestPartitionDate(t *testing.T) {
	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{
			name: "standard date",
			time: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			want: "2024-01-15",
		},
		{
			name: "new year",
			time: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			want: "2025-01-01",
		},
		{
			name: "december 31",
			time: time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			want: "2024-12-31",
		},
		{
			name: "non-utc timezone converted to utc",
			time: time.Date(2024, 6, 15, 23, 30, 0, 0, time.FixedZone("EST", -5*3600)),
			want: "2024-06-16", // 23:30 EST = 04:30 UTC next day
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := partitionDate(tc.time)
			if got != tc.want {
				t.Errorf("partitionDate(%v) = %q, want %q", tc.time, got, tc.want)
			}
		})
	}
}

// ---- parsePartitionDate Tests ----

func TestParsePartitionDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:  "valid date",
			input: "2024-01-15",
			want:  time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "new year",
			input: "2025-01-01",
			want:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "invalid format",
			input:   "not-a-date",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "us format",
			input:   "01/15/2024",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePartitionDate(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parsePartitionDate(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePartitionDate(%q) unexpected error: %v", tc.input, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("parsePartitionDate(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParsePartitionDate_RoundTrip(t *testing.T) {
	// partitionDate followed by parsePartitionDate should round-trip to midnight UTC.
	original := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	formatted := partitionDate(original)
	parsed, err := parsePartitionDate(formatted)
	if err != nil {
		t.Fatalf("parsePartitionDate(%q) unexpected error: %v", formatted, err)
	}

	want := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	if !parsed.Equal(want) {
		t.Errorf("round-trip: got %v, want %v", parsed, want)
	}
}

// ---- partitionsToDrop Tests ----

func TestPartitionsToDrop(t *testing.T) {
	cutoff := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		partitions []string
		cutoff     time.Time
		want       []string
	}{
		{
			name:       "nil list",
			partitions: nil,
			cutoff:     cutoff,
			want:       nil,
		},
		{
			name:       "empty list",
			partitions: []string{},
			cutoff:     cutoff,
			want:       nil,
		},
		{
			name:       "single partition never dropped",
			partitions: []string{"2024-01-01"},
			cutoff:     cutoff,
			want:       nil,
		},
		{
			name:       "two partitions oldest before cutoff",
			partitions: []string{"2024-01-01", "2024-01-15"},
			cutoff:     cutoff,
			want:       []string{"2024-01-01"},
		},
		{
			name:       "all older than cutoff except most recent",
			partitions: []string{"2024-01-01", "2024-01-05", "2024-01-15"},
			cutoff:     cutoff,
			want:       []string{"2024-01-01", "2024-01-05"},
		},
		{
			name:       "none older than cutoff",
			partitions: []string{"2024-01-10", "2024-01-15"},
			cutoff:     cutoff,
			want:       nil,
		},
		{
			name:       "mixed ages",
			partitions: []string{"2024-01-01", "2024-01-10", "2024-01-15"},
			cutoff:     cutoff,
			want:       []string{"2024-01-01"},
		},
		{
			name:       "unsorted input",
			partitions: []string{"2024-01-15", "2024-01-01", "2024-01-05"},
			cutoff:     cutoff,
			want:       []string{"2024-01-01", "2024-01-05"},
		},
		{
			name:       "all same date",
			partitions: []string{"2024-01-15", "2024-01-15"},
			cutoff:     cutoff,
			want:       nil, // Only one unique partition (most recent), never dropped.
		},
		{
			name:       "exact cutoff boundary not dropped",
			partitions: []string{"2024-01-10", "2024-01-15"},
			cutoff:     cutoff,
			want:       nil, // "2024-01-10" is equal to cutoff, not before.
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := partitionsToDrop(tc.partitions, tc.cutoff)
			if len(got) != len(tc.want) {
				t.Fatalf("partitionsToDrop(%v, %v) = %v (len=%d), want %v (len=%d)",
					tc.partitions, tc.cutoff, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("partitionsToDrop[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestPartitionsToDrop_NeverDropsMostRecent(t *testing.T) {
	// Even if the most recent partition is older than the cutoff,
	// it must never be dropped.
	cutoff := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	partitions := []string{"2024-01-01", "2024-06-15", "2025-01-01"}

	got := partitionsToDrop(partitions, cutoff)
	for _, p := range got {
		if p == "2025-01-01" {
			t.Fatal("partitionsToDrop must never drop the most recent partition")
		}
	}

	// All three are before cutoff, but only the first two should be dropped.
	want := []string{"2024-01-01", "2024-06-15"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// ---- SQL Generation Tests ----

func TestDropPartitionQuery(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		partition string
		want      string
	}{
		{
			name:      "cpu metrics",
			table:     "cpu_metrics",
			partition: "2024-01-15",
			want:      "ALTER TABLE cpu_metrics DROP PARTITION LIST '2024-01-15'",
		},
		{
			name:      "spans",
			table:     "spans",
			partition: "2023-12-31",
			want:      "ALTER TABLE spans DROP PARTITION LIST '2023-12-31'",
		},
		{
			name:      "aggregated metrics",
			table:     "aggregated_metrics",
			partition: "2024-06-01",
			want:      "ALTER TABLE aggregated_metrics DROP PARTITION LIST '2024-06-01'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dropPartitionQuery(tc.table, tc.partition)
			if got != tc.want {
				t.Errorf("dropPartitionQuery(%q, %q) = %q, want %q",
					tc.table, tc.partition, got, tc.want)
			}
		})
	}
}

func TestListPartitionsQuery(t *testing.T) {
	tests := []struct {
		table string
		want  string
	}{
		{
			table: "cpu_metrics",
			want:  "SELECT DISTINCT cast(timestamp as date) FROM cpu_metrics ORDER BY 1",
		},
		{
			table: "aggregated_metrics",
			want:  "SELECT DISTINCT cast(timestamp as date) FROM aggregated_metrics ORDER BY 1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.table, func(t *testing.T) {
			got := listPartitionsQuery(tc.table)
			if got != tc.want {
				t.Errorf("listPartitionsQuery(%q) = %q, want %q", tc.table, got, tc.want)
			}
		})
	}
}

// ---- ColdArchiver Mock Tests ----

// mockColdArchiver records ArchiveTable calls for test verification.
type mockColdArchiver struct {
	calls []coldArchiveCall
	err   error
}

type coldArchiveCall struct {
	table  string
	before time.Time
}

func (m *mockColdArchiver) ArchiveTable(ctx context.Context, table string, before time.Time) error {
	m.calls = append(m.calls, coldArchiveCall{table: table, before: before})
	return m.err
}

func TestArchiveTableData_NilColdStore(t *testing.T) {
	rm := &RetentionManager{
		coldStore: nil,
		logger:    zap.NewNop(),
	}

	err := rm.ArchiveTableData(context.Background(), "cpu_metrics", time.Now())
	if err != nil {
		t.Errorf("ArchiveTableData with nil coldStore should return nil, got: %v", err)
	}
}

func TestArchiveTableData_CallsArchiver(t *testing.T) {
	mock := &mockColdArchiver{}
	before := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	rm := &RetentionManager{
		coldStore: mock,
		logger:    zap.NewNop(),
	}

	err := rm.ArchiveTableData(context.Background(), "cpu_metrics", before)
	if err != nil {
		t.Fatalf("ArchiveTableData returned error: %v", err)
	}

	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.calls))
	}

	call := mock.calls[0]
	if call.table != "cpu_metrics" {
		t.Errorf("call.table = %q, want %q", call.table, "cpu_metrics")
	}
	if !call.before.Equal(before) {
		t.Errorf("call.before = %v, want %v", call.before, before)
	}
}

func TestArchiveTableData_PropagatesError(t *testing.T) {
	mock := &mockColdArchiver{err: fmt.Errorf("cold storage unavailable")}

	rm := &RetentionManager{
		coldStore: mock,
		logger:    zap.NewNop(),
	}

	err := rm.ArchiveTableData(context.Background(), "cpu_metrics", time.Now())
	if err == nil {
		t.Error("ArchiveTableData should propagate cold archiver error")
	}
}

func TestArchiveTableData_MultipleCalls(t *testing.T) {
	mock := &mockColdArchiver{}
	rm := &RetentionManager{
		coldStore: mock,
		logger:    zap.NewNop(),
	}

	tables := []string{"cpu_metrics", "memory_metrics", "spans"}
	for _, table := range tables {
		if err := rm.ArchiveTableData(context.Background(), table, time.Now()); err != nil {
			t.Fatalf("ArchiveTableData(%q) error: %v", table, err)
		}
	}

	if len(mock.calls) != len(tables) {
		t.Fatalf("expected %d calls, got %d", len(tables), len(mock.calls))
	}
	for i, table := range tables {
		if mock.calls[i].table != table {
			t.Errorf("call[%d].table = %q, want %q", i, mock.calls[i].table, table)
		}
	}
}

// ---- RetentionManager Constructor Tests ----

func TestNewRetentionManager_DefaultRules(t *testing.T) {
	rm := NewRetentionManager(nil, nil, nil, zap.NewNop())
	if len(rm.rules) != len(DefaultRetentionRules) {
		t.Errorf("expected %d default rules, got %d", len(DefaultRetentionRules), len(rm.rules))
	}
}

func TestNewRetentionManager_EmptyRules(t *testing.T) {
	rm := NewRetentionManager(nil, []RetentionRule{}, nil, zap.NewNop())
	if len(rm.rules) != len(DefaultRetentionRules) {
		t.Errorf("empty rules should default to DefaultRetentionRules, got %d rules", len(rm.rules))
	}
}

func TestNewRetentionManager_CustomRules(t *testing.T) {
	custom := []RetentionRule{
		{Table: "custom_table", MaxAge: 24 * time.Hour, ArchiveToCold: false},
	}
	rm := NewRetentionManager(nil, custom, nil, zap.NewNop())
	if len(rm.rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rm.rules))
	}
	if rm.rules[0].Table != "custom_table" {
		t.Errorf("expected table %q, got %q", "custom_table", rm.rules[0].Table)
	}
}

func TestNewRetentionManager_PreservesColdStore(t *testing.T) {
	mock := &mockColdArchiver{}
	rm := NewRetentionManager(nil, nil, mock, zap.NewNop())
	if rm.coldStore != mock {
		t.Error("NewRetentionManager should preserve the coldStore reference")
	}
}

// ---- RunRetention Tests ----

func TestRunRetention_EmptyRules(t *testing.T) {
	rm := &RetentionManager{
		pool:      nil, // Not used since rules are empty.
		rules:     []RetentionRule{},
		coldStore: nil,
		logger:    zap.NewNop(),
	}

	err := rm.RunRetention(context.Background())
	if err != nil {
		t.Errorf("RunRetention with empty rules should return nil, got: %v", err)
	}
}

func TestRunRetention_NilPoolCompiles(t *testing.T) {
	// Verify RunRetention method signature compiles with the expected types.
	// Actual execution requires a running QuestDB instance.
	var rm *RetentionManager
	if rm != nil {
		_ = rm.RunRetention(context.Background())
	}
}

// ---- DropPartition / ListPartitions Signature Tests ----

func TestDropPartition_SignatureCompiles(t *testing.T) {
	// Verify DropPartition method signature compiles.
	// Actual execution requires a running QuestDB instance.
	var rm *RetentionManager
	if rm != nil {
		_ = rm.DropPartition(context.Background(), "cpu_metrics", "2024-01-15")
	}
}

func TestListPartitions_SignatureCompiles(t *testing.T) {
	// Verify ListPartitions method signature compiles.
	// Actual execution requires a running QuestDB instance.
	var rm *RetentionManager
	if rm != nil {
		_, _ = rm.ListPartitions(context.Background(), "cpu_metrics")
	}
}

// ---- Integration-Level Behavior Tests ----

// mockRetentionArchiver tracks the ordering of archive vs drop calls.
type mockRetentionArchiver struct {
	archiveCalls int
	err          error
}

func (m *mockRetentionArchiver) ArchiveTable(ctx context.Context, table string, before time.Time) error {
	m.archiveCalls++
	return m.err
}

func TestRunRetention_ArchiveBeforeDrop(t *testing.T) {
	// Verify that archiving is attempted before partition dropping.
	// With a nil pool and an archiver that returns an error, RunRetention
	// should fail on the archive step and never reach the drop step.
	archiver := &mockRetentionArchiver{err: fmt.Errorf("archive failed")}

	rules := []RetentionRule{
		{Table: "cpu_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
	}

	rm := &RetentionManager{
		pool:      nil, // Would panic if reached — it shouldn't.
		rules:     rules,
		coldStore: archiver,
		logger:    zap.NewNop(),
	}

	err := rm.RunRetention(context.Background())
	if err == nil {
		t.Error("RunRetention should return error when archiving fails")
	}

	// The archiver should have been called exactly once.
	if archiver.archiveCalls != 1 {
		t.Errorf("expected 1 archive call, got %d", archiver.archiveCalls)
	}

	// Verify the error message references the archive failure.
	if err != nil && !strContains(err.Error(), "archive") {
		t.Errorf("error should reference archive failure, got: %v", err)
	}
}

func TestRunRetention_SkipsArchiveWhenDisabled(t *testing.T) {
	// Rules with ArchiveToCold=false should not call the archiver,
	// even if coldStore is provided. The rule skips archiving, then
	// panics on ListPartitions (nil pool) — we recover to verify behavior.
	archiver := &mockRetentionArchiver{}

	rules := []RetentionRule{
		{Table: "process_metrics", MaxAge: 7 * 24 * time.Hour, ArchiveToCold: false},
	}

	rm := &RetentionManager{
		pool:      nil, // Will panic when ListPartitions is called.
		rules:     rules,
		coldStore: archiver,
		logger:    zap.NewNop(),
	}

	// Recover from the expected nil-pool panic.
	func() {
		defer func() {
			_ = recover() // Expected: nil pointer on pool.Query.
		}()
		_ = rm.RunRetention(context.Background())
	}()

	// The archiver should NOT have been called (ArchiveToCold=false).
	if archiver.archiveCalls != 0 {
		t.Errorf("expected 0 archive calls for ArchiveToCold=false, got %d", archiver.archiveCalls)
	}
}

func TestRunRetention_SkipsArchiveWhenColdStoreNil(t *testing.T) {
	// Even if ArchiveToCold=true, a nil coldStore should skip archiving.
	// The rule skips archiving, then panics on ListPartitions (nil pool).
	rules := []RetentionRule{
		{Table: "cpu_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
	}

	rm := &RetentionManager{
		pool:      nil,
		rules:     rules,
		coldStore: nil, // No archiver — should skip archive step.
		logger:    zap.NewNop(),
	}

	// Recover from the expected nil-pool panic after archive is skipped.
	var retentionErr error
	func() {
		defer func() {
			_ = recover() // Expected: nil pointer on pool.Query.
		}()
		retentionErr = rm.RunRetention(context.Background())
	}()

	// RunRetention should not have returned an archive-related error
	// because coldStore was nil (archive was skipped).
	if retentionErr != nil && strContains(retentionErr.Error(), "archive") {
		t.Errorf("should not attempt archiving with nil coldStore, got: %v", retentionErr)
	}
}

// ---- Helper ----

// strContains reports whether substr is within s.
// Used instead of strings.Contains to avoid importing strings for one call.
func strContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
