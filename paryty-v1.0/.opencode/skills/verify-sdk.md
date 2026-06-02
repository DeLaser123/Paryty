# Verify Go SDK

Verify the Go SDK (`agent/go_sdk/`) for correctness, performance, and OpenTelemetry compatibility.

## Verification Steps

### 1. Build & Vet
```bash
cd agent/go_sdk && go build ./... 2>&1
cd agent/go_sdk && go vet ./... 2>&1
```

### 2. Unit Tests
```bash
cd agent/go_sdk && go test -race -count=1 ./... 2>&1
```
Verify:
- Metric recording (counters, gauges, histograms)
- Batch flush behavior (size trigger: 100 items, time trigger: 50ms)
- Context propagation round-trip (W3C TraceContext)
- Reconnection behavior (exponential backoff)
- Graceful shutdown (all data flushed within 30s)

### 3. OTel Compatibility Tests
- OTLP exporter sends traces and metrics correctly
- OTel spans bridge to Paryty traces
- OTel metrics bridge to Paryty metrics
- Semantic conventions aligned

### 4. Benchmarks
```bash
cd agent/go_sdk && go test -bench=. -benchmem ./... 2>&1
```
- Counter increment: <100ns
- Span creation: <500ns
- Batch flush: >10K items/second
- Memory overhead: <10MB for 100K buffered items

### 5. Security Audit
- [ ] TLS by default
- [ ] No credentials in logs
- [ ] No sensitive data in metric labels or span attributes
- [ ] API key not in source code

### 6. Runtime Validation
- SDK adds <1ms latency to host application
- Batch flush happens at configured interval
- Reconnection succeeds after network partition
- Graceful shutdown flushes all data

## Pass Criteria
- All tests pass with race detector
- OTel compatibility verified
- All benchmarks within budget
- Security checklist clean
- No impact on host application performance
