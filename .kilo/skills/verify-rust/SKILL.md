---
name: verify-rust
description: Run the full Rust verification pipeline — build, clippy, test, format check. Use after any Rust code change.
---
# Verify Rust — Paryty Agent Verification Pipeline

## Execution Steps

### Gate 1: Build
```bash
cd paryty-v1.0/agent && cargo build 2>&1
```

### Gate 2: Clippy
```bash
cd paryty-v1.0/agent && cargo clippy -- -D warnings 2>&1
```

### Gate 3: Unit Tests
```bash
cd paryty-v1.0/agent && cargo test 2>&1
```

### Gate 4: Format Check
```bash
cd paryty-v1.0/agent && cargo fmt --check 2>&1
```

## Exit Protocol
- ALL pass: "Rust verification passed"
- Any fail: Report specific failure with file:line, STOP
- Show raw, unfiltered output from each gate
