# Verify Query Layer

Verify the Query Layer (`cluster/internal/api/`, `cluster/cmd/query/`) for correctness, security, and performance.

## Verification Steps

### 1. Build & Vet
```bash
cd cluster && go build ./... 2>&1
cd cluster && go vet ./... 2>&1
```

### 2. Unit Tests
```bash
cd cluster && go test -race -count=1 ./internal/api/... ./cmd/query/... 2>&1
```
Verify:
- REST endpoints return correct status codes and format
- GraphQL resolvers return correct data
- WebSocket message encoding/decoding
- Cache hit/miss behavior
- Rate limiting enforcement

### 3. API Contract Tests
- REST: OpenAPI spec compliance
- GraphQL: schema compliance
- WebSocket: binary protobuf frames
- SSE: event format and Last-Event-ID

### 4. Security Tests
- JWT validation (expired, invalid, missing tokens)
- RBAC enforcement (admin-only endpoints)
- Rate limiting (per-tenant limits, 429 returned)
- Input validation (SQL injection, XSS in filters)

### 5. Load Tests (k6)
- 10K concurrent WebSocket connections
- 1K requests/second REST
- p99 latency <100ms REST, <50ms cached

## Pass Criteria
- All tests pass with race detector
- API contract tests pass
- Security tests pass
- Load test meets targets
