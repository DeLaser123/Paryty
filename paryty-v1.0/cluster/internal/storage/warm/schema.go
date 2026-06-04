// Package warm — Schema migration system for QuestDB warm storage.
//
// SchemaManager provides versioned, idempotent schema migrations for all
// QuestDB tables. It tracks the current schema version in a dedicated
// paryty_schema_version table and applies only the migrations needed to
// bring the schema up to the target version.
//
// Migrations are additive only — columns and tables are never deleted.
// Each migration script is idempotent (safe to re-run).
package warm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// SchemaVersion is the current schema version. Increment this when adding
// new migrations. Each version corresponds to a migration script in the
// migration registry.
const SchemaVersion = 3

// ---- Column Type Constants ----

const (
	// ColTypeTimestamp is a QuestDB timestamp column.
	ColTypeTimestamp = "TIMESTAMP"

	// ColTypeSymbol is a QuestDB symbol column (interned, low-cardinality).
	ColTypeSymbol = "SYMBOL"

	// ColTypeDouble is a QuestDB double-precision float column.
	ColTypeDouble = "DOUBLE"

	// ColTypeLong is a QuestDB 64-bit integer column.
	ColTypeLong = "LONG"

	// ColTypeString is a QuestDB variable-length string column.
	ColTypeString = "STRING"

	// ColTypeBoolean is a QuestDB boolean column.
	ColTypeBoolean = "BOOLEAN"

	// ColTypeInt is a QuestDB 32-bit integer column.
	ColTypeInt = "INT"
)

// ---- Partition Strategy Constants ----

const (
	// PartitionByDay partitions data by calendar day. Used for most tables
	// with moderate data volume.
	PartitionByDay = "DAY"

	// PartitionByHour partitions data by hour. Used for high-volume tables
	// where daily partitions would be too large.
	PartitionByHour = "HOUR"

	// PartitionByNone disables partitioning. Used for small lookup tables.
	PartitionByNone = "NONE"
)

// ---- Schema Definition Types ----

// SchemaDefinition describes the complete schema at a specific version.
type SchemaDefinition struct {
	// Version is the schema version number.
	Version int

	// Tables lists all table definitions in this schema version.
	Tables []TableDefinition
}

// TableDefinition describes a single QuestDB table.
type TableDefinition struct {
	// Name is the table name (lowercase, snake_case).
	Name string

	// Columns defines the ordered list of columns.
	Columns []ColumnDefinition

	// PartitionBy is the partition strategy (DAY, HOUR, or NONE).
	PartitionBy string

	// TTL is the retention period (e.g., "30d"). Empty means no TTL.
	TTL string

	// Dedup enables QuestDB WAL deduplication on this table.
	Dedup bool
}

// ColumnDefinition describes a single column in a QuestDB table.
type ColumnDefinition struct {
	// Name is the column name.
	Name string

	// Type is the QuestDB type (TIMESTAMP, SYMBOL, DOUBLE, LONG, STRING, BOOLEAN, INT).
	Type string

	// Indexed creates a column index for SYMBOL columns.
	Indexed bool

	// Nullable allows NULL values. Default is false for most QuestDB types.
	Nullable bool
}

// ---- Schema Manager ----

// SchemaManager manages versioned schema migrations for QuestDB.
// It tracks the current schema version in paryty_schema_version and applies
// only the migrations needed to reach the target version.
//
// Thread-safe: all methods are safe for concurrent use (though migrations
// should only be run once at startup).
type SchemaManager struct {
	pool      *pgxpool.Pool
	logger    *zap.Logger
	schema    SchemaDefinition
	migration map[int]migrationEntry
}

// migrationEntry holds a single migration step.
type migrationEntry struct {
	// description is a human-readable description of the migration.
	description string

	// up applies the migration. Must be idempotent.
	up func(ctx context.Context, pool *pgxpool.Pool) error
}

// NewSchemaManager creates a new SchemaManager with the given pgxpool and logger.
//
// Panics if logger is nil. The returned manager uses the default schema definition
// and migration registry. Call Migrate() at startup to apply pending migrations.
func NewSchemaManager(pool *pgxpool.Pool, logger *zap.Logger) *SchemaManager {
	if logger == nil {
		panic("warm.SchemaManager: logger must not be nil")
	}

	sm := &SchemaManager{
		pool:      pool,
		logger:    logger.Named("schema"),
		schema:    DefaultSchema(),
		migration: make(map[int]migrationEntry),
	}

	sm.registerMigrations()
	return sm
}

