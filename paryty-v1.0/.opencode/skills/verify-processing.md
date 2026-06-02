# Verify Processing Layer

Verify the Processing Layer (`cluster/internal/processing/`) for correctness, idempotency, and performance.

## Verification Steps

### 1. Build & Vet
```bash
cd cluster && go build ./... 2>&1
cd cluster && go vet ./... 2>&1
```

### 2. Unit Tests
```bash
cd cluster && go test -race -count=1 ./internal/processing/... 2>&1
```
Verify:
- Aggregation correctness (sum, avg, min, max, p50, p95, p99)
- Window boundary handling
- Late data handling (within/beyond lateness)
- Correlation accuracy
- Enrichment from cache

### 3. Idempotency Tests
- Process same message twice: output identical
- No side effects duplicated
- Aggregate: sum of per-minute == per-hour (within rounding)

### 4. Windowing Tests
- Tumbling window boundaries correct
- Watermark-based processing: window not processed early
- Late data: accepted within 5-minute lateness, dropped after

### 5. Benchmarks
- Aggregation: >100K metrics/second per core
- Correlation: >10K traces/second per core
- Enrichment: >50K lookups/second per core

## Pass Criteria
- All tests pass with race detector
- Idempotency verified
- Windowing correct
- All benchmarks within budget
