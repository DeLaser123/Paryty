package warm

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// RetentionRule defines a retention policy for a single QuestDB table.
// MaxAge controls when data becomes eligible for retention, and ArchiveToCold
// determines whether data is exported to cold storage (SeaweedFS) before
// the partition is dropped.
type RetentionRule struct {
	Table         string        `json:"table"`
	MaxAge        time.Duration `json:"max_age"`
	ArchiveToCold bool          `json:"archive_to_cold"`
}

// DefaultRetentionRules are the default retention policies for all QuestDB tables.
// Tables with ArchiveToCold=true will have their data exported to cold storage
// (SeaweedFS) before the partition is dropped.
//
// Retention periods:
//   - Metric tables (cpu, memory, disk, network): 30 days, archived
//   - Process/container metrics: 7 days, not archived (high volume, low value)
//   - Aggregated metrics: 90 days, archived (downsampled summaries)
//   - Spans and DB queries: 14 days, archived
//   - Topology snapshots: 7 days, archived
var DefaultRetentionRules = []RetentionRule{
	{Table: "cpu_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
	{Table: "memory_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
	{Table: "disk_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
	{Table: "network_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
	{Table: "process_metrics", MaxAge: 7 * 24 * time.Hour, ArchiveToCold: false},
	{Table: "container_metrics", MaxAge: 7 * 24 * time.Hour, ArchiveToCold: false},
	{Table: "aggregated_metrics", MaxAge: 90 * 24 * time.Hour, ArchiveToCold: true},
	{Table: "spans", MaxAge: 14 * 24 * time.Hour, ArchiveToCold: true},
	{Table: "db_queries", MaxAge: 14 * 24 * time.Hour, ArchiveToCold: true},
	{Table: "topology_snapshots", MaxAge: 7 * 24 * time.Hour, ArchiveToCold: true},
}

// ColdArchiver is the interface for archiving warm-tier data to cold storage.
// Implementations are responsible for querying the warm tier, serializing the
// data, and storing it in the cold tier (e.g., SeaweedFS via minio-go).
//
// The before parameter indicates the retention cutoff time — only data older
// than this time should be archived.
type ColdArchiver interface {
	ArchiveTable(ctx context.Context, table string, before time.Time) error
}

// RetentionManager manages QuestDB data lifecycle via partition-based retention.
// It periodically drops old partitions and optionally archives data to cold
// storage before deletion.
//
// Partition dropping in QuestDB is an instant metadata-only operation that does
// not require scanning the data. This makes retention management efficient even
// for tables with billions of rows.
//
// Safety guarantees:
//   - The most recent partition is NEVER dropped, even if it exceeds MaxAge
//   - Archiving is attempted BEFORE dropping (no data loss on archive failure)
//   - Failed partition drops are logged but do not abort the retention run
type RetentionManager struct {
	pool      *pgxpool.Pool
	rules     []RetentionRule
	coldStore ColdArchiver // nil if archiving disabled
	logger    *zap.Logger
}

// NewRetentionManager creates a new RetentionManager.
//
// If coldStore is nil, data will not be archived before partition drops.
// If rules is nil or empty, DefaultRetentionRules will be used.
//
// The pool is used for QuestDB partition operations (listing and dropping).
// The logger receives structured retention events.
func NewRetentionManager(
	pool *pgxpool.Pool,
	rules []RetentionRule,
	coldStore ColdArchiver,
	logger *zap.Logger,
) *RetentionManager {
	if len(rules) == 0 {
		rules = DefaultRetentionRules
	}
	return &RetentionManager{
		pool:      pool,
		rules:     rules,
		coldStore: coldStore,
		logger:    logger,
	}
}

// RunRetention executes the retention policy for all configured tables.
// For each rule, it:
//  1. Archives old data to cold storage (if configured and ArchiveToCold=true)
//  2. Lists all partitions for the table
//  3. Identifies partitions older than the retention cutoff
//  4. Drops expired partitions (excluding the most recent one)
//
// Archiving always happens before dropping to prevent data loss. If archiving
// fails for a table, that table's partitions are NOT dropped.
//
// The method continues processing all rules even if individual rules fail.
// It returns the last error encountered, or nil if all rules succeeded.
func (rm *RetentionManager) RunRetention(ctx context.Context) error {
	rm.logger.Info("starting retention run", zap.Int("rules", len(rm.rules)))

	var lastErr error
	for _, rule := range rm.rules {
		if err := rm.processRule(ctx, rule); err != nil {
			rm.logger.Error("retention rule failed",
				zap.String("table", rule.Table),
				zap.Error(err),
			)
			lastErr = err
			// Continue with remaining rules — one failure must not abort the run.
		}
	}

	rm.logger.Info("retention run complete")
	return lastErr
}

// processRule applies a single retention rule: optionally archive, then drop
// expired partitions. Returns an error if archiving fails; partition drop
// failures are logged but do not propagate.
func (rm *RetentionManager) processRule(ctx context.Context, rule RetentionRule) error {
	cutoff := retentionCutoff(rule.MaxAge)

	rm.logger.Debug("processing retention rule",
		zap.String("table", rule.Table),
		zap.Duration("max_age", rule.MaxAge),
		zap.Time("cutoff", cutoff),
		zap.Bool("archive_to_cold", rule.ArchiveToCold),
	)

	// Step 1: Archive to cold storage if configured.
	// Archiving MUST succeed before we drop partitions — this is the
	// no-data-loss guarantee.
	if rule.ArchiveToCold && rm.coldStore != nil {
		if err := rm.ArchiveTableData(ctx, rule.Table, cutoff); err != nil {
			return fmt.Errorf("archive %s: %w", rule.Table, err)
		}
	}

	// Step 2: List existing partitions and identify expired ones.
	partitions, err := rm.ListPartitions(ctx, rule.Table)
	if err != nil {
		return fmt.Errorf("list partitions for %s: %w", rule.Table, err)
	}

	toDrop := partitionsToDrop(partitions, cutoff)
	if len(toDrop) == 0 {
		rm.logger.Debug("no partitions to drop",
			zap.String("table", rule.Table),
			zap.Int("total_partitions", len(partitions)),
		)
		return nil
	}

	// Step 3: Drop expired partitions.
	var dropped, failed int
	for _, p := range toDrop {
		if err := rm.DropPartition(ctx, rule.Table, p); err != nil {
			rm.logger.Error("drop partition failed",
				zap.String("table", rule.Table),
				zap.String("partition", p),
				zap.Error(err),
			)
			failed++
			continue
		}
		dropped++
	}

	rm.logger.Info("retention completed for table",
		zap.String("table", rule.Table),
		zap.Int("partitions_dropped", dropped),
		zap.Int("partitions_failed", failed),
		zap.Int("total_partitions", len(partitions)),
		zap.Duration("max_age", rule.MaxAge),
	)

	return nil
}

// DropPartition drops a single partition from a QuestDB table.
// The partition name must be in "YYYY-MM-DD" format (QuestDB DAY partition naming).
//
// This is a fast metadata-only operation in QuestDB — it does not scan or
// rewrite data. The partition files are removed from disk.
func (rm *RetentionManager) DropPartition(ctx context.Context, table string, partition string) error {
	query := dropPartitionQuery(table, partition)

	if _, err := rm.pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("drop partition %s from %s: %w", partition, table, err)
	}

	rm.logger.Debug("dropped partition",
		zap.String("table", table),
		zap.String("partition", partition),
	)
	return nil
}

// ListPartitions returns the list of partition dates for a QuestDB table.
// Each date is formatted as "YYYY-MM-DD" (QuestDB DAY partition naming convention).
//
// Returns an empty slice if the table has no data or doesn't exist yet.
// The returned slice is sorted in ascending order (oldest first).
func (rm *RetentionManager) ListPartitions(ctx context.Context, table string) ([]string, error) {
	query := listPartitionsQuery(table)

	rows, err := rm.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query partitions for %s: %w", table, err)
	}
	defer rows.Close()

	var partitions []string
	for rows.Next() {
		var ts time.Time
		if err := rows.Scan(&ts); err != nil {
			return nil, fmt.Errorf("scan partition date from %s: %w", table, err)
		}
		partitions = append(partitions, partitionDate(ts))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate partitions for %s: %w", table, err)
	}

	return partitions, nil
}

// ArchiveTableData exports data older than the given time to cold storage
// via the ColdArchiver interface. If coldStore is nil, this is a no-op
// and returns nil.
//
// The actual query, serialization, and storage logic is delegated to the
// ColdArchiver implementation. The RetentionManager is only responsible
// for orchestrating the call and logging.
func (rm *RetentionManager) ArchiveTableData(ctx context.Context, table string, before time.Time) error {
	if rm.coldStore == nil {
		return nil
	}

	rm.logger.Info("archiving table data to cold storage",
		zap.String("table", table),
		zap.Time("before", before),
	)

	if err := rm.coldStore.ArchiveTable(ctx, table, before); err != nil {
		return fmt.Errorf("cold archive %s before %s: %w",
			table, before.Format("2006-01-02"), err)
	}

	rm.logger.Info("archived table data to cold storage",
		zap.String("table", table),
		zap.Time("before", before),
	)
	return nil
}

// ---- SQL Generation ----

// dropPartitionQuery returns the QuestDB SQL for dropping a partition.
// The partition must be in "YYYY-MM-DD" format.
//
// Example output:
//
//	ALTER TABLE cpu_metrics DROP PARTITION LIST '2024-01-15'
func dropPartitionQuery(table, partition string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP PARTITION LIST '%s'", table, partition)
}

// listPartitionsQuery returns the QuestDB SQL for listing partition dates.
// Returns distinct dates (cast from the timestamp column) sorted ascending.
//
// Example output:
//
//	SELECT DISTINCT cast(timestamp as date) FROM cpu_metrics ORDER BY 1
func listPartitionsQuery(table string) string {
	return fmt.Sprintf("SELECT DISTINCT cast(timestamp as date) FROM %s ORDER BY 1", table)
}

// ---- Internal Helpers ----

// retentionCutoff returns the cutoff time for the given max age.
// Partitions whose date is strictly before this cutoff are eligible for
// retention. The cutoff is truncated to midnight UTC for clean day
// boundaries (matching QuestDB DAY partitioning).
func retentionCutoff(maxAge time.Duration) time.Time {
	now := time.Now().UTC()
	// Use explicit date construction instead of Truncate(24h) to avoid
	// alignment issues with Go's Truncate (which is relative to the
	// zero time, not calendar days).
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return midnight.Add(-maxAge)
}

// partitionDate formats a time.Time as a QuestDB partition name ("YYYY-MM-DD").
// The time is converted to UTC before formatting.
func partitionDate(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// parsePartitionDate parses a QuestDB partition name ("YYYY-MM-DD") back to
// a time.Time at midnight UTC.
func parsePartitionDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse partition date %q: %w", s, err)
	}
	return t, nil
}

// partitionsToDrop returns partition names whose dates are strictly before
// the cutoff, excluding the most recent partition (safety check to prevent
// data loss).
//
// The most recent partition is identified by lexicographic sort (which works
// correctly for "YYYY-MM-DD" format). Even if the most recent partition is
// older than the cutoff, it is never included in the drop list.
//
// Returns nil if there are 0 or 1 partitions, or if no partitions qualify.
func partitionsToDrop(partitions []string, cutoff time.Time) []string {
	if len(partitions) <= 1 {
		return nil
	}

	// Copy and sort ascending to identify the most recent partition.
	sorted := make([]string, len(partitions))
	copy(sorted, partitions)
	sort.Strings(sorted)

	// The most recent partition is the last element after ascending sort.
	// It is always preserved regardless of age.
	mostRecentIdx := len(sorted) - 1

	var toDrop []string
	for i := 0; i < mostRecentIdx; i++ {
		t, err := parsePartitionDate(sorted[i])
		if err != nil {
			continue // Skip malformed partition names.
		}
		if t.Before(cutoff) {
			toDrop = append(toDrop, sorted[i])
		}
	}

	return toDrop
}
