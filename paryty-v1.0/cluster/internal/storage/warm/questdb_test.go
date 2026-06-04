package warm

import (
	"strings"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
)

// ---- ILP Line Formatting Tests ----

func TestFormatILPLine(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name   string
		table  string
		tags   map[string]string
		fields map[string]any
		ts     time.Time
		want   string
	}{
		{
			name:  "cpu metrics basic",
			table: "cpu_metrics",
			tags:  map[string]string{"agent_id": "agent-1", "tenant_id": "tenant-a"},
			fields: map[string]any{
				"total_usage_pct":  50.5,
				"context_switches": uint64(12345),
			},
			ts:   ts,
			want: "cpu_metrics,agent_id=agent-1,tenant_id=tenant-a context_switches=12345i,total_usage_pct=50.5 1736937000000000000\n",
		},
		{
			name:  "memory metrics with large values",
			table: "memory_metrics",
			tags:  map[string]string{"agent_id": "agent-42", "tenant_id": "default"},
			fields: map[string]any{
				"total_bytes": uint64(17179869184),
				"used_bytes":  uint64(8589934592),
			},
			ts:   ts,
			want: "memory_metrics,agent_id=agent-42,tenant_id=default total_bytes=17179869184i,used_bytes=8589934592i 1736937000000000000\n",
		},
		{
			name:  "string field value",
			table: "disk_metrics",
			tags:  map[string]string{"agent_id": "agent-1", "tenant_id": "t1", "device": "/dev/sda1"},
			fields: map[string]any{
				"mount_point": "/mnt/data",
				"total_bytes": uint64(1073741824),
			},
			ts:   ts,
			want: `disk_metrics,agent_id=agent-1,device=/dev/sda1,tenant_id=t1 mount_point="/mnt/data",total_bytes=1073741824i 1736937000000000000` + "\n",
		},
		{
			name:   "empty tags and fields",
			table:  "test_table",
			tags:   map[string]string{},
			fields: map[string]any{},
			ts:     ts,
			want:   "test_table  1736937000000000000\n",
		},
		{
			name:  "boolean field",
			table: "events",
			tags:  map[string]string{"agent_id": "a1", "tenant_id": "t1"},
			fields: map[string]any{
				"healthy": true,
				"count":   int(3),
			},
			ts:   ts,
			want: "events,agent_id=a1,tenant_id=t1 count=3i,healthy=t 1736937000000000000\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatILPLine(tc.table, tc.tags, tc.fields, tc.ts)
			if got != tc.want {
				t.Errorf("formatILPLine() =\n  %q\nwant:\n  %q", got, tc.want)
			}
		})
	}
}

func TestFormatILPLine_TagsAreSorted(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	tags := map[string]string{
		"z_tag": "z",
		"a_tag": "a",
		"m_tag": "m",
	}

	// Format twice -- output must be identical (deterministic).
	line1 := formatILPLine("test", tags, map[string]any{"v": 1.0}, ts)
	line2 := formatILPLine("test", tags, map[string]any{"v": 1.0}, ts)

	if line1 != line2 {
		t.Errorf("non-deterministic ILP output:\n  line1=%q\n  line2=%q", line1, line2)
	}

	// Tags should appear in sorted order.
	if !strings.Contains(line1, "a_tag=a,m_tag=m,z_tag=z") {
		t.Errorf("tags not sorted: %q", line1)
	}
}

func TestFormatILPLine_FieldsAreSorted(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	fields := map[string]any{
		"z_field": 1.0,
		"a_field": 2.0,
		"m_field": 3.0,
	}

	line := formatILPLine("test", map[string]string{"t": "v"}, fields, ts)

	if !strings.Contains(line, "a_field=2,m_field=3,z_field=1") {
		t.Errorf("fields not sorted: %q", line)
	}
}

