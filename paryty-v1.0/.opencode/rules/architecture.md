# Paryty Architecture Rules

## Component Boundaries

### Three-Part Architecture
1. **Agent** (Rust) — Collection, compression, streaming
2. **Cluster** (Go) — Ingestion, processing, storage, query
3. **Frontend** (TypeScript) — Visualization, intelligence layer

### Communication Rules
- Agent <-> Cluster: gRPC bidirectional streaming only
- Cluster <-> Frontend: REST + WebSocket only
- Go SDK <-> Cluster: gRPC client only
- No direct Agent <-> Frontend communication

### Layer Rules (Cluster)
```
Ingestion Layer: Stateless, protocol validation, tenant routing
    ↓
Stream Engine: Redpanda, durable, ordered
    ↓
Processing Layer: Aggregator, Correlator, Enricher
    ↓
Storage Layer: Dragonfly (hot), QuestDB (warm), SeaweedFS (cold)
    ↓
Query Layer: REST, WebSocket, GraphQL
```

- No skipping layers (e.g., Ingestion must go through Stream)
- No circular dependencies between layers
- Each layer has a single responsibility

## API Design Rules

### REST Conventions
```
GET    /api/v1/topology          — List topology
GET    /api/v1/topology/:id      — Get service
POST   /api/v1/topology          — Create service
PUT    /api/v1/topology/:id      — Update service
DELETE /api/v1/topology/:id      — Delete service

GET    /api/v1/metrics?range=1h  — Get metrics
GET    /api/v1/traces/:trace_id  — Get trace

GET    /healthz                   — Liveness
GET    /readyz                    — Readiness
```

### gRPC Conventions
- Service definitions in proto files
- Request/Response messages named: `{Service}{Method}Request/Response`
- Streaming for high-throughput data
- Unary for control plane operations

### Error Handling
```json
{
  "error": {
    "code": "RESOURCE_NOT_FOUND",
    "message": "Service not found",
    "details": {
      "service_id": "abc-123"
    }
  }
}
```

## Data Flow Rules

### Metrics Flow
```
Agent -> gRPC -> Ingestion -> Redpanda -> Processing -> Storage -> Query -> Frontend
```

### No Shortcuts
- Agent cannot write directly to storage
- Frontend cannot read directly from storage
- Processing cannot bypass stream engine

### Multi-Tenancy
- Every request must include tenant_id
- Data isolation at storage level
- No cross-tenant data leakage

## Security Rules

### Authentication
- API key for agent authentication
- JWT for frontend authentication
- mTLS for cluster-to-cluster communication

### Authorization
- RBAC for frontend access
- Tenant isolation for data access
- Role-based permissions (admin, viewer, operator)

### Data Protection
- TLS for all network communication
- Encryption at rest for sensitive data
- No credentials in code or logs
- Audit logging for all mutations

## Performance Rules

### Latency Budgets
- Hot store: < 1ms
- Warm store: < 50ms
- Cold store: < 1s
- API query: < 100ms
- Frontend render: < 16ms (60fps)

### Resource Limits
- Agent: < 50MB memory, < 2% CPU
- Cluster node: < 4GB memory
- Frontend: < 512MB GPU memory

### Optimization Requirements
- No N+1 queries
- Pagination for large result sets
- Caching at query layer
- Compression for network transfer

## Testing Rules

### Coverage Requirements
- Unit tests: 80% minimum
- Integration tests: 70% minimum
- Critical paths: 90% minimum

### Test Isolation
- No shared state between tests
- Cleanup after each test
- Use fixtures for deterministic data
- Mock external dependencies

### Test Naming
```
Test{Function}_{Scenario}_{Expected}
Example: TestAggregation_EmptyInput_ReturnsZero
```