// ---- Default Schema ----

// DefaultSchema returns the complete schema definition at the current version.
// This includes all original tables (cpu_metrics, memory_metrics, etc.) and
// the Phase 4 tables (db_queries, topology_snapshots).
func DefaultSchema() SchemaDefinition {
	return SchemaDefinition{
		Version: SchemaVersion,
		Tables: []TableDefinition{
			// ---- Original Metric Tables ----
			{
				Name:        "cpu_metrics",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "total_usage_pct", Type: ColTypeDouble},
					{Name: "per_core_pct", Type: ColTypeString},
					{Name: "load_avg_1", Type: ColTypeDouble},
					{Name: "load_avg_5", Type: ColTypeDouble},
					{Name: "load_avg_15", Type: ColTypeDouble},
					{Name: "frequency_mhz", Type: ColTypeDouble},
					{Name: "context_switches", Type: ColTypeLong},
					{Name: "physical_cores", Type: ColTypeInt},
					{Name: "logical_cores", Type: ColTypeInt},
					{Name: "model_name", Type: ColTypeString},
					{Name: "vendor_id", Type: ColTypeString},
				},
			},
			{
				Name:        "memory_metrics",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "total_bytes", Type: ColTypeLong},
					{Name: "used_bytes", Type: ColTypeLong},
					{Name: "free_bytes", Type: ColTypeLong},
					{Name: "available_bytes", Type: ColTypeLong},
					{Name: "cached_bytes", Type: ColTypeLong},
					{Name: "buffer_bytes", Type: ColTypeLong},
					{Name: "swap_total_bytes", Type: ColTypeLong},
					{Name: "swap_used_bytes", Type: ColTypeLong},
					{Name: "usage_percent", Type: ColTypeDouble},
					{Name: "pressure_some_avg10", Type: ColTypeDouble},
					{Name: "pressure_some_avg60", Type: ColTypeDouble},
					{Name: "pressure_some_avg300", Type: ColTypeDouble},
					{Name: "pressure_full_avg10", Type: ColTypeDouble},
					{Name: "pressure_full_avg60", Type: ColTypeDouble},
					{Name: "pressure_full_avg300", Type: ColTypeDouble},
				},
			},
			{
				Name:        "disk_metrics",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "device", Type: ColTypeSymbol},
					{Name: "mount_point", Type: ColTypeSymbol},
					{Name: "filesystem_type", Type: ColTypeSymbol},
					{Name: "total_bytes", Type: ColTypeLong},
					{Name: "used_bytes", Type: ColTypeLong},
					{Name: "free_bytes", Type: ColTypeLong},
					{Name: "read_bytes_per_sec", Type: ColTypeLong},
					{Name: "write_bytes_per_sec", Type: ColTypeLong},
					{Name: "iops_read", Type: ColTypeLong},
					{Name: "iops_write", Type: ColTypeLong},
					{Name: "io_latency_ms", Type: ColTypeDouble},
					{Name: "queue_depth", Type: ColTypeDouble},
					{Name: "is_ssd", Type: ColTypeBoolean},
					{Name: "utilization_pct", Type: ColTypeDouble},
				},
			},
			{
				Name:        "network_metrics",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "interface", Type: ColTypeSymbol},
					{Name: "rx_bytes_per_sec", Type: ColTypeLong},
					{Name: "tx_bytes_per_sec", Type: ColTypeLong},
					{Name: "rx_packets", Type: ColTypeLong},
					{Name: "tx_packets", Type: ColTypeLong},
					{Name: "rx_dropped", Type: ColTypeLong},
					{Name: "tx_dropped", Type: ColTypeLong},
					{Name: "errors", Type: ColTypeLong},
					{Name: "estimated_rtt_ms", Type: ColTypeDouble},
					{Name: "total_rx_bytes", Type: ColTypeLong},
					{Name: "total_tx_bytes", Type: ColTypeLong},
					{Name: "total_rx_packets", Type: ColTypeLong},
					{Name: "total_tx_packets", Type: ColTypeLong},
					{Name: "speed_mbps", Type: ColTypeLong},
					{Name: "is_up", Type: ColTypeBoolean},
					{Name: "tcp_established", Type: ColTypeInt},
					{Name: "tcp_time_wait", Type: ColTypeInt},
					{Name: "tcp_listen", Type: ColTypeInt},
					{Name: "tcp_retransmit_count", Type: ColTypeLong},
				},
			},
			{
				Name:        "process_metrics",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "pid", Type: ColTypeLong},
					{Name: "parent_pid", Type: ColTypeLong},
					{Name: "name", Type: ColTypeSymbol},
					{Name: "command_line", Type: ColTypeString},
					{Name: "cpu_usage_pct", Type: ColTypeDouble},
					{Name: "memory_bytes", Type: ColTypeLong},
					{Name: "vsz_bytes", Type: ColTypeLong},
					{Name: "status", Type: ColTypeSymbol},
					{Name: "threads", Type: ColTypeLong},
					{Name: "fd_count", Type: ColTypeLong},
					{Name: "container_id", Type: ColTypeSymbol},
					{Name: "exe", Type: ColTypeString},
					{Name: "disk_read_bytes", Type: ColTypeLong},
					{Name: "disk_written_bytes", Type: ColTypeLong},
					{Name: "user_id", Type: ColTypeString},
				},
			},
			{
				Name:        "container_metrics",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "container_id", Type: ColTypeSymbol},
					{Name: "runtime", Type: ColTypeSymbol},
					{Name: "name", Type: ColTypeSymbol},
					{Name: "image", Type: ColTypeSymbol},
					{Name: "status", Type: ColTypeSymbol},
					{Name: "cgroup_version", Type: ColTypeSymbol},
					{Name: "memory_limit_bytes", Type: ColTypeLong},
					{Name: "cpu_quota", Type: ColTypeDouble},
					{Name: "cpu_shares", Type: ColTypeLong},
				},
			},
			{
				Name:        "aggregated_metrics",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "name", Type: ColTypeSymbol},
					{Name: "labels", Type: ColTypeString},
					{Name: "window", Type: ColTypeLong},
					{Name: "agg_type", Type: ColTypeSymbol},
					{Name: "value", Type: ColTypeDouble},
				},
			},
			{
				Name:        "spans",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "trace_id", Type: ColTypeSymbol},
					{Name: "span_id", Type: ColTypeSymbol},
					{Name: "parent_span_id", Type: ColTypeSymbol},
					{Name: "name", Type: ColTypeSymbol},
					{Name: "kind", Type: ColTypeSymbol},
					{Name: "service_name", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "end_time", Type: ColTypeTimestamp},
					{Name: "duration", Type: ColTypeLong},
					{Name: "status", Type: ColTypeSymbol},
					{Name: "status_code", Type: ColTypeSymbol},
					{Name: "status_message", Type: ColTypeString},
				},
			},

			// ---- Network Event Tables (eBPF) ----
			{
				Name:        "tcp_events",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "event_type", Type: ColTypeSymbol},
					{Name: "source_ip", Type: ColTypeString},
					{Name: "source_port", Type: ColTypeInt},
					{Name: "destination_ip", Type: ColTypeString},
					{Name: "destination_port", Type: ColTypeInt},
					{Name: "state", Type: ColTypeSymbol},
					{Name: "bytes_sent", Type: ColTypeLong},
					{Name: "bytes_received", Type: ColTypeLong},
					{Name: "pid", Type: ColTypeInt},
					{Name: "process_name", Type: ColTypeString},
				},
			},
			{
				Name:        "dns_events",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "query_name", Type: ColTypeString},
					{Name: "query_type", Type: ColTypeSymbol},
					{Name: "latency_ms", Type: ColTypeDouble},
					{Name: "pid", Type: ColTypeInt},
				},
			},
			{
				Name:        "http_events",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "method", Type: ColTypeSymbol},
					{Name: "path", Type: ColTypeString},
					{Name: "status_code", Type: ColTypeInt},
					{Name: "latency_ms", Type: ColTypeDouble},
					{Name: "source_ip", Type: ColTypeString},
					{Name: "destination_ip", Type: ColTypeString},
					{Name: "destination_port", Type: ColTypeInt},
					{Name: "host", Type: ColTypeString},
					{Name: "pid", Type: ColTypeInt},
				},
			},

			// ---- Phase 4 Tables ----

			// db_queries captures eBPF database inspection events.
			{
				Name:        "db_queries",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "agent_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "protocol", Type: ColTypeSymbol},
					{Name: "query_type", Type: ColTypeSymbol},
					{Name: "table_name", Type: ColTypeSymbol},
					{Name: "database", Type: ColTypeSymbol},
					{Name: "destination_ip", Type: ColTypeSymbol},
					{Name: "destination_port", Type: ColTypeInt},
					{Name: "pid", Type: ColTypeInt},
					{Name: "process_name", Type: ColTypeSymbol},
					{Name: "latency_ms", Type: ColTypeDouble},
					{Name: "row_count", Type: ColTypeLong},
					{Name: "error_message", Type: ColTypeString},
					{Name: "query_sample", Type: ColTypeString},
				},
			},

			// topology_snapshots stores periodic graph snapshots for timeline playback.
			{
				Name:        "topology_snapshots",
				PartitionBy: PartitionByDay,
				Columns: []ColumnDefinition{
					{Name: "timestamp", Type: ColTypeTimestamp},
					{Name: "snapshot_id", Type: ColTypeSymbol},
					{Name: "tenant_id", Type: ColTypeSymbol},
					{Name: "node_count", Type: ColTypeLong},
					{Name: "edge_count", Type: ColTypeLong},
					{Name: "snapshot_data", Type: ColTypeString},
					{Name: "is_checkpoint", Type: ColTypeBoolean},
				},
			},

			// paryty_schema_version tracks applied migration versions.
			// Not WAL/partitioned — small metadata table.
			{
				Name:        "paryty_schema_version",
				PartitionBy: PartitionByNone,
				Columns: []ColumnDefinition{
					{Name: "version", Type: ColTypeLong},
					{Name: "applied_at", Type: ColTypeTimestamp},
					{Name: "description", Type: ColTypeString},
				},
			},
		},
	}
}

