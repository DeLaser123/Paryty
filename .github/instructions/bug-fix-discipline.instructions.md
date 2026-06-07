---
description: "Bug fix discipline. Use when diagnosing or fixing a bug."
---

# Bug Fix Discipline — Fix Once, Never Again

This rule is mandatory for all agents, all languages, and all components. No exceptions.

## Core Principle

Every bug must be fixed in a way that makes it **structurally impossible to recur**. A bug that resurfaces after a fix is worse than a bug that was never addressed — it erodes trust, wastes time, and signals a lack of engineering discipline.

**"Works for now" is not a fix. "Fix once, never again" is the only acceptable outcome.**

## Auto-Activation

This rule activates **automatically** the moment any of the following occurs — regardless of whether `/fix-bug` is explicitly invoked:

- The user reports a bug, error, crash, failure, or unexpected behavior
- A test fails (unit, integration, verification gate, or benchmark)
- A build or compilation error surfaces during development
- A runtime error, panic, segfault, or exception is observed
- The user describes incorrect behavior, even informally (e.g., "this is broken", "why does it do that?", "this shouldn't happen")

**No slash command is required.** Any agent that is asked to fix, investigate, or resolve any of the above **must** follow the full 7-step protocol below. The protocol is not optional, not invocable-only-by-skill, and not dependent on user memory.

The `/fix-bug` skill exists for structured orchestration and reporting — but the *discipline itself* is always active.

## The 7-Step Protocol

Every bug fix MUST follow these steps in order. Skipping any step is prohibited.

### Step 1: Reproduce

Write a test, script, or command that **reliably triggers the bug** before touching any code.

- If you cannot reproduce it, you do not understand it — do not attempt a fix.
- The reproduction must be deterministic, not flaky.
- Include the exact inputs, environment conditions, and sequence of events that trigger the bug.
- For intermittent bugs: identify the triggering conditions (timing, load, concurrency, state).

### Step 2: Root Cause Analysis

Trace from the visible symptom back to the **underlying design or logic flaw**.

- Use the **5 Whys technique**: ask "why?" at least 5 times until you reach the structural cause.
- Distinguish clearly:
  - **Symptom**: What the user sees (e.g., "connection fails")
  - **Proximate cause**: The immediate code failure (e.g., "nil pointer dereference")
  - **Root cause**: The design/logic flaw (e.g., "connection lifecycle doesn't handle partial initialization")
- Document the root cause in a comment or commit message before writing the fix.

### Step 3: Class Elimination

Search the **entire codebase** for the same anti-pattern or structural flaw.

- Use `Grep` / `SearchCodebase` to find all instances of the same pattern.
- Check across all components (agent, cluster, frontend, intelligence layer) — not just where the bug was observed.
- Fix every instance of the pattern in the same change, not just the one that triggered the bug.
- If the pattern is too widespread for one change: fix the reported instance and create tracked TODOs for the rest.

### Step 4: Systemic Fix

Apply a fix that makes the bug **structurally unrepresentable** in the code, not just checked at runtime.

Fix quality hierarchy (best to worst):
1. **Make it unrepresentable**: Use the type system to prevent the invalid state (enums, sealed types, newtypes, required fields)
2. **Guard at construction**: Validate at object creation time, fail immediately if invalid
3. **Assert at boundary**: Validate at function/service/API boundaries
4. **Check at usage**: Runtime check at point of use (least preferred — still error-prone)

### Step 5: Regression Test

Add a test that **would fail without the fix and passes with it**.

- The test must be minimal and focused on the specific bug scenario.
- Name the test clearly: `Test{Component}_{Scenario}_DoesNot{BugBehavior}` or equivalent.
- Include the exact conditions that triggered the bug (inputs, state, timing).
- For table-driven tests (Go): add the bug scenario as a new table entry.
- For property-based tests (Rust/proptest): add the bug case as a known regression case.
- The test is the permanent guardian — it runs in CI forever.

### Step 6: Environment Independence

Verify the fix works across **all target environments** for Paryty:

| Environment | Check |
|---|---|
| Windows | Tested on Windows, not just WSL |
| WSL2 | Tested in WSL2 Linux environment |
| Linux (bare metal) | Tested on native Linux |
| After restart | Fix persists across process/machine restarts |
| Concurrent execution | Fix holds under concurrent/parallel load |
| Edge cases | Empty inputs, nil values, timeouts, high load, boundary values |

### Step 7: Post-Mortem Note

Include in the code or commit:
- What the root cause was (not just the symptom)
- Why this fix prevents the entire class of bug from recurring
- What the regression test covers

Format for inline comment (if fix is non-obvious):
```
// BUGFIX: [Brief description]
// Root cause: [Why the bug existed]
// Fix: [What was changed and why it's permanent]
// Regression: [Name of test that guards against recurrence]
```

## Verification Checklist

Before declaring any bug fix complete, confirm ALL of the following:

- [ ] The fix addresses the **root cause**, not just the observed symptom
- [ ] A regression test was added that **would have caught this bug** before the fix
- [ ] The same anti-pattern was searched for and fixed **across the entire codebase**
- [ ] The relevant verification pipeline passes (verify-rust / verify-go / verify-frontend / verify-python)
- [ ] The fix works on **all target environments** (Windows, WSL, Linux)
- [ ] The fix works **after restart** and under **concurrent load**
- [ ] A post-mortem note is included in code comment or commit message
- [ ] The fix does not introduce **new bugs** or regressions
- [ ] The fix respects all **architecture rules** and **locked decisions**
- [ ] The fix handles the **entire class of bugs**, not just the single observed instance

## Forbidden Shortcuts

The following patterns are **always wrong**. Any agent or engineer who applies them has violated this rule:

| Shortcut | Why It's Wrong |
|---|---|
| Symptom patching | Fixes the visible behavior but leaves the root cause intact |
| `if x != nil` without asking why x is nil | Treats the symptom, not the cause of the unexpected nil |
| `try/except` around the failing call | Swallows the error without addressing why it occurs |
| Fixing only the file where the bug was observed | Same pattern likely exists elsewhere in the codebase |
| Skipping the regression test | The bug can silently return with the next code change |
| Environment-specific workaround | Will fail on any other machine, OS, or after restart |
| `// TODO: fix properly` with no tracked issue | The fix will never happen — it's buried and forgotten |
| Silencing an error log or alert | Hides the problem without solving it |
| Hardcoded magic values to bypass the bug | Fragile and non-portable; breaks under different conditions |
| "It doesn't happen in my environment" | Environment-dependent bugs are real bugs; fix the root cause |

## Integration with Other Rules

- **When fixing a bug**: Invoke `/fix-bug` for the full orchestrated protocol.
- **After fixing**: Invoke `/code-review` to validate fix quality against this discipline.
- **Verification gates**: All language-specific verification gates must still pass after the fix.
- **Locked decisions**: A bug fix is never an excuse to violate a locked architectural decision.

