---
name: run-phase
description: Execute a Paryty implementation phase — reads the hardened spec, creates a task breakdown, dispatches work to specialist agents, and verifies output.
---

# Run Phase — Phase Execution Orchestrator

## Purpose
Execute a specific Paryty implementation phase by reading its hardened spec, breaking it into tasks, and coordinating specialist agents.

## Input
Phase number (1-8) or specific phase name.

## Execution Steps

### Step 1: Read Phase Spec
Read the hardened spec at paryty-v1.0/docs/development/phase-{N}-hardened-spec.md.
Identify:
- Layer breakdown and LOC targets
- File list with exact paths
- Dependencies on other phases
- Key interfaces and contracts

### Step 2: Read Locked Decisions
Review .qoder/rules/locked-decisions.md for any constraints affecting this phase.
Cross-reference with the phase spec to ensure alignment.

### Step 3: Create Task Breakdown
Break the phase into ordered implementation tasks:
1. Proto/model changes first (contract-first)
2. Core implementation per layer
3. Integration between layers
4. Tests for each component
5. Verification gates

### Step 4: Identify Required Agents
Map tasks to specialist agents:
- Rust code — rust-agent-engineer or ebpf-engineer
- Go code — go-cluster-engineer
- TypeScript code — frontend-engineer
- Python code — python-ml-engineer
- Proto changes — integration-engineer
- Security review — security-reviewer
- Performance check — performance-engineer

### Step 5: Execute Tasks Sequentially
For each task:
1. Dispatch to the appropriate agent with specific instructions
2. Run verification skill after each task (/verify-go, /verify-rust, /verify-python, /verify-frontend, etc.)
3. Present result to user for acceptance
4. Move to next task

**Bug Fix Auto-Activation:** If any task surfaces a bug, error, test failure, or unexpected behavior — whether found by verification gates, the user, or the agent itself — the responsible agent **must** follow `bug-fix-discipline.md` immediately before proceeding to the next task. No `/fix-bug` invocation required; the discipline activates automatically. The phase does not advance until the bug is fixed, the regression test is in place, and verification gates pass.

### Step 6: Phase Completion
1. Run full verification suite
2. Run /code-review on all changes
3. Run /test-integration for any cross-component changes
4. Present summary to user:
   - Files created/modified
   - Tests passing
   - Coverage achieved
   - Performance metrics
   - Any deferred items

## Phase Reference
| Phase | Focus | Primary Agent |
|---|---|---|
| 1 | Agent Core (Rust) | rust-agent-engineer |
| 2 | eBPF Observer | ebpf-engineer |
| 3 | Processing Pipeline (Go) | go-cluster-engineer |
| 4 | Storage Layer + DB Inspection | go-cluster-engineer + ebpf-engineer |
| 5 | Query API + SDK | go-cluster-engineer + integration-engineer |
| 6 | Intelligence Layer (Python ML) | python-ml-engineer |
| 7 | Frontend Visualization | frontend-engineer |
| 8 | Production Hardening | All agents |

## Notes
- Always start with contract-first (proto changes before implementation)
- Run verification after every agent task, not just at the end
- Run /test-integration after any proto or cross-component change
- Present each completed task to the user before proceeding
- Defer items that depend on unfinished phases
