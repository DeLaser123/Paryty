// Package warm — Schema migration system tests.
//
// Tests validate DDL generation, schema definition correctness, and migration
// logic without requiring a running QuestDB instance.
package warm

import (
	"strings"
	"testing"

	"go.uber.org/zap"
)

// ---- DefaultSchema Tests ----

func TestDefaultSchema_TableCount(t *testing.T) {
	schema := DefaultSchema()

	// Must have 14 tables: 11 original + db_queries + topology_snapshots + paryty_schema_version.
	want := 14
	if got := len(schema.Tables); got != want {
		t.Errorf("DefaultSchema has %d tables, want %d", got, want)
	}
}

func TestDefaultSchema_ContainsAllOriginalTables(t *testing.T) {
	schema := DefaultSchema()

	required := []string{
		"cpu_metrics", "memory_metrics", "disk_metrics", "network_metrics",
		"process_metrics", "container_metrics", "aggregated_metrics", "spans",
		"tcp_events", "dns_events", "http_events",
	}

	tables := make(map[string]bool)
	for _, tbl := range schema.Tables {
		tables[tbl.Name] = true
	}

	for _, name := range required {
		if !tables[name] {
			t.Errorf("DefaultSchema missing required table: %s", name)
		}
	}
}

func TestDefaultSchema_ContainsPhase4Tables(t *testing.T) {
	schema := DefaultSchema()

	required := []string{"db_queries", "topology_snapshots", "paryty_schema_version"}
	tables := make(map[string]bool)
	for _, tbl := range schema.Tables {
		tables[tbl.Name] = true
	}

	for _, name := range required {
		if !tables[name] {
			t.Errorf("DefaultSchema missing Phase 4 table: %s", name)
		}
	}
}

func TestDefaultSchema_AllTablesHaveTimestampColumn(t *testing.T) {
	schema := DefaultSchema()

	for _, tbl := range schema.Tables {
		hasTimestamp := false
		for _, col := range tbl.Columns {
			if col.Type == ColTypeTimestamp {
				hasTimestamp = true
				break
			}
		}
		if !hasTimestamp {
			t.Errorf("table %s has no TIMESTAMP column", tbl.Name)
		}
	}
}

func TestDefaultSchema_AllPartitionedTablesHaveTenantID(t *testing.T) {
	schema := DefaultSchema()

	for _, tbl := range schema.Tables {
		if tbl.PartitionBy == PartitionByNone {
			continue // Skip non-partitioned tables.
		}
		if !tbl.HasColumn("tenant_id") {
			t.Errorf("partitioned table %s missing tenant_id column", tbl.Name)
		}
	}
}

// ---- Table Validation Tests ----

func TestValidateTableDefinition_Valid(t *testing.T) {
	def := TableDefinition{
		Name:        "test_table",
		PartitionBy: PartitionByDay,
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
			{Name: "agent_id", Type: ColTypeSymbol},
			{Name: "value", Type: ColTypeDouble},
		},
	}

	if err := ValidateTableDefinition(def); err != nil {
		t.Errorf("ValidateTableDefinition() returned error for valid table: %v", err)
	}
}

func TestValidateTableDefinition_EmptyName(t *testing.T) {
	def := TableDefinition{
		Name:        "",
		PartitionBy: PartitionByDay,
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
		},
	}

	if err := ValidateTableDefinition(def); err == nil {
		t.Error("ValidateTableDefinition() should return error for empty name")
	}
}

func TestValidateTableDefinition_EmptyColumns(t *testing.T) {
	def := TableDefinition{
		Name:        "test",
		PartitionBy: PartitionByDay,
		Columns:     []ColumnDefinition{},
	}

	if err := ValidateTableDefinition(def); err == nil {
		t.Error("ValidateTableDefinition() should return error for empty columns")
	}
}

func TestValidateTableDefinition_NoTimestampColumn(t *testing.T) {
	def := TableDefinition{
		Name:        "test",
		PartitionBy: PartitionByDay,
		Columns: []ColumnDefinition{
			{Name: "id", Type: ColTypeLong},
			{Name: "value", Type: ColTypeDouble},
		},
	}

	if err := ValidateTableDefinition(def); err == nil {
		t.Error("ValidateTableDefinition() should return error when first column is not TIMESTAMP")
	}
}

func TestValidateTableDefinition_InvalidPartitionBy(t *testing.T) {
	def := TableDefinition{
		Name:        "test",
		PartitionBy: "INVALID",
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
		},
	}

	if err := ValidateTableDefinition(def); err == nil {
		t.Error("ValidateTableDefinition() should return error for invalid partition strategy")
	}
}

func TestValidateTableDefinition_InvalidColumnType(t *testing.T) {
	def := TableDefinition{
		Name:        "test",
		PartitionBy: PartitionByDay,
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
			{Name: "value", Type: "INVALID_TYPE"},
		},
	}

	if err := ValidateTableDefinition(def); err == nil {
		t.Error("ValidateTableDefinition() should return error for invalid column type")
	}
}