// ---- Migration Registry ----

// registerMigrations populates the migration map with ordered migration steps.
// Each migration key represents the version the database will be at AFTER
// the migration is applied.
func (sm *SchemaManager) registerMigrations() {
	// Migration v0 → v1: Initial schema (existing tables already created by EnsureTables).
	// This is a no-op for fresh installs where tables already exist.
	sm.migration[1] = migrationEntry{
		description: "initial schema (tables created by EnsureTables)",
		up: func(_ context.Context, _ *pgxpool.Pool) error {
			sm.logger.Info("migration v1: initial schema — tables already exist, no-op")
			return nil
		},
	}

	// Migration v1 → v2: Add db_queries and topology_snapshots tables.
	sm.migration[2] = migrationEntry{
		description: "add db_queries and topology_snapshots tables (Phase 4)",
		up: func(ctx context.Context, pool *pgxpool.Pool) error {
			schema := DefaultSchema()
			for _, table := range schema.Tables {
				if table.Name == "db_queries" || table.Name == "topology_snapshots" {
					ddl := BuildCreateTableDDL(table)
					sm.logger.Info("applying migration: creating table",
						zap.String("table", table.Name),
					)
					if _, err := pool.Exec(ctx, ddl); err != nil {
						return fmt.Errorf("create table %s: %w", table.Name, err)
					}
				}
			}
			return nil
		},
	}

	// Migration v2 → v3: Placeholder for future changes.
	sm.migration[3] = migrationEntry{
		description: "placeholder for future schema changes",
		up: func(_ context.Context, _ *pgxpool.Pool) error {
			sm.logger.Info("migration v3: no-op placeholder applied")
			return nil
		},
	}
}

