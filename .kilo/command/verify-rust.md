---
description: Run full Rust verification pipeline — build, clippy, test, format check
agent: rust-agent-engineer
---
# Verify Rust — Paryty Agent Verification Pipeline

Run the full Rust verification pipeline for the Paryty Agent. After every Rust code change, all gates must pass.

## Execution Steps

### Gate 1: Build
```bash
cd paryty-v1.0/agent && cargo build 2>&1
```
Expected: Clean compilation with zero warnings.

### Gate 2: Clippy
```bash
cd paryty-v1.0/agent && cargo clippy -- -D warnings 2>&1
```
Expected: Zero clippy warnings (treated as errors).

### Gate 3: Unit Tests
```bash
cd paryty-v1.0/agent && cargo test 2>&1
```
Expected: All tests pass. Report any failures with test name and assertion.

### Gate 4: Format Check
```bash
cd paryty-v1.0/agent && cargo fmt --check 2>&1
```
Expected: All code properly formatted.

## Checklist
- [ ] No `unwrap()` in library code
- [ ] Every `unsafe` block has `// SAFETY:` comment
- [ ] No `println!` or `dbg!` in production code
- [ ] All public items have doc comments
- [ ] Memory usage within budget (<50MB)

## Exit Protocol
- ALL gates pass: Report "Rust verification passed" with raw output
- Build fails: Report compilation errors with file:line, STOP
- Clippy fails: Report warning with file:line, STOP
- Test fails: Report test name, assertion, and file:line, STOP
- Format fails: Report unformatted files, STOP

Show raw, unfiltered output from each gate. Never summarize or redact.
