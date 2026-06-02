# Verify Ingestion Layer

Verify the Ingestion Layer (`cluster/cmd/ingestion/`, `cluster/internal/api/`) for correctness, scalability, and security.

## Verification Steps

### 1. Build & Vet
```bash
cd cluster && go build ./... 2>&1
cd cluster && go vet ./... 2>&1
```

### 2. Unit Tests
```bash
cd cluster && go test -race -count=1 ./cmd/ingestion/... ./internal/api/... 2>&1
```
Verify:
- gRPC server accepts connections
- Protocol validation rejects invalid messages
- Tenant routing correct
- Rate limiting enforced per tenant

### 3. Load Tests (k6)
- 10K concurrent connections sustained
- 100K messages/second throughput
- p99 latency <10ms for message acceptance
- Graceful degradation under overload (503 returned)

### 4. Security Tests
- API key validation works
- JWT token validation works
- Tenant isolation (one tenant cannot see another's data)
- Rate limiting enforcement (429 returned when exceeded)

### 5. Graceful Shutdown Test
- Drain all active connections within 30s
- No messages dropped during shutdown
- GOAWAY sent on gRPC

## Pass Criteria
- All tests pass with race detector
- Load test meets targets
- Security tests pass
- Graceful shutdown verified