// ---- Migration Execution ----

// Migrate applies all pending schema migrations to bring the database up to
// the current SchemaVersion. It is idempotent — running Migrate multiple times
// applies only the migrations that have not yet been applied.
//
// The migration process:
//  1. Ensure the paryty_schema_version table exists.
//  2. Read the current schema version from the table.
//  3. Apply each missing migration in order, updating the version after each.
//
// Errors are wrapped with context. Failed migrations do not advance the version.
func (sm *SchemaManager) Migrate(ctx context.Context) error {
	// Step 1: Ensure the version tracking table exists.
	if err := sm.EnsureTable(ctx, sm.findTable("paryty_schema_version")); err != nil {
		return fmt.Errorf("ensure schema version table: %w", err)
	}

	// Step 2: Read current version.
	currentVersion, err := sm.GetSchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("get current schema version: %w", err)
	}

	sm.logger.Info("schema migration check",
		zap.Int("current_version", currentVersion),
		zap.Int("target_version", SchemaVersion),
	)

	if currentVersion >= SchemaVersion {
		sm.logger.Info("schema is up to date",
			zap.Int("version", currentVersion),
		)
		return nil
	}

	// Step 3: Apply missing migrations in order.
	for v := currentVersion + 1; v <= SchemaVersion; v++ {
		migration, ok := sm.migration[v]
		if !ok {
			return fmt.Errorf("no migration registered for version %d", v)
		}

		sm.logger.Info("applying migration",
			zap.Int("target_version", v),
			zap.String("description", migration.description),
		)

		start := time.Now()

		if err := migration.up(ctx, sm.pool); err != nil {
			return fmt.Errorf("migration v%d failed: %w", v, err)
		}

		if err := sm.SetSchemaVersion(ctx, v, migration.description); err != nil {
			return fmt.Errorf("set schema version to %d: %w", v, err)
		}

		sm.logger.Info("migration applied",
			zap.Int("version", v),
			zap.Duration("elapsed", time.Since(start)),
		)
	}

	sm.logger.Info("schema migration complete",
		zap.Int("final_version", SchemaVersion),
	)
	return nil
}

