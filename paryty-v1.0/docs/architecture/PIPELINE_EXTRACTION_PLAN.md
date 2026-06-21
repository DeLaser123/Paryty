# Pipeline Stage Extraction Plan

## Current State

The Aggregator, Correlator, and Enricher run as in-process goroutines within a single Go binary (`pipeline` service). This is the Phase 3 locked decision — single binary with in-process stages connected by Go channels.

```
┌─────────────────────────────────────┐
│           Pipeline Binary           │
│                                     │
│  Redpanda Consumer                  │
│       ↓                             │
│  ┌───────────┐                      │
│  │ Aggregator │ (goroutine pool)    │
│  └─────┬─────┘                      │
│        ↓ (Go channel)              │
│  ┌───────────┐                      │
│  │ Correlator │ (goroutine pool)    │
│  └─────┬─────┘                      │
│        ↓ (Go channel)              │
│  ┌───────────┐                      │
│  │  Enricher  │ (goroutine pool)    │
│  └─────┬─────┘                      │
│        ↓                             │
│  Redpanda Producer + Storage Writers │
└─────────────────────────────────────┘
```

**Advantages of current design:**
- Zero network hops between stages
- Single deployment unit
- Simple debugging (all in one process)
- Low latency (in-process channel communication)

---

## Extraction Trigger

Extract stages into separate services when ANY of the following thresholds are met:

| Metric | Threshold | Current |
|--------|-----------|---------|
| Pipeline binary memory | > 4 GB RSS | TBD |
| Processing lag | > 5 minutes sustained | TBD |
| Single stage CPU | > 80% sustained (scaling blocked) | TBD |
| Tenant count requiring isolation | > 50 tenants with SLA requirements | TBD |

**Monitoring:** Track `paryty.pipeline.process.duration_ms` and `paryty.pipeline.queue.depth` via OpenTelemetry metrics.

---

## Extraction Architecture

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│  Aggregator      │     │  Correlator      │     │  Enricher        │
│  (Consumer Group)│     │  (Consumer Group)│     │  (Consumer Group)│
│                  │     │                  │     │                  │
│  In: raw metrics │     │  In: aggregated  │     │  In: correlated  │
│  Out: aggregated │     │  Out: correlated │     │  Out: enriched   │
└────────┬─────────┘     └────────┬─────────┘     └────────┬─────────┘
         │                        │                         │
         ↓                        ↓                         ↓
  paryty.{tenant}.     paryty.{tenant}.            paryty.{tenant}.
  metrics.aggregated   metrics.correlated          metrics.enriched
```

---

## Extraction Steps

### Step 1: Define Inter-Stage Message Format

Create a unified `PipelineMessage` protobuf that all stages consume and produce:

```protobuf
// proto/paryty/v1/pipeline.proto
syntax = "proto3";
package paryty.v1;

message PipelineMessage {
  string tenant_id = 1;
  string message_id = 2;
  MessageType type = 3;
  bytes payload = 4;
  map<string, string> metadata = 5;
  int64 created_at = 6;
  int32 stage_version = 7;
}

enum MessageType {
  MESSAGE_TYPE_UNSPECIFIED = 0;
  MESSAGE_TYPE_METRIC = 1;
  MESSAGE_TYPE_TRACE = 2;
  MESSAGE_TYPE_EVENT = 3;
  MESSAGE_TYPE_TOPOLOGY = 4;
}
```

### Step 2: Add Redpanda Topics

New inter-stage topics:

```
paryty.{tenant}.pipeline.aggregated   — Aggregator output
paryty.{tenant}.pipeline.correlated   — Correlator output
paryty.{tenant}.pipeline.enriched     — Enricher output (final)
```

### Step 3: Implement Feature Flag

Add environment variable to control pipeline mode:

```bash
# Single binary (default, current behavior)
PARYTY_PIPELINE_MODE=single

# Distributed (each stage is separate consumer group)
PARYTY_PIPELINE_MODE=distributed
```

Implementation in `cluster/internal/pipeline/mode.go`:

```go
type PipelineMode string

const (
    PipelineModeSingle     PipelineMode = "single"
    PipelineModeDistributed PipelineMode = "distributed"
)

func GetPipelineMode() PipelineMode {
    mode := os.Getenv("PARYTY_PIPELINE_MODE")
    if mode == string(PipelineModeDistributed) {
        return PipelineModeDistributed
    }
    return PipelineModeSingle
}
```

### Step 4: Implement Distributed Mode

When `PARYTY_PIPELINE_MODE=distributed`:

1. Each stage becomes a separate binary with its own consumer group
2. Each stage reads from its input topic and writes to its output topic
3. Consumer groups: `paryty-pipeline-aggregator`, `paryty-pipeline-correlator`, `paryty-pipeline-enricher`

### Step 5: Update HPA Configuration

```yaml
# deploy/helm/paryty/templates/hpa-pipeline.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: paryty-pipeline-aggregator
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: paryty-pipeline-aggregator
  minReplicas: 2
  maxReplicas: 20
  metrics:
    - type: Pods
      pods:
        metric:
          name: paryty_pipeline_queue_depth
        target:
          type: AverageValue
          averageValue: "1000"
```

---

## Rollout Plan

| Phase | Action | Risk |
|-------|--------|------|
| 1 | Deploy `single` mode with `PipelineMessage` format | Low — no behavior change |
| 2 | Add inter-stage Redpanda topics (empty) | Low — no consumers yet |
| 3 | Deploy `distributed` mode alongside `single` (shadow) | Medium — validate output matches |
| 4 | Switch traffic to `distributed` mode | Medium — monitor latency/throughput |
| 5 | Remove `single` mode code | Low — cleanup |

---

## Rollback

If distributed mode causes issues:

1. Set `PARYTY_PIPELINE_MODE=single`
2. Restart pipeline pods
3. Inter-stage topics remain (no data loss) but are unused

---

## Performance Budgets (Post-Extraction)

| Metric | Budget |
|--------|--------|
| End-to-end processing latency | < 10 seconds (p99) |
| Per-stage processing latency | < 3 seconds (p99) |
| Message ordering | Preserved per tenant (Redpanda partition key = tenant_id) |
| At-least-once delivery | Yes (idempotent stage logic) |
