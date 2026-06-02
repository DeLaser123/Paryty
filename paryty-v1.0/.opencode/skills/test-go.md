# Test Go — Paryty Cluster Comprehensive Testing

## Purpose
Run the full Go testing pipeline for Paryty Cluster and Go SDK. Covers unit tests, integration tests, benchmarks, race detection, and coverage reporting.

## Execution Steps

### Step 1: Unit Tests
Run: go test -v -count=1 ./... 2>&1
Expected: All tests pass with zero failures.

### Step 2: Race Detection
Run: go test -race -count=1 ./... 2>&1
Expected: No data races detected.

### Step 3: Integration Tests (Build Tag)
Run: go test -v -tags=integration -count=1 ./... 2>&1
Expected: All integration tests pass. Requires running infrastructure (Redpanda, Dragonfly, QuestDB).

### Step 4: Benchmarks
Run: go test -bench=. -benchmem -count=3 ./... 2>&1
Expected: All benchmarks complete. Report any allocations in hot paths.

### Step 5: Coverage Report
Run: go test -coverprofile=coverage.out -covermode=atomic ./... 2>&1
Run: go tool cover -html=coverage.out -o coverage.html
Run: go tool cover -func=coverage.out | tail -1
Expected: Overall coverage >= 75%.

### Step 6: Escape Analysis (Hot Paths)
Run: go build -gcflags="-m" ./cmd/ingestion/... 2>&1 | Select-String "escapes to heap"
Expected: No unexpected heap escapes in ingestion hot path.

### Step 7: Vet
Run: go vet ./... 2>&1
Expected: Zero issues.

## Test Patterns

### Table-Driven Test Structure
```go
func TestMetricAggregation(t *testing.T) {
    tests := []struct {
        name     string
        input    []Metric
        expected AggregatedMetric
    }{
        {
            name:     "single metric",
            input:    []Metric{{Value: 42}},
            expected: AggregatedMetric{Avg: 42, Min: 42, Max: 42},
        },
        {
            name:     "multiple metrics",
            input:    []Metric{{Value: 10}, {Value: 20}, {Value: 30}},
            expected: AggregatedMetric{Avg: 20, Min: 10, Max: 30},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := Aggregate(tt.input)
            if result != tt.expected {
                t.Errorf("got %+v, want %+v", result, tt.expected)
            }
        })
    }
}
```

### Integration Test Structure
//go:build integration

```go
func TestIngestionLayer_GRPCStream(t *testing.T) {
    // Requires running Redpanda
    conn, err := grpc.Dial("localhost:443", grpc.WithInsecure())
    require.NoError(t, err)
    defer conn.Close()
    
    // Test bidirectional streaming
}
```

### Benchmark Structure
```go
func BenchmarkMetricSerialization(b *testing.B) {
    metric := generateTestMetric()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = serializeMetric(metric)
    }
}
```

### Mock Pattern (testify/mock)
```go
type MockStreamClient struct {
    mock.Mock
}

func (m *MockStreamClient) Send(msg *pb.Metric) error {
    args := m.Called(msg)
    return args.Error(0)
}
```

## Exit Protocol
- ALL gates pass: Report "All testing gates passed"
- Unit test fails: Report test name, assertion, and file:line, STOP
- Race detected: Report goroutine stacks and data race details, STOP
- Coverage below 75%: Report which packages are below threshold, STOP
- Benchmark regression: Report which benchmark regressed, STOP
- Vet issues: Report file:line and issue, STOP

## Coverage Requirements
| Package | Minimum Coverage |
|---------|-----------------|
| internal/ingestion/ | 80% |
| internal/processing/ | 80% |
| internal/storage/ | 75% |
| internal/stream/ | 75% |
| internal/api/ | 70% |
| internal/models/ | 90% |

## Notes
- Must run from cluster/ or agent/go_sdk/ directory
- Race tests require CGO_ENABLED=1 on some systems
- Integration tests require running infrastructure (Redpanda, Dragonfly, QuestDB)
- Use `go test -short` to skip long-running tests
