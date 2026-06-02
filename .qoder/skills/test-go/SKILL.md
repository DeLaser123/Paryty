---
name: test-go
description: Run comprehensive Go tests for Paryty Cluster — unit tests, race detection, integration tests, benchmarks, coverage, and escape analysis.
---

# Test Go — Comprehensive Go Testing

## Execution Steps

### Step 1: Unit Tests
```bash
cd paryty-v1.0/cluster && go test -v -count=1 ./... 2>&1
```

### Step 2: Race Detection
```bash
cd paryty-v1.0/cluster && go test -race -count=1 ./... 2>&1
```

### Step 3: Benchmarks
```bash
cd paryty-v1.0/cluster && go test -bench=. -benchmem -count=3 ./... 2>&1
```
Report any allocations in hot paths.

### Step 4: Coverage Report
```bash
cd paryty-v1.0/cluster && go test -coverprofile=coverage.out -covermode=atomic ./... 2>&1
cd paryty-v1.0/cluster && go tool cover -func=coverage.out | Select-String "total"
```

### Step 5: Escape Analysis (Hot Paths)
```bash
cd paryty-v1.0/cluster && go build -gcflags="-m" ./cmd/pipeline/... 2>&1 | Select-String "escapes to heap"
```

### Step 6: Vet
```bash
cd paryty-v1.0/cluster && go vet ./... 2>&1
```

## Test Patterns Required

### Table-Driven Tests
```go
func TestXxx(t *testing.T) {
    tests := []struct {
        name     string
        input    Input
        expected Output
    }{ /* cases */ }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) { /* assert */ })
    }
}
```

### Integration Tests (build tag)
```go
//go:build integration
func TestIngestion_GRPCStream(t *testing.T) { /* requires infra */ }
```

## Coverage Requirements
| Package | Minimum |
|---|---|
| internal/processing/ | 80% |
| internal/storage/ | 75% |
| internal/stream/ | 75% |
| internal/api/ | 70% |
| internal/models/ | 90% |

## Exit Protocol
- ALL pass: "All Go testing gates passed"
- Test fails: Report name + assertion + file:line, STOP
- Race detected: Report stacks, STOP
- Coverage below threshold: Report packages, STOP
- Benchmark regression >10%: Report, STOP
