---
description: Orchestrate a disciplined bug fix following the 7-step Fix Once, Never Again protocol
agent: code
---
# Fix Bug — Paryty Bug Fix Orchestrator

Execute a complete, disciplined bug fix following the "Fix Once, Never Again" protocol defined in `.qoder/rules/bug-fix-discipline.md`. No temporary fixes. No symptom patches.

## The 7-Step Protocol

### Step 1: Reproduce
Write a minimal test or command that reliably triggers the bug BEFORE touching any code.
- For Rust: `cargo test -- <test_name>` that fails
- For Go: `go test -run Test<Name> ./...` that fails
- For TypeScript: Vitest test that fails
- For Python: pytest test that fails
- **Report:** "Bug reproduced. Here is the failing test/command and its output."

### Step 2: Root Cause Analysis
Apply the **5 Whys technique** until reaching the structural cause.
- **Symptom:** What the user sees
- **Proximate cause:** Immediate code failure
- **Root cause:** Design/logic flaw
- **Report:** "Root cause identified: [explanation]. Here's the 5-whys chain."

### Step 3: Class Elimination
Search the ENTIRE codebase for the same anti-pattern:
- agent/ (Rust), agent/src/ebpf/ (C), cluster/ (Go), frontend/ (TypeScript), Intelligence Layer (Python)
- Fix ALL instances, not just the observed one.
- **Report:** "Found N instances of the same anti-pattern. Fixing all of them."

### Step 4: Systemic Fix
Apply a fix that makes the bug structurally impossible:
1. **Best**: Make it unrepresentable (type system)
2. **Good**: Guard at construction
3. **Acceptable**: Assert at boundary
4. **Last resort**: Check at usage (explain why higher tiers not possible)
Include `// BUGFIX:` comment for non-obvious fixes.

### Step 5: Regression Test
The reproduction test from Step 1 becomes the permanent regression test.
- Verify it FAILS without the fix
- Verify it PASSES with the fix
- Name: `Test{Component}_{Scenario}_DoesNot{BugBehavior}`

### Step 6: Environment Independence
Verify fix works on: Windows, WSL2, Linux, after restart, under concurrent load, edge cases.

### Step 7: Post-Mortem
Document: root cause, why fix is permanent, what regression test covers.

### Step 8: Verification Gates
Run the appropriate verification command:
- Rust: `/verify-rust`
- Go: `/verify-go`
- TypeScript: `/verify-frontend`
- Python: `/verify-python`
- Cross-component: `/test-integration`

All gates must pass. No exceptions.

## Agent Dispatch Rules

| Bug Domain | Agent |
|---|---|
| Rust (agent/) | rust-agent-engineer |
| eBPF (agent/src/ebpf/) | ebpf-engineer |
| Go (cluster/) | go-cluster-engineer |
| TypeScript (frontend/) | frontend-engineer |
| Python (intelligence) | python-ml-engineer |
| Proto / cross-component | integration-engineer |
| Performance regression | performance-engineer |
| Security vulnerability | security-reviewer |

## Hard Rules
1. Never skip a step. All 8 steps must complete in order.
2. Never fix without reproducing first.
3. Never fix just the symptom. Always trace to root cause.
4. Never fix just one file. Always search for the pattern across the entire codebase.
5. Never skip the regression test.
6. Never apply environment-specific workarounds.
7. Never declare "fixed" without verification gates passing.
