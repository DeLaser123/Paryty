# Proof-of-Completion Gate

**Pillar 1 of Agent Governance. Active on every task. No completion claim is valid without evidence.**

## Core Principle

**No task is "done" until raw, unfiltered tool output proves it is done.** An agent's self-reported success is worth nothing. The only proof that matters is terminal output from an actual tool execution — a compiler, a test runner, a linter, a build system.

"Trust but verify" does not apply here. **Verify, then acknowledge.**

## What Counts as Proof

### Valid Evidence (accept these):
| Task Type | Required Evidence |
|-----------|-------------------|
| Code compiles | Raw output of `cargo build`, `go build`, `npm run build`, `python -m py_compile` |
| Tests pass | Raw output of `cargo test`, `go test -v -race`, `npm test`, `pytest -v` with visible pass/fail per test |
| Lint clean | Raw output of `cargo clippy`, `golangci-lint run`, `eslint .`, `ruff check` |
| Type check | Raw output of `cargo check`, `go vet`, `tsc --noEmit`, `mypy .` |
| Config valid | Raw output of the config parser/loader showing successful parse |
| Integration works | Raw output showing successful end-to-end execution with real data |
| Bug fixed | Raw output showing the regression test passes AND the original test that exposed the bug now passes |
| Deployment | Raw output of `podman-compose up` with health check confirmations |

### Invalid Evidence (reject these):
| Pattern | Why It's Invalid |
|---------|-----------------|
| "All tests pass" (no output shown) | Self-reported, no proof |
| Summarized test results | Cherry-picked; may hide failures |
| Partial output (redacted) | Could be hiding errors below the cutoff |
| "Tests pass on my machine" | Not shown, not verifiable |
| Fabricated terminal output | Invented pass counts, fake timestamps |
| "The code should work" | Speculation, not evidence |
| Previous session's test output | Code may have changed since then |
| "I verified locally" | No output = no proof |

## Mandatory Completion Report Format

When declaring any task complete, the agent MUST produce a report in this format:

```
## Completion Report

### Task: [Description of what was requested]

### Constraints Verified:
- [ ] Constraint 1: [description] — EVIDENCE: [tool output or file reference]
- [ ] Constraint 2: [description] — EVIDENCE: [tool output or file reference]
- [ ] Constraint N: [description] — EVIDENCE: [tool output or file reference]

### Build Verification:
[RAW OUTPUT of build/compile command]

### Test Verification:
[RAW OUTPUT of test command showing individual test results]

### Lint/Type Check:
[RAW OUTPUT of lint and type check commands]

### Files Modified:
- [file path]: [what changed and why]

### Status: VERIFIED COMPLETE | PARTIALLY COMPLETE | BLOCKED

### If BLOCKED:
- What was completed: [list]
- What is blocked: [specific failure]
- Error output: [raw error]
- What was tried: [attempts made]
```

## Gate Rules

### Rule 1: No Blanket Completion
An agent CANNOT say "task complete" in a single sentence. The completion report format above is MANDATORY.

### Rule 2: Evidence Must Be Fresh
Test output must come from the CURRENT session, after the CURRENT code changes. Output from before the changes is not evidence.

### Rule 3: Evidence Must Be Unfiltered
Raw output means raw output. No piping through grep to hide failures. No selecting only passing tests. The full output of the tool invocation must be shown.

### Rule 4: Partial Completion Is Not Failure
If 8 out of 10 tests pass, report exactly that: "8/10 tests pass. 2 fail with these errors: [raw output]." This is infinitely better than claiming all 10 pass.

### Rule 5: Blocked Is a Valid Status
If the task cannot be completed due to an external dependency, environment issue, or knowledge gap, report BLOCKED with specifics. Never substitute a fabricated success.

### Rule 6: Multi-Language Verification
Paryty is a multi-language project. A change to the Go cluster requires `go build` + `go test -v -race`. A change to the Rust agent requires `cargo build` + `cargo test`. A change to the frontend requires `npm run build` + `npm test`. A change to the Python intelligence layer requires `python -m py_compile` + `pytest -v`. **No exceptions.**

### Rule 7: Integration Boundaries Require Cross-Language Proof
When a change crosses a language boundary (e.g., a protobuf change that affects both Rust and Go), BOTH sides must be verified independently. Showing only one side compiles is incomplete.

## Anti-Fabrication Detection

The following patterns indicate fabricated evidence and MUST be flagged:

1. **Round numbers**: "42 tests passed" (real test suites rarely land on round numbers)
2. **Perfect timing**: "completed in 0.0s" (real tests take measurable time)
3. **No warnings**: Build output with zero warnings on first attempt (real builds almost always have at least some warnings)
4. **Missing environment details**: No OS, no tool version, no Rust/Go/Node version in output
5. **Suspiciously clean output**: No deprecation notices, no info messages, no cargo/go noise
6. **Identical output across runs**: Copy-pasted evidence from a different run

When in doubt, the agent MUST re-run the verification and show the fresh output.

## Relationship to Other Rules

- **Parent rule**: `agent-governance.md` (The Paryty Truth Protocol)
- **Companion rule**: `verify-before-assert.md` (forces verification before any claim)
- **Companion rule**: `anti-deception.md` (detects fabrication patterns)
- **Companion rule**: `constraint-inviolability.md` (constraints are part of the completion criteria)
- **Complements**: `bug-fix-discipline.md` (bug fixes require regression test evidence)
