# Verify Stream Engine

Verify the Stream Engine (`cluster/internal/stream/`) for correctness, exactly-once delivery, and schema compatibility.

## Verification Steps

### 1. Build & Vet
```bash
cd cluster && go build ./... 2>&1
cd cluster && go vet ./... 2>&1
```

### 2. Unit Tests
```bash
cd cluster && go test -race -count=1 ./internal/stream/... 2>&1
```
Verify:
- Topic creation with correct retention
- Idempotent producer deduplication
- Consumer group coordination
- Schema registry compatibility checks

### 3. Exactly-Once Verification
- Produce 10K messages with duplicates
- Consume and verify: exactly 10K unique messages
- No message loss, no duplicates processed

### 4. Schema Evolution Tests
- Add optional field: backward compatible
- Remove field: breaking change detected
- Enum value addition: backward compatible
- Breaking change: rejected by schema registry

### 5. Consumer Rebalance Tests
- Add/remove consumers during processing
- No message loss during rebalance
- Correct partition assignment after rebalance

## Pass Criteria
- All tests pass with race detector
- Exactly-once delivery verified
- Schema evolution rules enforced
- Consumer rebalance handled correctly