func TestValidateTableDefinition_EmptyColumnName(t *testing.T) {
	def := TableDefinition{
		Name:        "test",
		PartitionBy: PartitionByDay,
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
			{Name: "", Type: ColTypeDouble},
		},
	}

	if err := ValidateTableDefinition(def); err == nil {
		t.Error("ValidateTableDefinition() should return error for empty column name")
	}
}

// ---- Schema Validation Tests ----

func TestValidateSchema_AllTablesValid(t *testing.T) {
	schema := DefaultSchema()
	for _, table := range schema.Tables {
		// NONE-partitioned tables (like paryty_schema_version) may not start
		// with TIMESTAMP — their first column can be any type.
		if table.PartitionBy == PartitionByNone {
			continue
		}
		if err := ValidateTableDefinition(table); err != nil {
			t.Errorf("table %s: %v", table.Name, err)
		}
	}
}

// ---- DDL Generation Tests ----

func TestBuildCreateTableDDL_BasicTable(t *testing.T) {
	def := TableDefinition{
		Name:        "test_metrics",
		PartitionBy: PartitionByDay,
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
			{Name: "agent_id", Type: ColTypeSymbol},
			{Name: "value", Type: ColTypeDouble},
		},
	}

	ddl := BuildCreateTableDDL(def)

	if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS test_metrics") {
		t.Errorf("DDL missing table name: %s", ddl)
	}
	if !strings.Contains(ddl, "TIMESTAMP(timestamp)") {
		t.Errorf("DDL missing TIMESTAMP clause: %s", ddl)
	}
	if !strings.Contains(ddl, "PARTITION BY DAY") {
		t.Errorf("DDL missing PARTITION BY DAY: %s", ddl)
	}
	if !strings.Contains(ddl, "WAL") {
		t.Errorf("DDL missing WAL clause: %s", ddl)
	}
}

func TestBuildCreateTableDDL_NonePartition(t *testing.T) {
	def := TableDefinition{
		Name:        "config",
		PartitionBy: PartitionByNone,
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
			{Name: "key", Type: ColTypeString},
			{Name: "value", Type: ColTypeString},
		},
	}

	ddl := BuildCreateTableDDL(def)

	if strings.Contains(ddl, "PARTITION BY") {
		t.Errorf("PartitionBy=NONE should not have PARTITION BY clause: %s", ddl)
	}
	if strings.Contains(ddl, "WAL") {
		t.Errorf("PartitionBy=NONE should not have WAL clause: %s", ddl)
	}
	if strings.Contains(ddl, "TIMESTAMP(timestamp)") {
		t.Errorf("PartitionBy=NONE should not have TIMESTAMP clause: %s", ddl)
	}
}

func TestBuildCreateTableDDL_ReservedWord(t *testing.T) {
	def := TableDefinition{
		Name:        "test",
		PartitionBy: PartitionByDay,
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
			{Name: "interface", Type: ColTypeSymbol},
		},
	}

	ddl := BuildCreateTableDDL(def)

	if !strings.Contains(ddl, `"interface"`) {
		t.Errorf("DDL should quote reserved word 'interface': %s", ddl)
	}
}

func TestBuildCreateTableDDL_HourPartition(t *testing.T) {
	def := TableDefinition{
		Name:        "high_volume",
		PartitionBy: PartitionByHour,
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
			{Name: "value", Type: ColTypeDouble},
		},
	}

	ddl := BuildCreateTableDDL(def)
	if !strings.Contains(ddl, "PARTITION BY HOUR") {
		t.Errorf("DDL missing PARTITION BY HOUR: %s", ddl)
	}
}

// ---- DDL Comparison Tests ----

func TestBuildAllCreateTableDDLs_Count(t *testing.T) {
	ddls := BuildAllCreateTableDDLs()
	if len(ddls) != 14 {
		t.Errorf("BuildAllCreateTableDDLs() returned %d DDLs, want 14", len(ddls))
	}
}

// ---- QuoteColumnName Tests ----

