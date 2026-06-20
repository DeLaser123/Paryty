---
description: Run performance benchmarks and check against budgets across all components
agent: performance-engineer
---
# Performance Benchmark — Paryty Performance Analysis

Run benchmarks and verify against Paryty performance budgets.

## Budgets to Check

### Latency Targets
| Component | Target |
|---|---|
| Hot store (Dragonfly) | < 1ms p99 |
| Warm store (QuestDB) | < 50ms p99 |
| Cold store (SeaweedFS) | < 1s p99 |
| API query | < 100ms p99 |
| Frontend render | < 16ms (60fps) |
| Agent → Cluster | < 5ms gRPC |

### Resource Budgets
| Component | Memory | CPU |
|---|---|---|
| Agent | < 50MB | < 2% |
| Cluster node | < 4GB | - |
| Frontend GPU | < 512MB | - |

### Throughput Targets
| Operation | Target |
|---|---|
| Ingestion | > 100K metrics/sec |
| Aggregation | > 100K metrics/sec/core |
| Hot store write | > 100K ops/sec |
| Hot store read | > 200K ops/sec |

## Execution

### Rust Benchmarks
```bash
cd paryty-v1.0/agent && cargo bench 2>&1
```

### Go Benchmarks
```bash
cd paryty-v1.0/cluster && go test -bench=. -benchmem -count=3 ./internal/... 2>&1
```

### Frontend Benchmarks
```bash
cd paryty-v1.0/frontend && npm run bench 2>&1
```

## Reporting Format
```
Benchmark: [Name]
  Before: [X] ops/sec, [Y] B/op, [Z] allocs/op
  After:  [X] ops/sec, [Y] B/op, [Z] allocs/op
  Budget: [pass/fail]
  Improvement: +N% throughput, -N% allocations
  Method: [what changed]
```

## Red Flags
Any latency target exceeded by >2x, memory leak detected, throughput regression >10%, heap allocation in hot path, frame rate drops below 30fps.