// findTable returns the TableDefinition with the given name from the default schema.
// Returns a zero-value TableDefinition if not found.
func (sm *SchemaManager) findTable(name string) TableDefinition {
	for _, table := range sm.schema.Tables {
		if table.Name == name {
			return table
		}
	}
	return TableDefinition{Name: name}
}

// ---- Table Management ----

// EnsureTable creates the table described by def if it does not already exist.
// Uses CREATE TABLE IF NOT EXISTS for idempotency. Also applies column
// definitions from the schema.
//
// For tables with PartitionBy "NONE", no PARTITION BY or WAL clause is added.
// The paryty_schema_version table uses this pattern.
func (sm *SchemaManager) EnsureTable(ctx context.Context, def TableDefinition) error {
	ddl := BuildCreateTableDDL(def)

	sm.logger.Debug("ensuring table",
		zap.String("table", def.Name),
		zap.Int("columns", len(def.Columns)),
		zap.String("partition", def.PartitionBy),
	)

	if _, err := sm.pool.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create table %s: %w", def.Name, err)
	}

	return nil
}

// EnsureAllTables creates all tables in the schema definition. Tables that
// already exist are skipped (CREATE TABLE IF NOT EXISTS). This is a convenience
// method for initial setup.
func (sm *SchemaManager) EnsureAllTables(ctx context.Context) error {
	for _, table := range sm.schema.Tables {
		if err := sm.EnsureTable(ctx, table); err != nil {
			return fmt.Errorf("ensure table %s: %w", table.Name, err)
		}
	}
	sm.logger.Info("all schema tables ensured",
		zap.Int("count", len(sm.schema.Tables)),
	)
	return nil
}

// ---- Schema Version Management ----

// GetSchemaVersion reads the current schema version from the paryty_schema_version
// table. Returns 0 if no version has been recorded (fresh database).
func (sm *SchemaManager) GetSchemaVersion(ctx context.Context) (int, error) {
	query := `SELECT MAX(version) FROM paryty_schema_version`

	var version int
	err := sm.pool.QueryRow(ctx, query).Scan(&version)
	if err != nil {
		// Table might exist but be empty — return 0 for fresh database.
		sm.logger.Debug("get schema version: no rows, treating as version 0",
			zap.Error(err),
		)
		return 0, nil
	}

	return version, nil
}

// SetSchemaVersion records a new schema version in the paryty_schema_version table.
// This is called after each successful migration step.
func (sm *SchemaManager) SetSchemaVersion(ctx context.Context, version int, description string) error {
	query := `INSERT INTO paryty_schema_version (version, applied_at, description)
		VALUES ($1, $2, $3)`

	if _, err := sm.pool.Exec(ctx, query, int64(version), time.Now().UTC(), description); err != nil {
		return fmt.Errorf("insert schema version %d: %w", version, err)
	}

	return nil
}

// ---- DDL Generation ----

