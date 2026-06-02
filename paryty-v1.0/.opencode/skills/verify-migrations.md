# Verify Migrations — Database Migration Verification

## Purpose
Validate database schema migrations for correctness, rollback safety, and performance impact. Run before applying any migration.

## Execution Steps

### Step 1: Validate Migration Syntax
Check: SQL/QuestDB syntax is valid
Check: Migration has both up and down scripts
Check: Migration is idempotent where possible

### Step 2: Check Migration Order
Check: Migrations are numbered sequentially
Check: No gaps in migration sequence
Check: No conflicting migrations

### Step 3: Verify Rollback Safety
Check: Down migration reverses up migration
Check: No data loss in rollback
Check: Indexes can be dropped safely

### Step 4: Performance Impact Assessment
Check: No full table scans in migration
Check: Indexes added concurrently (PostgreSQL/QuestDB)
Check: Large data migrations are batched
Check: Migration timeout is reasonable

### Step 5: Test Migration
Run: Apply migration to test database
Run: Verify schema changes
Run: Apply rollback
Run: Verify rollback restores original state

### Step 6: Verify Application Compatibility
Check: New columns have defaults
Check: Renamed columns have aliases
Check: Removed columns are not referenced in code

## Paryty-Specific Migrations

### Dragonfly (Hot Store)
- Schema is implicit (key-value)
- No migrations needed
- TTL policies managed via configuration
- Verify: Key patterns match expected format

### QuestDB (Warm Store)
```sql
-- Good: Add column with default
ALTER TABLE metrics ADD COLUMN service_version VARCHAR DEFAULT 'unknown';

-- Bad: Add column without default (breaks existing inserts)
ALTER TABLE metrics ADD COLUMN service_version VARCHAR;
```

### SeaweedFS (Cold Store)
- Schema is implicit (object storage)
- No migrations needed
- Verify: Object naming conventions
- Verify: Retention policies applied

### PostgreSQL (Metadata)
```sql
-- Good: Safe migration
BEGIN;
ALTER TABLE services ADD COLUMN IF NOT EXISTS environment VARCHAR(50) DEFAULT 'production';
CREATE INDEX IF NOT EXISTS idx_services_environment ON services(environment);
COMMIT;

-- Bad: Unsafe migration
ALTER TABLE services ADD COLUMN environment VARCHAR(50);
CREATE INDEX idx_services_environment ON services(environment);
```

## Migration Checklist

### Schema Changes
- [ ] New columns have defaults
- [ ] Renamed columns have migration path
- [ ] Removed columns are not referenced
- [ ] Data types are appropriate
- [ ] Constraints are valid

### Index Changes
- [ ] Indexes created concurrently
- [ ] Unused indexes identified for removal
- [ ] Composite indexes in correct order
- [ ] Index size is reasonable

### Data Migrations
- [ ] Batched for large tables
- [ ] Progress tracking implemented
- [ ] Rollback strategy defined
- [ ] Timeout is reasonable

### Performance
- [ ] No full table scans
- [ ] No table locks on large tables
- [ ] Migration runs in reasonable time
- [ ] No impact on running queries

## Exit Protocol
- ALL checks pass: Report "Migration verification passed"
- Syntax error: Report SQL error, STOP
- Missing rollback: Report migration ID, STOP
- Performance risk: Report estimated impact, STOP
- Compatibility issue: Report affected code, STOP

## Notes
- Run before applying any migration
- Test migrations on staging before production
- Keep migrations small and focused
- Document complex migrations
