---
name: fix-bug
description: Orchestrate a disciplined bug fix for Paryty — walks through the full 7-step Fix Once, Never Again protocol with specialist agent dispatch and verification.
---

# Fix Bug — Paryty Bug Fix Orchestrator

## Purpose
Orchestrate a complete, disciplined bug fix following the "Fix Once, Never Again" protocol defined in `bug-fix-discipline.md`. No temporary fixes. No symptom patches. Fix it once, fix it right, fix it forever.

## Input
Bug description from the user — may include: error messages, stack traces, reproduction steps, observed vs expected behavior, affected component, environment details.

## Execution Steps

### Step 1: Reproduce (Before Any Code Change)

**Goal:** Prove you can trigger the bug reliably.

1. Parse the user's bug report for: affected component, error message, environment, steps to trigger.
2. Write a minimal test or command that triggers the bug deterministically.
   - For Rust: `cargo test -- <test_name>` that fails
   - For Go: `go test -run Test<Name> ./...` that fails
   - For TypeScript: Vitest test that fails
   - For Python: pytest test that fails
   - For integration: script or sequence that reproduces the cross-component failure
3. If the bug cannot be reproduced: ask the user for more context. Do NOT guess and patch.
4. **Report to user:** "Bug reproduced. Here is the failing test/command and its output."

### Step 2: Root Cause Analysis

**Goal:** Understand WHY the bug exists, not just WHAT it does.

1. Read the relevant source files — trace from the symptom inward.
2. Apply the **5 Whys technique**: keep asking "why?" until you reach the structural cause.
3. Clearly articulate to the user:
   - **Symptom:** What the user sees
   - **Proximate cause:** The immediate code-level failure
   - **Root cause:** The design/logic flaw that allowed the bug to exist
4. **Report to user:** "Root cause identified: [explanation]. Here's the 5-whys chain."

### Step 3: Class Elimination

**Goal:** Find and fix ALL instances of the same anti-pattern across the codebase.

1. Identify the anti-pattern from the root cause.
2. Use `Grep` and `SearchCodebase` to search the entire Paryty codebase for the same pattern.
3. Check across ALL components — not just where the bug was observed:
   - agent/ (Rust)
   - agent/src/ebpf/ (C)
   - cluster/ (Go)
   - frontend/ (TypeScript)
   - Intelligence Layer (Python)
4. List all instances found.
5. **Report to user:** "Found N instances of the same anti-pattern. Fixing all of them."

### Step 4: Systemic Fix

**Goal:** Apply a fix that makes the bug structurally impossible.

1. Choose the highest-quality fix from the hierarchy:
   - Best: Make it unrepresentable (type system, sealed types, required fields)
   - Good: Guard at construction (validate at creation, fail fast)
   - Acceptable: Assert at boundary (validate at service/API boundaries)
   - Last resort: Check at usage (runtime guard — must include explanation why higher tiers aren't possible)
2. Apply the fix to ALL instances found in Step 3.
3. Include a `// BUGFIX:` comment for non-obvious fixes.
4. **Report to user:** "Fix applied: [description]. Fix quality tier: [unrepresentable/guarded/asserted/checked]."

### Step 5: Regression Test

**Goal:** Add a permanent test guardian that prevents the bug from silently returning.

1. The reproduction test from Step 1 becomes the regression test.
2. Verify it FAILS without the fix (temporarily revert to confirm, or explain why it would fail).
3. Verify it PASSES with the fix.
4. Name the test clearly: `Test{Component}_{Scenario}_DoesNot{BugBehavior}` or equivalent.
5. Add to the appropriate test file in the affected component.
6. **Report to user:** "Regression test added: [test name]. It fails without the fix and passes with it."

### Step 6: Environment Independence

**Goal:** Confirm the fix works everywhere Paryty runs.

Verify the fix holds under:
- Windows (native PowerShell)
- WSL2 (Linux under Windows)
- Linux (bare metal or VM)
- After process/machine restart
- Under concurrent/parallel load
- Edge cases: empty inputs, nil/None values, timeouts, boundary conditions

**Report to user:** "Environment independence verified: [list of environments/conditions tested]."

### Step 7: Verification Gates

**Goal:** Run the full verification pipeline for the affected component(s).

Dispatch the appropriate verification skill(s):
- Rust changes → `/verify-rust`
- Go changes → `/verify-go`
- TypeScript changes → `/verify-frontend`
- Python changes → `/verify-python`
- Cross-component → `/test-integration`
- Performance-affected → `/performance-engineer` agent review
- Security-relevant → `/security-reviewer` agent review

All gates must pass. No exceptions.

### Step 8: Post-Mortem and Completion

**Goal:** Document and close the fix.

1. Include in the fix (code comment or commit message):
   - Root cause explanation
   - Why this fix is permanent
   - What the regression test covers
2. Run `/code-review` on the fix with the Bug Fix Quality dimension active.
3. **Final report to user:**

```
Bug Fix Report
──────────────
Bug:           [description]
Component:     [agent/cluster/frontend/intelligence/cross-component]
Root Cause:    [explanation]
Fix Applied:   [what changed and why it's permanent]
Fix Tier:      [unrepresentable / guarded / asserted / checked]
Class Fixed:   [N instances of the anti-pattern fixed across codebase]
Regression:    [test name and file]
Environments:  [Windows / WSL / Linux / restart / concurrent]
Verification:  [which skills passed]
Code Review:   [APPROVED / CHANGES REQUESTED]
```

## Agent Dispatch Rules

| Bug Domain | Primary Agent | Supporting Agent |
|---|---|---|
| Rust (agent/) | rust-agent-engineer | — |
| eBPF (agent/src/ebpf/) | ebpf-engineer | rust-agent-engineer (loader) |
| Go (cluster/) | go-cluster-engineer | — |
| TypeScript (frontend/) | frontend-engineer | — |
| Python (intelligence) | python-ml-engineer | — |
| Proto / cross-component | integration-engineer | language-specific agent |
| Performance regression | performance-engineer | language-specific agent |
| Security vulnerability | security-reviewer | language-specific agent |

## Hard Rules

1. **Never skip a step.** All 8 steps must complete in order.
2. **Never fix without reproducing first.** No-repro bugs require more investigation, not guesses.
3. **Never fix just the symptom.** Always trace to root cause.
4. **Never fix just one file.** Always search for the pattern across the entire codebase.
5. **Never skip the regression test.** The test is the permanent guardian.
6. **Never apply environment-specific workarounds.** The fix must work everywhere.
7. **Never declare "fixed" without verification gates passing.** All gates, not some gates.
8. **Reference `bug-fix-discipline.md`** as the authoritative rule throughout.