// BuildCreateTableDDL generates a CREATE TABLE IF NOT EXISTS DDL statement
// from a TableDefinition. The generated SQL follows QuestDB conventions:
//
//   - SYMBOL columns are used for low-cardinality fields.
//   - TIMESTAMP(timestamp) designates the designated timestamp column.
//   - PARTITION BY DAY/HOUR enables time-based partitioning.
//   - WAL enables write-ahead log mode for concurrent ingestion.
//
// For PartitionBy "NONE", no TIMESTAMP, PARTITION BY, or WAL clauses are added.
func BuildCreateTableDDL(def TableDefinition) string {
	var b strings.Builder
	b.Grow(512)

	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	b.WriteString(def.Name)
	b.WriteString(" (\n")

	for i, col := range def.Columns {
		b.WriteString("\t")
		b.WriteString(QuoteColumnName(col.Name))
		b.WriteString(" ")
		b.WriteString(col.Type)

		// SYMBOL columns can have indexed/not-indexed.
		if col.Type == ColTypeSymbol && !col.Indexed {
			// Default: not explicitly indexed (QuestDB indexes all SYMBOLs by default).
		}

		if i < len(def.Columns)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(")")

	// For NONE partitioning (small lookup tables), skip timestamp/partition/wal.
	if def.PartitionBy != PartitionByNone {
		// Find the first TIMESTAMP column as the designated timestamp.
		tsCol := findTimestampColumn(def.Columns)
		if tsCol != "" {
			b.WriteString(" TIMESTAMP(")
			b.WriteString(tsCol)
			b.WriteString(")")
		}

		b.WriteString(" PARTITION BY ")
		b.WriteString(def.PartitionBy)
		b.WriteString(" WAL")
	}

	return b.String()
}

// BuildColumnMigrationStatements generates ALTER TABLE ADD COLUMN statements
// for all columns in the table definition. These are idempotent — if the column
// already exists, the statement will fail silently when executed.
//
// The first column is assumed to be the timestamp/designated column and is skipped.
func BuildColumnMigrationStatements(def TableDefinition) []string {
	if len(def.Columns) <= 1 {
		return nil
	}

	stmts := make([]string, 0, len(def.Columns)-1)
	for _, col := range def.Columns[1:] { // skip timestamp
		stmts = append(stmts, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s",
			def.Name,
			QuoteColumnName(col.Name),
			col.Type,
		))
	}
	return stmts
}

// BuildAllCreateTableDDLs returns DDL statements for all tables in the schema.
// This is useful for comparison with the existing createTableStatements slice
// in questdb.go.
func BuildAllCreateTableDDLs() []string {
	schema := DefaultSchema()
	ddls := make([]string, 0, len(schema.Tables))
	for _, table := range schema.Tables {
		ddls = append(ddls, BuildCreateTableDDL(table))
	}
	return ddls
}

// ---- Utility Functions ----

// findTimestampColumn returns the name of the first TIMESTAMP column in the list.
// Returns empty string if no TIMESTAMP column is found.
func findTimestampColumn(cols []ColumnDefinition) string {
	for _, col := range cols {
		if col.Type == ColTypeTimestamp {
			return col.Name
		}
	}
	return ""
}

// QuoteColumnName quotes a column name if it conflicts with a QuestDB reserved word.
// QuestDB uses double-quote quoting for reserved words like "interface".
func QuoteColumnName(name string) string {
	// Only quote known reserved words. This avoids unnecessary quoting
	// while protecting against SQL keyword collisions.
	if isReservedWord(name) {
		return `"` + name + `"`
	}
	return name
}

// isReservedWord returns true if the name is a QuestDB/SQL reserved word
// that requires quoting when used as a column name.
func isReservedWord(name string) bool {
	reserved := map[string]bool{
		"interface": true,
		"table":     true,
		"select":    true,
		"from":      true,
		"where":     true,
		"index":     true,
		"key":       true,
		"primary":   true,
		"values":    true,
		"column":    true,
		"alter":     true,
		"create":    true,
		"drop":      true,
		"database":  true,
		"order":     true,
		"group":     true,
		"by":        true,
		"limit":     true,
		"offset":    true,
		"join":      true,
		"union":     true,
		"into":      true,
		"set":       true,
		"null":      true,
		"default":   true,
		"check":     true,
		"unique":    true,
		"not":       true,
		"and":       true,
		"or":        true,
		"in":        true,
		"is":        true,
		"as":        true,
		"on":        true,
	}
	return reserved[name]
}

// ---- Schema Introspection ----