func TestQuoteColumnName_Reserved(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"interface", `"interface"`},
		{"table", `"table"`},
		{"select", `"select"`},
		{"column", `"column"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := QuoteColumnName(tc.name)
			if got != tc.want {
				t.Errorf("QuoteColumnName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestQuoteColumnName_NotReserved(t *testing.T) {
	tests := []string{"agent_id", "timestamp", "value", "cpu_usage_pct"}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			got := QuoteColumnName(name)
			if got != name {
				t.Errorf("QuoteColumnName(%q) = %q, want %q", name, got, name)
			}
		})
	}
}

// ---- Schema Introspection Tests ----

func TestSchemaDefinition_TableNames(t *testing.T) {
	schema := DefaultSchema()
	names := schema.TableNames()

	if len(names) != len(schema.Tables) {
		t.Errorf("TableNames() returned %d names, expected %d", len(names), len(schema.Tables))
	}

	for i, name := range names {
		if name != schema.Tables[i].Name {
			t.Errorf("TableNames()[%d] = %q, want %q", i, name, schema.Tables[i].Name)
		}
	}
}

func TestSchemaDefinition_FindTable(t *testing.T) {
	schema := DefaultSchema()

	table, ok := schema.FindTable("cpu_metrics")
	if !ok {
		t.Fatal("FindTable(cpu_metrics) returned false")
	}
	if table.Name != "cpu_metrics" {
		t.Errorf("FindTable returned table name %q, want 'cpu_metrics'", table.Name)
	}
}

func TestSchemaDefinition_FindTable_NotFound(t *testing.T) {
	schema := DefaultSchema()

	_, ok := schema.FindTable("nonexistent")
	if ok {
		t.Error("FindTable(nonexistent) should return false")
	}
}

func TestSchemaDefinition_HasTable(t *testing.T) {
	schema := DefaultSchema()

	if !schema.HasTable("cpu_metrics") {
		t.Error("HasTable(cpu_metrics) should return true")
	}
	if schema.HasTable("nonexistent") {
		t.Error("HasTable(nonexistent) should return false")
	}
}

func TestTableDefinition_HasColumn(t *testing.T) {
	schema := DefaultSchema()
	table, _ := schema.FindTable("cpu_metrics")

	if !table.HasColumn("agent_id") {
		t.Error("HasColumn(agent_id) should return true")
	}
	if table.HasColumn("nonexistent") {
		t.Error("HasColumn(nonexistent) should return false")
	}
}

func TestTableDefinition_ColumnCount(t *testing.T) {
	schema := DefaultSchema()

	// cpu_metrics has 14 columns.
	cpu, _ := schema.FindTable("cpu_metrics")
	if got := cpu.ColumnCount(); got != 14 {
		t.Errorf("cpu_metrics ColumnCount() = %d, want 14", got)
	}
}

func TestTableDefinition_ColumnNames(t *testing.T) {
	def := TableDefinition{
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
			{Name: "value", Type: ColTypeDouble},
		},
	}

	names := def.ColumnNames()
	if len(names) != 2 {
		t.Fatalf("ColumnNames() returned %d names, want 2", len(names))
	}
	if names[0] != "timestamp" || names[1] != "value" {
		t.Errorf("ColumnNames() = %v, want [timestamp, value]", names)
	}
}

// ---- Schema Version Constant Tests ----

func TestSchemaVersion_IsPositive(t *testing.T) {
	if SchemaVersion < 1 {
		t.Errorf("SchemaVersion = %d, must be positive", SchemaVersion)
	}
}

// ---- Column Migration Statement Tests ----

func TestBuildColumnMigrationStatements(t *testing.T) {
	def := TableDefinition{
		Name: "test_table",
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
			{Name: "agent_id", Type: ColTypeSymbol},
			{Name: "value", Type: ColTypeDouble},
		},
	}

	stmts := BuildColumnMigrationStatements(def)

	// Should have 2 statements (skipping timestamp).
	if len(stmts) != 2 {
		t.Fatalf("expected 2 migration statements, got %d", len(stmts))
	}

	if !strings.Contains(stmts[0], "ADD COLUMN") {
		t.Errorf("first statement missing ADD COLUMN: %s", stmts[0])
	}
	if !strings.Contains(stmts[0], "agent_id") {
		t.Errorf("first statement missing column name: %s", stmts[0])
	}
}

func TestBuildColumnMigrationStatements_SingleColumn(t *testing.T) {
	def := TableDefinition{
		Name: "minimal",
		Columns: []ColumnDefinition{
			{Name: "timestamp", Type: ColTypeTimestamp},
		},
	}

	stmts := BuildColumnMigrationStatements(def)
	if stmts != nil {
		t.Errorf("expected nil for single-column table, got %d statements", len(stmts))
	}
}

// ---- Migration Registration Tests ----

func TestSchemaManager_MigrationVersionsComplete(t *testing.T) {
	// All versions from 2 to SchemaVersion must have a registered migration.
	// Version 1 is the base (no migration needed).
	for v := 2; v <= SchemaVersion; v++ {
		sm := &SchemaManager{
			logger:    zap.NewNop(),
			schema:    DefaultSchema(),
			migration: make(map[int]migrationEntry),
		}
		sm.registerMigrations()

		if _, ok := sm.migration[v]; !ok {
			t.Errorf("no migration registered for version %d", v)
		}
	}
}

func TestSchemaManager_MigrationDescriptionsNotEmpty(t *testing.T) {
	sm := &SchemaManager{
		logger:    zap.NewNop(),
		schema:    DefaultSchema(),
		migration: make(map[int]migrationEntry),
	}
	sm.registerMigrations()

	for v, m := range sm.migration {
		if m.description == "" {
			t.Errorf("migration v%d has empty description", v)
		}
	}
}