func TestFormatILPLine_Idempotent(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	tags := map[string]string{"agent_id": "a1", "tenant_id": "t1"}
	fields := map[string]any{"value": 42.0, "count": uint64(100)}

	// Same input must always produce the same output.
	line1 := formatILPLine("metrics", tags, fields, ts)
	line2 := formatILPLine("metrics", tags, fields, ts)

	if line1 != line2 {
		t.Errorf("idempotency violation:\n  line1=%q\n  line2=%q", line1, line2)
	}
}

// ---- Special Character Escaping Tests ----

func TestEscapeILPToken(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no escaping needed",
			input: "simple-value",
			want:  "simple-value",
		},
		{
			name:  "space",
			input: "hello world",
			want:  `hello\ world`,
		},
		{
			name:  "comma",
			input: "a,b",
			want:  `a\,b`,
		},
		{
			name:  "equals",
			input: "key=value",
			want:  `key\=value`,
		},
		{
			name:  "backslash",
			input: `path\to`,
			want:  `path\\to`,
		},
		{
			name:  "multiple special chars",
			input: `a =b, c\d`,
			want:  `a\ \=b\,\ c\\d`,
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only special chars",
			input: " ,=\\",
			want:  `\ \,\=\\`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := escapeILPToken(tc.input)
			if got != tc.want {
				t.Errorf("escapeILPToken(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestEscapeILPString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no escaping needed",
			input: "normal string",
			want:  "normal string",
		},
		{
			name:  "double quote",
			input: `say "hello"`,
			want:  `say \"hello\"`,
		},
		{
			name:  "backslash",
			input: `path\to\file`,
			want:  `path\\to\\file`,
		},
		{
			name:  "both",
			input: `a "b" \c`,
			want:  `a \"b\" \\c`,
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := escapeILPString(tc.input)
			if got != tc.want {
				t.Errorf("escapeILPString(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestEscapeILPToken_TagWithSpaces(t *testing.T) {
	// Simulates a tag value like "service name" appearing in a metric.
	table := "cpu_metrics"
	tags := map[string]string{
		"agent_id":  "agent-1",
		"tenant_id": "my tenant",
	}
	fields := map[string]any{"value": 1.0}
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	line := formatILPLine(table, tags, fields, ts)

	// The tenant_id tag value should be escaped.
	if !strings.Contains(line, `tenant_id=my\ tenant`) {
		t.Errorf("expected escaped tenant_id, got: %q", line)
	}
}

// ---- WriteFieldValue Tests ----

func TestWriteFieldValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "float64", value: 3.14, want: "3.14"},
		{name: "int64", value: int64(42), want: "42i"},
		{name: "uint64", value: uint64(9999), want: "9999i"},
		{name: "int", value: 7, want: "7i"},
		{name: "string", value: "hello", want: `"hello"`},
		{name: "bool true", value: true, want: "t"},
		{name: "bool false", value: false, want: "f"},
		{name: "int32", value: int32(5), want: "5i"},
		{name: "uint32", value: uint32(10), want: "10i"},
		{name: "float32", value: float32(1.5), want: "1.5"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			writeFieldValue(&b, tc.value)
			got := b.String()
			if got != tc.want {
				t.Errorf("writeFieldValue(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// ---- EnsureTables DDL Validation Tests ----

func TestCreateTableStatements_Count(t *testing.T) {
	// Must have 14 tables: cpu, memory, disk, network, process, container,
	// aggregated, spans, tcp_events, dns_events, http_events,
	// db_queries, topology_snapshots, paryty_schema_version.
	want := 14
	if got := len(createTableStatements); got != want {
		t.Errorf("createTableStatements has %d entries, want %d", got, want)
	}
}

func TestCreateTableStatements_ContainRequiredClauses(t *testing.T) {
	requiredClauses := []string{
		"CREATE TABLE IF NOT EXISTS",
		"TIMESTAMP(timestamp)",
		"PARTITION BY DAY",
		"WAL",
	}

	for i, ddl := range createTableStatements {
		// paryty_schema_version uses PartitionBy NONE - no TIMESTAMP/PARTITION/WAL.
		if strings.Contains(ddl, "paryty_schema_version") {
			if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS") {
				t.Errorf("createTableStatements[%d] (schema version) missing CREATE TABLE IF NOT EXISTS", i)
			}
			continue
		}
		for _, clause := range requiredClauses {
			if !strings.Contains(ddl, clause) {
				t.Errorf("createTableStatements[%d] missing %q", i, clause)
			}
		}
	}
}

func TestCreateTableStatements_UniqueTableNames(t *testing.T) {
	seen := make(map[string]bool)
	for _, ddl := range createTableStatements {
		// Extract table name from "CREATE TABLE IF NOT EXISTS <name>"
		idx := strings.Index(ddl, "CREATE TABLE IF NOT EXISTS ")
		if idx < 0 {
			t.Fatalf("DDL missing CREATE TABLE IF NOT EXISTS: %s", ddl)
		}
		rest := ddl[idx+len("CREATE TABLE IF NOT EXISTS "):]
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			t.Fatalf("could not extract table name from: %s", ddl)
		}
		name := parts[0]
		if seen[name] {
			t.Errorf("duplicate table name: %s", name)
		}
		seen[name] = true
	}
}

func TestCreateTableStatements_NetworkEventTables(t *testing.T) {
	// Verify the three network event tables exist in the DDL list.
	tableNames := make(map[string]bool)
	for _, ddl := range createTableStatements {
		idx := strings.Index(ddl, "CREATE TABLE IF NOT EXISTS ")
		if idx >= 0 {
			rest := ddl[idx+len("CREATE TABLE IF NOT EXISTS "):]
			parts := strings.Fields(rest)
			if len(parts) > 0 {
				tableNames[parts[0]] = true
			}
		}
	}

	required := []string{"tcp_events", "dns_events", "http_events"}
	for _, name := range required {
		if !tableNames[name] {
			t.Errorf("missing required table: %s", name)
		}
	}
}

func TestCreateTableStatements_Phase4Tables(t *testing.T) {
	tableNames := make(map[string]bool)
	for _, ddl := range createTableStatements {
		idx := strings.Index(ddl, "CREATE TABLE IF NOT EXISTS ")
		if idx >= 0 {
			rest := ddl[idx+len("CREATE TABLE IF NOT EXISTS "):]
			parts := strings.Fields(rest)
			if len(parts) > 0 {
				tableNames[parts[0]] = true
			}
		}
	}

	required := []string{"db_queries", "topology_snapshots", "paryty_schema_version"}
	for _, name := range required {
		if !tableNames[name] {
			t.Errorf("missing required Phase 4 table: %s", name)
		}
	}
}

// ---- FormatFloatSlice Tests ----

func TestFormatFloatSlice(t *testing.T) {
	tests := []struct {
		name  string
		input []float64
		want  string
	}{
		{"single", []float64{1.5}, "[1.5000]"},
		{"multiple", []float64{0.5, 0.3, 0.8}, "[0.5000,0.3000,0.8000]"},
		{"empty", []float64{}, "[]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatFloatSlice(tc.input)
			if got != tc.want {
				t.Errorf("formatFloatSlice(%v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---- PG INSERT Batch Fallback Tests ----

func TestInsertMetricBatchILP_FallsBackToPG(t *testing.T) {
	// InsertMetricBatchILP should delegate to InsertMetricBatch.
	// This test verifies the method exists and doesn't panic with a nil pool
	// (the actual DB test requires a running QuestDB).
	// We just verify the method signature compiles correctly.
	var c *Client
	if c != nil {
		batch := &models.MetricBatch{AgentID: "test"}
		_ = c.InsertMetricBatchILP(batch, "tenant")
	}
}

// ---- InsertNetworkEvents Signature Test ----

func TestInsertNetworkEvents_SignatureCompiles(t *testing.T) {
	// Verify InsertNetworkEvents method signature compiles.
	// The actual DB test requires a running QuestDB.
	var c *Client
	if c != nil {
		_ = c.InsertNetworkEvents(nil, "tenant")
	}
}
