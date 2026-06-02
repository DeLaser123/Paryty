# Verify Rust — Paryty Agent Compilation Check

## Purpose
Run the full Rust verification pipeline on the Paryty Agent. Execute after any Rust code change in agent/src/.

## Execution Steps

### Step 1: Format Check
Run: cargo fmt --check 2>&1
Expected: No diff. If fail, show what files need formatting.

### Step 2: Build Check
Run: cargo build 2>&1
Expected: Compilation succeeds with zero errors. Report warnings.

### Step 3: Clippy (Strict)
Run: cargo clippy -- -D warnings 2>&1
Expected: Zero warnings. Report each clippy lint with file:line.

### Step 4: Tests
Run: cargo test 2>&1
Expected: All tests pass. Report any failures with full output.

### Step 5: Miri (If unsafe code exists)
Check: grep -r "unsafe" agent/src/ 2>nul
If found: Run cargo miri test 2>&1
Expected: No undefined behavior detected.

## Exit Protocol
- ALL gates pass: Report "All verification gates passed"
- Any gate fails: Report which gate, the full error, and STOP (do not attempt fixes without user approval)
- Build fails: Report compile errors, STOP
- Test fails: Report test name and assertion failure, STOP

## Notes
- Must run from agent/ directory
- On Windows: eBPF cfg-gated dependencies won'`t compile — that'`s expected
- Always show the exact command output, not a summary
