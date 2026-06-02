# Verify Observability — OpenTelemetry Instrumentation Verification

## Purpose
Validate OpenTelemetry instrumentation, metrics collection, trace propagation, and log correlation across all Paryty components.

## Execution Steps

### Step 1: Verify OpenTelemetry SDK Configuration
Check: OTel SDK initialized in all services
Check: Resource attributes set (service.name, service.version)
Check: Exporters configured (OTLP, Prometheus)

### Step 2: Verify Metrics Collection
Check: All critical metrics are instrumented
Check: Metric names follow conventions
Check: Labels are consistent
Check: No high-cardinality labels

### Step 3: Verify Trace Propagation
Check: Trace context propagated across services
Check: Span attributes include relevant context
Check: Span events for important operations
Check: Parent-child relationships correct

### Step 4: Verify Log Correlation
Check: Logs include trace_id and span_id
Check: Log levels are appropriate
Check: Structured logging format (JSON)
Check: No sensitive data in logs

### Step 5: Verify Dashboards
Check: Grafana dashboards exist for critical metrics
Check: Dashboard panels have correct queries
Check: Alert rules configured

### Step 6: Verify Health Endpoints
Check: /healthz endpoint exists
Check: /readyz endpoint exists
Check: Health checks include dependency status

## Paryty-Specific Instrumentation

### Agent (Rust)
```rust
// Required metrics
- paryty_agent_metrics_collected_total (Counter)
- paryty_agent_metrics_sent_total (Counter)
- paryty_agent_buffer_size (Gauge)
- paryty_agent_grpc_connection_status (Gauge)
- paryty_agent_collection_duration_seconds (Histogram)
```

### Cluster - Ingestion (Go)
```go
// Required metrics
- paryty_ingestion_requests_total (Counter)
- paryty_ingestion_active_connections (Gauge)
- paryty_ingestion_request_duration_seconds (Histogram)
- paryty_ingestion_errors_total (Counter)
- paryty_ingestion_bytes_received_total (Counter)
```

### Cluster - Processing (Go)
```go
// Required metrics
- paryty_processing_messages_processed_total (Counter)
- paryty_processing_duration_seconds (Histogram)
- paryty_processing_queue_size (Gauge)
- paryty_processing_errors_total (Counter)
```

### Cluster - Storage (Go)
```go
// Required metrics
- paryty_storage_operations_total (Counter)
- paryty_storage_operation_duration_seconds (Histogram)
- paryty_storage_errors_total (Counter)
- paryty_storage_connection_pool_size (Gauge)
```

### Frontend (TypeScript)
```typescript
// Required metrics
- paryty_frontend_page_load_duration_seconds (Histogram)
- paryty_frontend_api_request_duration_seconds (Histogram)
- paryty_frontend_webgpu_fps (Gauge)
- paryty_frontend_node_count (Gauge)
```

## Metric Naming Conventions
```
paryty_{component}_{metric_name}_{unit}
```

### Labels
- `tenant_id`: Multi-tenant isolation
- `service_name`: Service identifier
- `environment`: dev/staging/prod
- `instance`: Instance identifier

## Trace Propagation

### W3C Trace Context
```
traceparent: 00-{trace_id}-{span_id}-{flags}
tracestate: paryty={tenant_id}
```

### Required Spans
- Agent: collect, send, buffer
- Ingestion: receive, validate, route
- Processing: aggregate, correlate, enrich
- Storage: read, write, query
- Frontend: render, api_call

## Log Format
```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "level": "info",
  "message": "Metric processed",
  "service": "ingestion",
  "trace_id": "abc123",
  "span_id": "def456",
  "tenant_id": "tenant-1",
  "metric_count": 100
}
```

## Exit Protocol
- ALL checks pass: Report "Observability verification passed"
- Missing metric: Report component and metric name, STOP
- Trace break: Report service boundary, STOP
- Log format issue: Report component, STOP
- Missing health endpoint: Report service, STOP

## Notes
- Observability is critical for production debugging
- All new code must include instrumentation
- Dashboard changes require review
- Alert rules require on-call team approval
