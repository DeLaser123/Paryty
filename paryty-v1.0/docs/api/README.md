# Paryty API Reference

## Base URL

| Environment | URL |
|-------------|-----|
| Development | `http://localhost:8080` |
| Production | `https://api.paryty.io` |

## Authentication

All endpoints (except login/register) require a JWT Bearer token in the `Authorization` header.

```
Authorization: Bearer <jwt_token>
```

Obtain tokens via `POST /api/v1/auth/login`. Tokens expire after 24 hours; use `POST /api/v1/auth/refresh` to extend.

---

## REST Endpoints

### Auth

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `POST` | `/api/v1/auth/register` | Create account | No |
| `POST` | `/api/v1/auth/login` | Authenticate, returns JWT | No |
| `POST` | `/api/v1/auth/refresh` | Refresh access token | Yes |
| `POST` | `/api/v1/auth/logout` | Revoke tokens | Yes |
| `POST` | `/api/v1/auth/change-password` | Change password | Yes |
| `POST` | `/api/v1/auth/forgot-password` | Request password reset | No |
| `POST` | `/api/v1/auth/reset-password` | Reset password with token | No |
| `POST` | `/api/v1/auth/verify-email` | Verify email address | No |

### Twins (Digital Twins)

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/api/v1/twins` | List all twins for tenant | Yes |
| `POST` | `/api/v1/twins` | Create a new twin | Yes |
| `GET` | `/api/v1/twins/:id` | Get twin by ID | Yes |
| `PUT` | `/api/v1/twins/:id` | Update twin | Yes |
| `DELETE` | `/api/v1/twins/:id` | Delete twin | Yes |

### Metrics

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/api/v1/metrics/:agent_id` | Get metrics for agent | Yes |
| `POST` | `/api/v1/metrics/query` | Query metrics with filters | Yes |

### Alerts

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/api/v1/alerts` | List alerts | Yes |
| `POST` | `/api/v1/alerts/:id/acknowledge` | Acknowledge alert | Yes |

### Agents

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/api/v1/agents` | List connected agents | Yes |
| `GET` | `/api/v1/agents/download/:platform` | Download agent binary | Yes |

Supported platforms: `linux-amd64`, `linux-arm64`, `windows-amd64`, `darwin-amd64`, `darwin-arm64`.

### Timeline

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/api/v1/timeline/snapshots` | List available snapshots | Yes |
| `GET` | `/api/v1/timeline/replay` | SSE stream for timeline replay | Yes |

### Intelligence

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `POST` | `/api/v1/intel/forecast` | Get metric forecast | Yes |
| `POST` | `/api/v1/intel/anomalies/detect` | Detect anomalies in metrics | Yes |
| `GET` | `/api/v1/anomalies` | List detected anomalies | Yes |
| `GET` | `/api/v1/intel/models/accuracy` | Get model accuracy metrics | Yes |
| `POST` | `/api/v1/intel/models/retrain` | Trigger model retraining | Yes |

### Health

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/healthz` | Liveness probe | No |
| `GET` | `/readyz` | Readiness probe | No |

---

## WebSocket

### Connection

```
ws://localhost:8080/api/v1/ws
wss://api.paryty.io/api/v1/ws
```

**Query Parameters:**
- `token` — JWT Bearer token (required)

### Message Format

All messages use JSON:

```json
{
  "type": "topology_update" | "metric_update" | "alert_update" | "agent_status",
  "data": { ... },
  "timestamp": "2026-06-20T22:30:47Z"
}
```

### Event Types

| Event Type | Description | Data Shape |
|------------|-------------|------------|
| `topology_update` | Node/edge added, removed, or changed | `{ nodes: [...], edges: [...] }` |
| `metric_update` | New metric data point | `{ agent_id, metric_name, value, timestamp }` |
| `alert_update` | Alert triggered or resolved | `{ alert_id, severity, message, status }` |
| `agent_status` | Agent connected/disconnected | `{ agent_id, status, last_seen }` |

---

## SSE (Server-Sent Events)

### Timeline Replay

```
GET /api/v1/timeline/replay?snapshot_id=<id>&speed=1x
```

**Query Parameters:**
- `snapshot_id` — Snapshot to replay (required)
- `speed` — Replay speed: `0.25x`, `0.5x`, `1x`, `2x`, `4x`, `8x`, `16x` (default: `1x`)

**Event Format:**
```
data: {"type":"topology_update","data":{...},"timestamp":"..."}
```

---

## Error Format

All errors follow a consistent format:

```json
{
  "error": "ERROR_CODE",
  "message": "Human-readable description"
}
```

### Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `UNAUTHORIZED` | 401 | Missing or invalid token |
| `FORBIDDEN` | 403 | Insufficient permissions |
| `NOT_FOUND` | 404 | Resource not found |
| `VALIDATION_ERROR` | 400 | Invalid request body |
| `CONFLICT` | 409 | Resource already exists |
| `RATE_LIMITED` | 429 | Too many requests |
| `INTERNAL_ERROR` | 500 | Server error |

---

## Rate Limiting

- **Default:** 100 requests per minute per tenant
- **Headers:** `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`
- **On limit:** `429 Too Many Requests` with `Retry-After` header
- **Backend:** Dragonfly-backed sliding window counter

---

## Pagination

List endpoints support cursor-based pagination:

```
GET /api/v1/twins?cursor=<id>&limit=50
```

**Response:**
```json
{
  "data": [...],
  "cursor": "next-page-cursor",
  "has_more": true
}
```

---

## Multi-Tenancy

All data is scoped to the tenant identified in the JWT token. Cross-tenant access is prohibited at the storage layer. The `tenant_id` is extracted from the JWT claims and enforced on every request.

---

## gRPC (Internal)

Internal services communicate via gRPC. Proto definitions are in `proto/paryty/v1/`.

| Service | Purpose | Protocol |
|---------|---------|----------|
| `AgentService` | Agent → Cluster streaming | gRPC bidirectional |
| `IngestionService` | Ingestion → Pipeline routing | gRPC unary |
| `IntelligenceService` | Cluster → ML services | gRPC unary + streaming |
| `GoSDKService` | Application → Cluster | gRPC client |
