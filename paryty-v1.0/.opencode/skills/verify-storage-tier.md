# Verify Storage Tier

Verify the Storage Tier (`cluster/internal/storage/`) for correctness, latency targets, and data lifecycle.

## Verification Steps

### 1. Build & Vet
```bash
cd cluster && go build ./... 2>&1
cd cluster && go vet ./... 2>&1
```

### 2. Unit Tests
```bash
cd cluster && go test -race -count=1 ./internal/storage/... 2>&1
```
Verify:
- Hot store (Dragonfly): TTL eviction, pipeline batch, Lua scripts
- Warm store (QuestDB): ILP ingestion, SQL queries, dedup
- Cold store (SeaweedFS): Parquet upload, S3 API, partitioning
- Lifecycle manager: promotion, archival, retention

### 3. Latency Tests
- Hot store: <1ms read/write
- Warm store: <50ms query
- Cold store: <1s upload

### 4. Data Lifecycle Tests
- Data written to hot appears in warm after promotion
- Data in warm archived to cold after 90 days
- Data in cold deleted after 7 years

### 5. Tenant Isolation Tests
- Tenant A cannot read Tenant B's data
- Separate key prefixes, partitions, and paths

### 6. Failover Tests
- Dragonfly unavailable: buffered and retried
- QuestDB unavailable: buffered and retried
- SeaweedFS unavailable: buffered and retried

## Pass Criteria
- All tests pass with race detector
- Latency targets met
- Data lifecycle correct
- Tenant isolation verified
- Failover behavior correct
