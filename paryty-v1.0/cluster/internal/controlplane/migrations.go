// Package controlplane implements the multi-tenant control plane for the
// Paryty cluster. This file contains the migration runner that applies
// versioned SQL migrations from the cluster/migrations/ directory.
//
// The migration runner complements the idempotent DDL in schema.go
// (EnsureTables, EnsurePhase8Tables) by providing a versioned, trackable
// migration history. It uses the existing pgxpool connection pool and
// does not depend on external migration libraries.
package controlplane

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// migrationsTableName is the tracking table for applied migrations.
	// Distinct from golang-migrate's schema_migrations (bigint version) to
	// avoid type conflicts while coexisting.
	migrationsTableName = "schema_migrations_controlplane"

	// migrationsDirDefault is the default directory for migration files
	// relative to the cluster root.
	migrationsDirDefault = "migrations"
)

// RunMigrations applies pending SQL migration files from the specified
// directory. Migrations are tracked in the schema_migrations_controlplane
// table. Each .sql file is executed within its own transaction, and
// already-applied migrations are skipped on subsequent runs.
//
// Only files matching *.sql are considered. Files ending with .down.sql
// (golang-migrate rollback scripts) are excluded — this runner only
// applies forward migrations and coexists with golang-migrate's naming
// conventions.
//
// Migration files are sorted alphabetically, so a naming convention of
// NNN_description.sql (e.g., 001_create_users.sql) ensures deterministic
// ordering.
//
// This function complements — not replaces — the idempotent DDL in
// EnsureTables and EnsurePhase8Tables. On a fresh database, both the
// migration runner and the idempotent DDL can safely run: the migration
// runner records history, and the idempotent DDL fills any gaps.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	if migrationsDir == "" {
		migrationsDir = migrationsDirDefault
	}

	// Resolve to absolute path so os.ReadDir works regardless of working
	// directory. Fall back to the provided path if resolution fails.
	if absPath, err := filepath.Abs(migrationsDir); err == nil {
		migrationsDir = absPath
	}

	// ── Ensure tracking table ──────────────────────────────────────────
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (
			version   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`, migrationsTableName)); err != nil {
		return fmt.Errorf("create migration tracking table: %w", err)
	}

	// ── Discover migration files ───────────────────────────────────────
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations directory %s: %w", migrationsDir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		// Exclude golang-migrate rollback scripts (.down.sql).
		if strings.HasSuffix(name, ".down.sql") {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)

	if len(files) == 0 {
		return nil
	}

	// ── Apply pending migrations ───────────────────────────────────────
	for _, filename := range files {
		version := strings.TrimSuffix(filename, ".sql")

		// Check whether this migration has already been applied.
		var exists bool
		err := pool.QueryRow(ctx,
			fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE version = $1)", migrationsTableName),
			version,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if exists {
			continue
		}

		// Read the SQL file.
		filePath := filepath.Join(migrationsDir, filename)
		sqlBytes, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read migration file %s: %w", filename, err)
		}
		sqlContent := string(sqlBytes)
		if strings.TrimSpace(sqlContent) == "" {
			continue
		}

		// Execute the migration within a transaction. The tracking INSERT
		// is part of the same transaction so we never record a migration
		// that didn't actually apply.
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, sqlContent); err != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				return fmt.Errorf("execute migration %s: %w (rollback also failed: %v)", version, err, rollbackErr)
			}
			return fmt.Errorf("execute migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx,
			fmt.Sprintf("INSERT INTO %s (version, applied_at) VALUES ($1, $2)", migrationsTableName),
			version, time.Now().UTC(),
		); err != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				return fmt.Errorf("record migration %s: %w (rollback also failed: %v)", version, err, rollbackErr)
			}
			return fmt.Errorf("record migration %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}

	return nil
}