// ValidateTableDefinition checks that a TableDefinition is structurally valid:
//   - Name must not be empty.
//   - Columns must not be empty.
//   - First column must be TIMESTAMP type.
//   - PartitionBy must be DAY, HOUR, or NONE.
//
// Returns an error describing the first validation failure, or nil if valid.
func ValidateTableDefinition(def TableDefinition) error {
	if def.Name == "" {
		return fmt.Errorf("table name must not be empty")
	}
	if len(def.Columns) == 0 {
		return fmt.Errorf("table %s: must have at least one column", def.Name)
	}

	// For time-partitioned tables, the first column must be TIMESTAMP.
	// PartitionBy NONE tables (like paryty_schema_version) can have any first column.
	if def.PartitionBy != PartitionByNone {
		if def.Columns[0].Type != ColTypeTimestamp {
			return fmt.Errorf("table %s: first column must be TIMESTAMP, got %s", def.Name, def.Columns[0].Type)
		}
	}
	switch def.PartitionBy {
	case PartitionByDay, PartitionByHour, PartitionByNone:
		// Valid.
	default:
		return fmt.Errorf("table %s: invalid partition strategy %q (must be DAY, HOUR, or NONE)",
			def.Name, def.PartitionBy)
	}

	// Validate column types.
	validTypes := map[string]bool{
		ColTypeTimestamp: true,
		ColTypeSymbol:    true,
		ColTypeDouble:    true,
		ColTypeLong:      true,
		ColTypeString:    true,
		ColTypeBoolean:   true,
		ColTypeInt:       true,
	}
	for _, col := range def.Columns {
		if col.Name == "" {
			return fmt.Errorf("table %s: column name must not be empty", def.Name)
		}
		if !validTypes[col.Type] {
			return fmt.Errorf("table %s: column %s has invalid type %q", def.Name, col.Name, col.Type)
		}
	}

	return nil
}

// ValidateSchema checks all table definitions in the schema for structural validity.
// Returns all validation errors found (one per invalid table), or nil if valid.
func ValidateSchema(schema SchemaDefinition) []error {
	var errs []error
	for _, table := range schema.Tables {
		if err := ValidateTableDefinition(table); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// TableNames returns the names of all tables in the schema definition.
func (sd SchemaDefinition) TableNames() []string {
	names := make([]string, len(sd.Tables))
	for i, table := range sd.Tables {
		names[i] = table.Name
	}
	return names
}

// FindTable returns the TableDefinition with the given name, or false if not found.
func (sd SchemaDefinition) FindTable(name string) (TableDefinition, bool) {
	for _, table := range sd.Tables {
		if table.Name == name {
			return table, true
		}
	}
	return TableDefinition{}, false
}

// HasTable returns true if the schema contains a table with the given name.
func (sd SchemaDefinition) HasTable(name string) bool {
	_, ok := sd.FindTable(name)
	return ok
}

// ColumnNames returns the names of all columns in the table definition.
func (td TableDefinition) ColumnNames() []string {
	names := make([]string, len(td.Columns))
	for i, col := range td.Columns {
		names[i] = col.Name
	}
	return names
}

// HasColumn returns true if the table has a column with the given name.
func (td TableDefinition) HasColumn(name string) bool {
	for _, col := range td.Columns {
		if col.Name == name {
			return true
		}
	}
	return false
}

// ColumnCount returns the number of columns in the table definition.
func (td TableDefinition) ColumnCount() int {
	return len(td.Columns)
}

// ---- New Table DDL Constants (for questdb.go integration) ----

// newTableStatements holds the DDL for Phase 4 tables that should be added
// to createTableStatements in questdb.go. These are the raw SQL strings
// matching the existing pattern.
var newTableStatements = []string{
	`CREATE TABLE IF NOT EXISTS db_queries (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		protocol SYMBOL,
		query_type SYMBOL,
		table_name SYMBOL,
		database SYMBOL,
		destination_ip SYMBOL,
		destination_port INT,
		pid INT,
		process_name SYMBOL,
		latency_ms DOUBLE,
		row_count LONG,
		error_message STRING,
		query_sample STRING
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS topology_snapshots (
		timestamp TIMESTAMP,
		snapshot_id SYMBOL,
		tenant_id SYMBOL,
		node_count LONG,
		edge_count LONG,
		snapshot_data STRING,
		is_checkpoint BOOLEAN
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS paryty_schema_version (
		version LONG,
		applied_at TIMESTAMP,
		description STRING
	)`,
}
