---
description: Execute a Paryty implementation phase — reads spec, breaks into tasks, dispatches to specialist agents
agent: code
---
# Run Phase — Phase Execution Orchestrator

Execute a specific Paryty implementation phase by reading its hardened spec, breaking it into tasks, and coordinating specialist agents.

## Input
Phase number (1-8) or specific phase name.

## Execution Steps

### Step 1: Read Phase Spec
Read the hardened spec at `paryty-v1.0/docs/development/phase-{N}-hardened-spec.md`.
Identify: layer breakdown, file list, dependencies, key interfaces.

### Step 2: Read Locked Decisions
Review `.qoder/rules/locked-decisions.md` for constraints affecting this phase.

### Step 3: Create Task Breakdown
Break into ordered tasks:
1. Proto/model changes first (contract-first)
2. Core implementation per layer
3. Integration between layers
4. Tests for each component
5. Verification gates

### Step 4: Dispatch to Specialist Agents
Map tasks to agents:
- Rust code → rust-agent-engineer or ebpf-engineer
- Go code → go-cluster-engineer
- TypeScript code → frontend-engineer
- Python code → python-ml-engineer
- Proto changes → integration-engineer
- Security → security-reviewer
- Performance → performance-engineer

### Step 5: Execute Tasks Sequentially
For each task:
1. Dispatch with specific instructions
2. Run verification after each task
3. Present result for acceptance
4. Move to next task

**Bug Fix Auto-Activation:** If any task surfaces a bug, the responsible agent must follow `bug-fix-discipline.md` immediately. Phase does not advance until bug is fixed and verification passes.

### Step 6: Phase Completion
1. Run full verification suite
2. Run /code-review on all changes
3. Run /test-integration for cross-component changes
4. Present summary: files created/modified, tests passing, coverage, performance, deferred items

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
- Run verification after every task, not just at the end
- Run /test-integration after any proto or cross-component change
- Present each completed task before proceeding
- Defer items that depend on unfinished phases
