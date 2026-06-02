# Paryty Architecture Rules

## Component Boundaries

### Core Components
1. **Agent** (Rust) — Collection, eBPF observation, compression, gRPC streaming
2. **Cluster** (Go) — Ingestion, processing pipeline, storage tiers, query API
3. **Frontend** (TypeScript) — PixiJS visualization, React UI, Zustand state
4. **Intelligence Layer** (Python) — Forecasting, anomaly detection, drift detection, ML model management

### Communication Rules
- Agent <-> Cluster: gRPC bidirectional streaming only
- Cluster <-> Frontend: REST + WebSocket only
- Cluster <-> Intelligence Layer: gRPC unary + streaming (async Python side via grpc.aio)
- Go SDK <-> Cluster: gRPC client only
- No direct Agent <-> Frontend communication
- No direct Frontend <-> Intelligence Layer communication

### Cluster Layer Rules
```
Ingestion Layer: Stateless, protocol validation, tenant routing
    ↓
Stream Engine: Redpanda, durable, ordered
    ↓
Processing Pipeline: Single binary (aggregate → correlate → enrich in-process)
    ↓
Storage Layer: Dragonfly (hot), QuestDB (warm), SeaweedFS (cold)
    ↓
Query Layer: REST, WebSocket
```

- No skipping layers (Ingestion must go through Stream Engine)
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
- Service definitions in `proto/paryty/v1/`
- Request/Response: `{Service}{Method}Request/Response`
- Streaming for high-throughput data
- Unary for control plane operations

### Error Handling
```json
{
  "error": {
    "code": "RESOURCE_NOT_FOUND",
    "message": "Service not found",
    "details": { "service_id": "abc-123" }
  }
}
```

## Data Flow Rules

### Metrics Flow
```
Agent → gRPC → Ingestion → Redpanda → Pipeline → Storage → Query → Frontend
                                                    ↘ Intelligence Layer (Python ML services)
```

### No Shortcuts
- Agent cannot write directly to storage
- Frontend cannot read directly from storage
- Processing cannot bypass stream engine
- Frontend reads intelligence results via Cluster Query API (no direct ML service access)
- Intelligence Layer reads from Storage via Cluster (never from Redpanda directly)

### Multi-Tenancy
- Every request must include tenant_id
- Data isolation at storage level
- No cross-tenant data leakage

## Security Rules
- API key for agent authentication
- JWT for frontend authentication
- mTLS for cluster-to-cluster and cluster-to-intelligence communication
- RBAC: Admin / Operator / Viewer
- TLS for all network communication
- Encryption at rest for sensitive data
- No credentials in code or logs
- Audit logging for all mutations

## Performance Rules

### Latency Budgets
- Hot store (Dragonfly): < 1ms
- Warm store (QuestDB): < 50ms
- Cold store (SeaweedFS): < 1s
- API query: < 100ms
- Frontend render: < 16ms (60fps)
- ML prediction (single): < 100ms
- ML prediction (batch): < 1s

### Resource Limits
- Agent: < 50MB memory, < 2% CPU
- Cluster node: < 4GB memory
- Frontend: < 512MB GPU memory
- Intelligence Layer: < 2GB memory

### Optimization Requirements
- No N+1 queries
- Pagination for large result sets
- Caching at query layer (Dragonfly, TTL 5min)
- Zstd compression for network transfer

## Testing Rules
- Unit tests: 80% minimum coverage
- Integration tests: 70% minimum
- Critical paths: 90% minimum
- No shared state between tests
- Use fixtures for deterministic data
- Mock external dependencies
- Test naming: `Test{Function}_{Scenario}_{Expected}`
