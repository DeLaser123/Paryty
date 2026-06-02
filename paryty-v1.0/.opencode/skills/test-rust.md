# Test Rust — Paryty Agent Comprehensive Testing

## Purpose
Run the full Rust testing pipeline for the Paryty Agent. Covers unit tests, integration tests, property-based tests, benchmarks, and coverage reporting.

## Execution Steps

### Step 1: Unit Tests
Run: cargo test 2>&1
Expected: All tests pass with zero failures.

### Step 2: Integration Tests
Run: cargo test --test '*' 2>&1
Expected: All integration tests pass.

### Step 3: Doc Tests
Run: cargo test --doc 2>&1
Expected: All doc tests pass.

### Step 4: Property-Based Tests (If proptest is configured)
Run: cargo test --features proptest 2>&1
Expected: All property-based tests pass. No counterexamples found.

### Step 5: Benchmark Tests (If criterion is configured)
Run: cargo bench 2>&1
Expected: All benchmarks complete. Report any regressions > 10%.

### Step 6: Coverage Report
Run: cargo tarpaulin --out xml --output-dir coverage/ 2>&1
Expected: Coverage >= 80% for core modules (metal, ebpf, communication).

### Step 7: Memory Safety (If unsafe code exists)
Run: cargo miri test 2>&1
Expected: No undefined behavior detected.

## Test Patterns

### Unit Test Structure
```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_function_name_scenario() {
        // Arrange
        let input = setup_test_data();
        
        // Act
        let result = function_under_test(input);
        
        // Assert
        assert_eq!(result, expected);
    }
}
```

### Integration Test Structure
```rust
// tests/integration_test.rs
use paryty_agent::*;

#[tokio::test]
async fn test_grpc_connection_lifecycle() {
    // Test connection, streaming, disconnect, reconnect
}
```

### Property-Based Test Structure
```rust
use proptest::prelude::*;

proptest! {
    #[test]
    fn test_metric_serialization_roundtrip(metric in any::<Metric>()) {
        let bytes = serialize(&metric).unwrap();
        let deserialized = deserialize(&bytes).unwrap();
        prop_assert_eq!(metric, deserialized);
    }
}
```

## Exit Protocol
- ALL gates pass: Report "All testing gates passed"
- Unit test fails: Report test name, assertion, and file:line, STOP
- Integration test fails: Report test name and full error output, STOP
- Coverage below 80%: Report which modules are below threshold, STOP
- Miri reports UB: Report undefined behavior details, STOP
- Benchmark regression > 10%: Report which benchmark regressed, STOP

## Coverage Requirements
| Module | Minimum Coverage |
|--------|-----------------|
| metal/ | 80% |
| ebpf/ | 70% (cfg-gated on Linux) |
| communication/ | 85% |
| config/ | 90% |
| supervisor/ | 75% |

## Notes
- Must run from agent/ directory
- Coverage requires cargo-tarpaulin: `cargo install cargo-tarpaulin`
- Property tests require proptest feature: `cargo test --features proptest`
- Benchmarks require criterion: `cargo bench`
- On Windows: eBPF tests are cfg-gated and will be skipped
