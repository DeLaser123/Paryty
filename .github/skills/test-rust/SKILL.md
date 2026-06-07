---
name: test-rust
description: Run comprehensive Rust tests for Paryty Agent — unit tests, property-based tests, miri, benchmarks, and format check.
---

# Test Rust — Comprehensive Rust Testing

## Execution Steps

### Step 1: Unit Tests
```bash
cd paryty-v1.0/agent && cargo test 2>&1
```

### Step 2: Clippy
```bash
cd paryty-v1.0/agent && cargo clippy -- -D warnings 2>&1
```

### Step 3: Miri (UB Detection, non-BPF code)
```bash
cd paryty-v1.0/agent && cargo +nightly miri test 2>&1
```

### Step 4: Benchmarks
```bash
cd paryty-v1.0/agent && cargo bench 2>&1
```
Report any performance regressions >10%.

### Step 5: Format Check
```bash
cd paryty-v1.0/agent && cargo fmt --check 2>&1
```

## Test Categories

### Unit Tests
- Metal scraper correctness (CPU, memory, disk, network)
- Communication layer (compression, edge buffer, reconnect)
- Config parsing and defaults
- Proto message serialization

### Property-Based Tests (proptest/quickcheck)
- Compression: decompress(compress(data)) == data
- Edge buffer: FIFO ordering preserved
- Sequence numbers: monotonically increasing
- Memory usage: bounded under 50MB

### Benchmarks (criterion)
- Compression throughput (Zstd)
- gRPC message serialization
- Metal scraper latency per metric type

## Exit Protocol
- ALL pass: "All Rust testing gates passed"
- Test fails: Report name + assertion + file:line, STOP
- Miri detects UB: Report location and description, STOP
- Benchmark regression >10%: Report, STOP
- Format issues: Report files, STOP
