# Test Performance — Performance Testing and Benchmarking

## Purpose
Run performance tests and benchmarks to ensure Paryty meets latency, throughput, and resource utilization targets. Includes load testing, stress testing, benchmark regression detection, and profiling.

## Prerequisites
Infrastructure must be running:
```bash
podman-compose -f deploy/compose/docker-compose.dev.yaml up -d
```

Install tools:
```bash
# k6 for load testing
winget install k6
# Or: go install go.k6.io/k6@latest
```

## Performance Targets

### Latency Targets
| Operation | Target | Measurement |
|-----------|--------|-------------|
| Hot store read | < 1ms p99 | Dragonfly GET |
| Hot store write | < 2ms p99 | Dragonfly SET |
| Warm store query | < 50ms p99 | QuestDB time-range |
| Cold store read | < 1s p99 | SeaweedFS S3 GET |
| gRPC ingestion | < 5ms p99 | Per-metric ingest |
| API query | < 100ms p99 | REST endpoint |

### Throughput Targets
| Component | Target | Measurement |
|-----------|--------|-------------|
| Ingestion | 100K metrics/sec | Per ingestion node |
| Stream produce | 50K msg/sec | Per Redpanda topic |
| Stream consume | 50K msg/sec | Per consumer group |
| Storage write | 20K writes/sec | Per Dragonfly node |

### Resource Targets
| Resource | Target | Measurement |
|----------|--------|-------------|
| Agent memory | < 50MB RSS | Per agent instance |
| Agent CPU | < 2% idle | Per agent instance |
| Cluster memory | < 4GB per node | Per ingestion node |
| Frontend memory | < 512MB GPU | GPU memory budget |

## Execution Steps

### Step 1: Run Rust Benchmarks
Run: cd agent && cargo bench 2>&1
Expected: All benchmarks within 10% of baseline. Report regressions.

### Step 2: Run Go Benchmarks
Run: cd cluster && go test -bench=. -benchmem -count=3 ./... 2>&1
Expected: No allocations in hot paths. Report ns/op and B/op.

### Step 3: Load Test Ingestion
Run: k6 run scripts/perf/ingestion-load-test.js 2>&1
Expected: 100K metrics/sec sustained, p99 < 5ms, error rate < 0.1%.

### Step 4: Stress Test Query Layer
Run: k6 run scripts/perf/query-stress-test.js 2>&1
Expected: 1000 concurrent queries, p99 < 100ms, error rate < 1%.

### Step 5: Profile Memory (Go)
Run: cd cluster && go test -memprofile=mem.prof ./internal/storage/...
Run: go tool pprof -http=:8081 mem.prof
Expected: No memory leaks, bounded allocations.

### Step 6: Profile CPU (Go)
Run: cd cluster && go test -cpuprofile=cpu.prof ./internal/processing/...
Run: go tool pprof -http=:8082 cpu.prof
Expected: No unexpected CPU spikes, bounded processing time.

### Step 7: Profile Memory (Rust)
Run: cd agent && cargo instruments -t time --bench metal_bench 2>&1
Expected: No memory leaks, bounded allocations.

## k6 Load Test Script Template
```javascript
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 100 },  // Ramp up
    { duration: '1m', target: 1000 },  // Sustained load
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(99)<100'],
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  const res = http.get('http://localhost:8080/api/topology');
  check(res, {
    'status is 200': (r) => r.status === 200,
    'duration < 100ms': (r) => r.timings.duration < 100,
  });
  sleep(0.1);
}
```

## Exit Protocol
- ALL targets met: Report "All performance targets met"
- Latency regression: Report which operation regressed, old vs new, STOP
- Throughput below target: Report which component, measured vs target, STOP
- Memory leak detected: Report allocation site and growth rate, STOP
- CPU regression: Report hot path and time increase, STOP
- Error rate > threshold: Report error type and rate, STOP

## Notes
- Benchmarks should run on idle system for consistent results
- Run each benchmark 3+ times and report median
- k6 tests require infrastructure running
- Profile files can be analyzed with `go tool pprof` or `cargo instruments`
