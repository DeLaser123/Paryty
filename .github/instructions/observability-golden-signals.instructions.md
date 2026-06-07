---
description: "Go coding standards. Use when writing Go code."
applyTo: "**/*.go"
---

# Paryty Observability — Golden Signals and Dogfooding

Paryty monitors itself using its own SDK (locked decision). No Prometheus, Grafana, or competitor tooling.
OpenTelemetry is the observability standard (locked decision). Every component MUST expose golden signals.

## Golden Signals Per Component

### Agent (Rust)
| Signal | Type | Name | Description |
|---|---|---|---|
| Latency | Histogram | paryty.agent.grpc.send.duration_ms | Time to send one batch to cluster |
| Traffic | Counter | paryty.agent.metrics.collected.total | Total metrics scraped |
| Errors | Counter | paryty.agent.errors.total | Collection errors by type |
| Saturation | Gauge | paryty.agent.memory.bytes | Current memory usage |
| Saturation | Gauge | paryty.agent.edge_buffer.size | Edge buffer queue depth |
| Health | Gauge | paryty.agent.grpc.connected | 1=connected, 0=disconnected |
| Health | Gauge | paryty.agent.ebpf.active | 1=eBPF loaded, 0=fallback |

### Cluster — Ingestion (Go)
| Signal | Type | Name | Description |
|---|---|---|---|
| Latency | Histogram | paryty.cluster.ingestion.process.duration_ms | Validate + route one message |
| Traffic | Counter | paryty.cluster.ingestion.messages.total | Messages ingested by tenant, type |
| Errors | Counter | paryty.cluster.ingestion.errors.total | Validation errors, routing failures |
| Saturation | Gauge | paryty.cluster.ingestion.active_streams | Active gRPC streams |

### Cluster — Pipeline (Go)
| Signal | Type | Name | Description |
|---|---|---|---|
| Latency | Histogram | paryty.cluster.pipeline.stage.duration_ms | Per-stage processing time |
| Traffic | Counter | paryty.cluster.pipeline.messages.total | Messages processed per stage |
| Errors | Counter | paryty.cluster.pipeline.errors.total | Processing errors per stage |
| Saturation | Gauge | paryty.cluster.pipeline.queue.depth | Inbound queue depth |
| Saturation | Gauge | paryty.pipeline.dead_letter.size | Dead letter queue size |

### Cluster — Storage (Go)
| Signal | Type | Name | Description |
|---|---|---|---|
| Latency | Histogram | paryty.cluster.storage.hot.read.duration_ms | Dragonfly read latency |
| Latency | Histogram | paryty.cluster.storage.hot.write.duration_ms | Dragonfly write latency |
| Latency | Histogram | paryty.cluster.storage.warm.write.duration_ms | QuestDB ILP write latency |
| Latency | Histogram | paryty.cluster.storage.cold.write.duration_ms | SeaweedFS upload latency |
| Errors | Counter | paryty.cluster.storage.errors.total | Storage errors per tier |
| Saturation | Gauge | paryty.cluster.storage.hot.connections | Active Dragonfly connections |

### Cluster — Query (Go)
| Signal | Type | Name | Description |
|---|---|---|---|
| Latency | Histogram | paryty.cluster.query.duration_ms | API query latency |
| Traffic | Counter | paryty.cluster.query.requests.total | Requests by endpoint, method |
| Errors | Counter | paryty.cluster.query.errors.total | 4xx, 5xx by endpoint |
| Saturation | Gauge | paryty.cluster.query.active_connections | Active WebSocket connections |

### Intelligence Layer (Python)
| Signal | Type | Name | Description |
|---|---|---|---|
| Latency | Histogram | paryty.intelligence.prediction.duration_ms | Model inference latency |
| Traffic | Counter | paryty.intelligence.predictions.total | Predictions by model type |
| Errors | Counter | paryty.intelligence.errors.total | Prediction failures |
| Saturation | Gauge | paryty.intelligence.model.memory.bytes | Model memory usage |
| Health | Gauge | paryty.intelligence.model.version | Current model version |
| Health | Gauge | paryty.intelligence.drift.score | Current drift score 0-1 |

### Frontend (TypeScript)
| Signal | Type | Name | Description |
|---|---|---|---|
| Latency | Histogram | paryty.frontend.render.frame_ms | Frame render time |
| Latency | Histogram | paryty.frontend.api.duration_ms | API call latency |
| Traffic | Counter | paryty.frontend.api.calls.total | API calls by endpoint |
| Errors | Counter | paryty.frontend.errors.total | JS errors, WebGL context losses |
| Saturation | Gauge | paryty.frontend.gpu.memory.bytes | GPU memory usage |
| Saturation | Gauge | paryty.frontend.nodes.count | Current node count |

## Naming Convention

paryty.{component}.{subsystem}.{metric_name}
- All lowercase, dot-separated
- Suffix: _total for counters, _bytes for gauges, _duration_ms for histograms

## OpenTelemetry Requirements

1. All components use OpenTelemetry SDK (not language-native metrics)
2. Trace context propagation: trace_id flows Agent to Ingestion to Pipeline to Storage to Query to Frontend
3. Span attributes: tenant_id, service_name, component, version
4. Resource attributes: service.name, service.version, deployment.environment
5. Exporters: OTLP/gRPC to collector (Paryty own ingestion — dogfooding)

## Health Check Contract

Every component MUST expose:
- GET /healthz — 200 OK (liveness: I am alive)
- GET /readyz — 200 OK (readiness: I can serve traffic), 503 (not ready)

Readiness MUST check:
- Agent: gRPC connection to cluster
- Cluster Ingestion: Redpanda connectivity
- Cluster Pipeline: All stages initialized
- Cluster Storage: All tier connections healthy
- Cluster Query: Storage tier connections healthy
- Intelligence: Model loaded, version available
- Frontend: API endpoint reachable

