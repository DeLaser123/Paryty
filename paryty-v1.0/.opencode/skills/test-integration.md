# Test Integration — Cross-Service Integration Testing

## Purpose
Run integration tests that verify interactions between Paryty components: Agent-Cluster communication, Cluster-Storage integration, Stream processing, and API contract compliance.

## Prerequisites
Infrastructure must be running:
```bash
podman-compose -f deploy/compose/docker-compose.dev.yaml up -d
```

## Execution Steps

### Step 1: Verify Infrastructure Health
Run: curl -s http://localhost:9644/v1/status/ready (Redpanda)
Run: redis-cli -h localhost -p 6379 ping (Dragonfly)
Run: curl -s http://localhost:9009/status (QuestDB)
Expected: All services respond healthy.

### Step 2: Agent-Cluster Integration
Run: go test -v -tags=integration -run TestAgentCluster ./internal/api/... 2>&1
Expected: Agent can connect via gRPC, stream metrics, handle reconnection.

### Step 3: Stream Integration (Redpanda)
Run: go test -v -tags=integration -run TestStream ./internal/stream/... 2>&1
Expected: Messages produced and consumed correctly, exactly-once delivery verified.

### Step 4: Storage Integration (Dragonfly + QuestDB)
Run: go test -v -tags=integration -run TestStorage ./internal/storage/... 2>&1
Expected: Hot store writes/reads < 1ms, warm store writes/reads < 50ms.

### Step 5: Processing Pipeline Integration
Run: go test -v -tags=integration -run TestPipeline ./internal/processing/... 2>&1
Expected: Ingestion -> Stream -> Processing -> Storage pipeline completes end-to-end.

### Step 6: API Contract Tests
Run: go test -v -tags=integration -run TestAPI ./internal/api/... 2>&1
Expected: REST responses match proto-derived schemas.

### Step 7: Frontend-Backend Integration
Run: npx vitest run --reporter=verbose src/api/ 2>&1
Expected: API client correctly handles all cluster endpoints.

## Test Scenarios

### Agent-Cluster Scenarios
1. **Happy path**: Agent connects, streams metrics for 60s, disconnects
2. **Reconnection**: Agent loses connection, reconnects with exponential backoff
3. **Edge buffer**: Agent buffers during disconnect, flushes on reconnect
4. **Backpressure**: Cluster applies backpressure, agent respects flow control
5. **Multi-tenant**: Two agents on different tenants, data isolated

### Stream Scenarios
1. **Produce-consume**: Metrics produced to topic, consumed by processor
2. **Exactly-once**: Duplicate messages deduplicated within window
3. **Retention**: Messages older than retention period are deleted
4. **Schema evolution**: New field added, old consumers still work

### Storage Scenarios
1. **Hot store TTL**: Keys expire after configured TTL
2. **Warm store queries**: Time-range queries return correct results
3. **Cold store lifecycle**: Data moves from warm to cold after retention
4. **Cross-tier query**: Query spans hot and warm stores correctly

## Exit Protocol
- ALL scenarios pass: Report "All integration tests passed"
- Infrastructure not ready: Report which service is down, STOP
- Agent-Cluster fails: Report connection error details, STOP
- Stream fails: Report topic/offset and error, STOP
- Storage fails: Report store type and latency/error, STOP
- Contract mismatch: Report expected vs actual schema, STOP

## Notes
- Integration tests are slow (30s-2min each), use `-run` to select specific tests
- Tests require infrastructure running via docker-compose.dev.yaml
- Use `-tags=integration` to include integration tests
- Tests clean up after themselves but may leave stale data on failure
