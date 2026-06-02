# Verify eBPF Observer

Verify the eBPF Network Observer layer (`agent/src/ebpf/`) for correctness, verifier compliance, and security.

## Verification Steps

### 1. Build (Linux Only)
```bash
cd agent && cargo build --features ebpf 2>&1
cd agent && cargo clippy --features ebpf -- -D warnings 2>&1
```

### 2. BPF Verifier Tests
```bash
cd agent && cargo test --features ebpf ebpf 2>&1
```
Verify:
- All BPF programs pass verifier
- Map operations correct (insert, lookup, delete)
- Event serialization to ring buffer works
- No unbounded loops in BPF code

### 3. Miri Tests (Non-BPF Code)
```bash
cd agent && cargo miri test --features ebpf 2>&1
```
- No undefined behavior in userspace code
- No memory safety violations

### 4. Integration Tests (Requires CAP_BPF)
```bash
sudo -E cargo test --features ebpf --test ebpf_integration 2>&1
```
- TCP connection tracking works on real interface
- DNS resolution captured
- HTTP request inspection works
- Database protocol inspection works

### 5. Benchmarks
- BPF program execution: <1us per invocation
- Ring buffer throughput: >1M events/second
- Userspace consumer: <10us per event
- Map memory: <50MB total

### 6. Security Audit
- [ ] No arbitrary kernel memory access
- [ ] `bpf_probe_read_kernel` used for kernel data
- [ ] All loops bounded
- [ ] No allocations in BPF context
- [ ] cfg-gated on `target_os = "linux"`
- [ ] Proper cleanup on detach

## Pass Criteria
- BPF verifier accepts all programs
- No Miri violations
- Integration tests pass (Linux with CAP_BPF)
- All benchmarks within budget
- Security checklist clean
