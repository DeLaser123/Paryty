---
name: run-phase
description: Execute a Paryty implementation phase — reads the hardened spec, creates a task breakdown, dispatches work to specialist agents, and verifies output.
---
# Run Phase — Phase Execution Orchestrator

## Purpose
Execute a specific Paryty implementation phase by reading its hardened spec, breaking it into tasks, and coordinating specialist agents.

## Execution Steps

### Step 1: Read Phase Spec
Read hardened spec at `paryty-v1.0/docs/development/phase-{N}-hardened-spec.md`.

### Step 2: Read Locked Decisions
Review `.qoder/rules/locked-decisions.md`.

### Step 3: Create Task Breakdown
1. Proto/model changes first
2. Core implementation per layer
3. Integration between layers
4. Tests for each component
5. Verification gates

### Step 4: Dispatch to Specialist Agents
- Rust → rust-agent-engineer / ebpf-engineer
- Go → go-cluster-engineer
- TypeScript → frontend-engineer
- Python → python-ml-engineer
- Proto → integration-engineer
- Security → security-reviewer
- Performance → performance-engineer

### Step 5: Execute Tasks Sequentially
Run verification after each task. Bug fix discipline activates automatically on any failure.

### Step 6: Phase Completion
Full verification suite, code review, integration tests, summary report.

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
