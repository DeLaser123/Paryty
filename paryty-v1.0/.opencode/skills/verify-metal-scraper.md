# Verify Metal Scraper

Verify the Metal Scraper layer (`agent/src/metal/`) for correctness, performance, and security.

## Verification Steps

### 1. Build & Lint
```bash
cd agent && cargo build 2>&1
cd agent && cargo clippy -- -D warnings 2>&1
```

### 2. Unit Tests
```bash
cd agent && cargo test --lib metal 2>&1
```
Verify:
- All collectors pass (CPU, memory, disk, network, process, container)
- Mock /proc and /sys fixtures used correctly
- Edge cases handled (empty files, missing fields, malformed data)

### 3. Property Tests
Verify invariants:
- CPU percentages sum to <= 100% per core
- Memory: RSS <= VSZ always
- Disk: IOPS >= 0, throughput >= 0
- Network: bytes >= 0, packets >= 0
- Process count > 0 when system running

### 4. Benchmarks
```bash
cd agent && cargo bench --bench metal_bench 2>&1
```
Verify against budget:
- Collection cycle: <1ms
- Allocations: <100 bytes per cycle
- Throughput: >10K metrics/second

### 5. Security Audit
- [ ] No `std::process::Command` usage
- [ ] No shell-outs anywhere
- [ ] Buffer sizes bounded
- [ ] No sensitive data in metric output
- [ ] File paths validated before reading

### 6. Runtime Validation
```bash
cd agent && cargo run --bin paryty-agent -- --validate-metal 2>&1
```
- Verify /proc reads succeed
- Verify metric values in expected ranges
- Verify collection interval maintained
- Verify memory usage stable

## Pass Criteria
- All tests pass
- All benchmarks within budget
- No clippy warnings
- No security issues found
- Runtime validation succeeds
