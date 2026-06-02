# Verify Supervisor

Verify the Supervisor layer (`agent/src/supervisor/`) for correctness, resilience, and security.

## Verification Steps

### 1. Build & Lint
```bash
cd agent && cargo build 2>&1
cd agent && cargo clippy -- -D warnings 2>&1
```

### 2. Unit Tests
```bash
cd agent && cargo test --lib supervisor 2>&1
```
Verify:
- Health checks (HTTP, TCP, gRPC) work with mock endpoints
- Circuit breaker state transitions correct
- Log tailer captures new lines
- Config watcher detects changes

### 3. Resilience Tests
- Health check timeout respected (HTTP 5s, TCP 2s, gRPC 5s)
- Circuit breaker opens after 5 failures in 30s
- Circuit breaker half-opens after 30s cooldown
- Log buffer never exceeds 10K lines capacity
- Exponential backoff with jitter works correctly

### 4. Security Audit
- [ ] No `std::process::Command` usage
- [ ] No arbitrary command execution
- [ ] Log content sanitized (control characters stripped)
- [ ] No sensitive data in health check responses
- [ ] TLS for health check endpoints when configured

### 5. Runtime Validation
- Health checks fire at configured intervals
- Circuit breaker recovers after cooldown
- Log tailer captures new content within 100ms
- Config watcher detects changes within 1s

## Pass Criteria
- All tests pass
- Resilience tests verify correct behavior under failure
- No security issues found
- Runtime validation succeeds
