---
description: Hands-off execution of multi-document plans — reads all plan files, extracts tasks, builds dependency graph, dispatches specialists, verifies, and reports
agent: code
subtask: true
---
# Execute Plan — Multi-Document Plan Orchestrator

Execute ALL specified plan documents to completion without user intervention. This is the "I'll be back in a few hours" command.

## Input

You will be given a list of plan document paths. These can be:
- `.qoder/plans/*.md` files (feature plans, redesigns, fixes)
- `docs/development/phase-*-hardened-spec.md` files (phase implementations)
- Any other markdown file containing tasks with Agent assignments

## Execution Protocol

### Phase 0: Ingestion
1. Read ALL plan documents into context
2. For each document, extract:
   - Title and context
   - All tasks (numbered or bulleted)
   - Agent assignments per task
   - Files to create/modify per task
   - Verification steps per task
   - Dependencies between tasks (if specified)
3. Build a master task list ordered by:
   - Document order (user-specified priority)
   - Within-document task order
   - Cross-document dependencies (if Plan B depends on Plan A output)

### Phase 1: Dependency Graph
Before executing, identify:
- **Overlaps**: Two plans touching the same file → warn, suggest order
- **Dependencies**: Plan B references types/code from Plan A → execute A first
- **Conflicts**: Two plans with contradictory changes to the same file → flag, ask if both intended
- **Independence**: Plans that touch entirely different components → can parallelize

If conflicts are found, report them and ask whether to proceed or reorder. If the user said "hands off," proceed with the most conservative order (execute in given sequence, skip conflicting tasks, report unresolved).

### Phase 2: Execution
For each task in the master list:

1. **Identify the specialist agent** from the task's domain:
   | Task Domain | Agent to Dispatch |
   |---|---|
   | Rust code (`agent/`, `agent/src/`) | `rust-agent-engineer` |
   | eBPF C code (`agent/src/ebpf/bpf/`) | `ebpf-engineer` |
   | Go code (`cluster/`) | `go-cluster-engineer` |
   | Frontend TypeScript (`frontend/src/`) | `frontend-engineer` (orchestrator, will sub-dispatch) |
   | Python ML (`intelligence/`) | `python-ml-engineer` |
   | Proto files (`proto/`) | `integration-engineer` |
   | Security audit | `security-reviewer` |
   | Performance profiling | `performance-engineer` |
   | Shell scripts, config YAML | `go-cluster-engineer` or direct |

2. **Dispatch with full context:**
   ```
   Task {N}/{Total} from plan "{PlanTitle}":
   Files to create: [...]
   Files to modify: [...]
   Specific changes: [from plan]
   Verification steps: [from plan]
   Previous task outputs to reference: [if dependent]
   ```

3. **Await completion.** Each specialist MUST:
   - Run domain verification gates (build/typecheck/tests)
   - Report: files created, files modified, test results, any contract changes
   - Signal any issues that affect downstream tasks

4. **Verify the task:**
   - If task specifies verification steps, run them
   - If specialist reports test failures, STOP and investigate
   - If bug fix discipline auto-activates, handle it before proceeding

5. **Cross-document check:** After each document's tasks complete, run the document's verification section.

### Phase 3: Security Gate
After ALL implementation tasks complete:
1. Dispatch `security-reviewer` for read-only audit
2. If CRITICAL or HIGH findings: flag to user (even in hands-off mode, security findings must be acknowledged)

### Phase 4: Integration Verification
If any document touched proto files or cross-component contracts:
1. Run `/test-integration`
2. Verify round-trips and streaming contracts

### Phase 5: Final Verification
Run ALL relevant verification gates:
```bash
# Rust
cd paryty-v1.0/agent && cargo build && cargo clippy -- -D warnings && cargo test && cargo fmt --check

# Go
cd paryty-v1.0/cluster && go build ./... && go vet ./... && go test -race -count=1 ./...

# Frontend
cd paryty-v1.0/frontend && npx tsc --noEmit && npm run build && npx vitest run && npx eslint src/

# Python
cd paryty-v1.0/intelligence && python -m py_compile *.py && ruff check . && python -m pytest -v
```

### Phase 6: Completion Report
Produce a comprehensive report:

```
## Plan Execution Report

### Documents Executed: N
| # | Document | Tasks | Status |
|---|----------|-------|--------|
| 1 | [title] | X tasks | COMPLETE |
| 2 | [title] | Y tasks | COMPLETE |

### Files Created: M
- [file]: [purpose]

### Files Modified: P
- [file]: [what changed]

### Test Results
- Rust: X/Y pass
- Go: X/Y pass
- Frontend: X/Y pass
- Python: X/Y pass

### Security Audit: [PASS / FINDINGS]

### Integration: [PASS / FAIL]

### Status: VERIFIED COMPLETE | PARTIALLY COMPLETE | BLOCKED

### If BLOCKED:
- Document: [which plan]
- Task: [which task]
- Blocker: [what failed]
- Resolution needed: [what user must decide]
```

## Hard Rules

1. **Contract-first.** Tasks that modify proto files execute BEFORE tasks that consume generated stubs.
2. **Type-first.** Tasks that create/modify types execute BEFORE tasks that consume those types.
3. **Build-after-every-task.** No task is complete until the relevant `build` command passes.
4. **Never skip verification.** Even in hands-off mode, every task's verification gates run.
5. **Bug fix auto-activation.** Any failure triggers the 7-step protocol. Fix before proceeding.
6. **Security findings are not silent.** Even in hands-off mode, CRITICAL security findings are reported to user.
7. **Report after every document.** Status updates after each plan document completes.
8. **One specialist per task.** Don't mix domains in a single dispatch. Split if needed.
