---
name: execute-plan
description: Hands-off execution of multi-document plans — ingests all plan files, extracts tasks, builds dependency graph, dispatches specialists, verifies, and produces completion report.
---
# Execute Plan — Multi-Document Plan Orchestration

## Purpose
Execute one or more plan documents to completion without user intervention. The orchestrator reads all documents, extracts tasks, resolves dependencies, dispatches the appropriate specialist agent for each task, verifies output, and produces a comprehensive completion report.

## When to Use
- You have multiple plan documents (any count, 2 to 20+) and want them executed hands-off
- You have a single plan document that's too large for one agent session
- You want to execute an entire implementation phase from its hardened spec
- You're doing production hardening across multiple components

## Execution Phases

### Phase 0: Document Ingestion
1. Read ALL specified documents
2. Extract: title, context, numbered tasks, agent assignments, file targets, verification steps, dependencies
3. Build a master task list

### Phase 1: Dependency Resolution
- Identify file overlaps (two plans touching the same file)
- Identify code dependencies (Plan B needs types from Plan A)
- Reorder if needed, flag conflicts
- If user said "hands off," use conservative ordering

### Phase 2: Sequential Task Execution
For each task:
1. Identify the specialist agent
2. Dispatch with full context (files, changes, verification steps)
3. Wait for completion + domain verification
4. Run task-level verification if specified
5. Handle any bug-fix-auto-activation
6. Report task completion

### Phase 3: Security Gate
Dispatch security-reviewer for audit after all implementation.

### Phase 4: Integration Verification
Run test-integration if proto files or cross-component contracts were touched.

### Phase 5: Final Verification
Run ALL language-specific verification pipelines.

### Phase 6: Completion Report
Comprehensive report with files, tests, security, status.

## Agent Dispatch Rules
| Task Domain | Agent |
|---|---|
| Rust code | rust-agent-engineer |
| eBPF C code | ebpf-engineer |
| Go code | go-cluster-engineer |
| Frontend | frontend-engineer |
| Python ML | python-ml-engineer |
| Proto | integration-engineer |
| Security | security-reviewer |
| Performance | performance-engineer |

## Hard Rules
1. Contract-first: proto tasks before consumer tasks
2. Type-first: type tasks before consumer tasks  
3. Build after every task
4. Never skip verification
5. Bug fix auto-activation on any failure
6. Report CRITICAL security findings even in hands-off mode
7. Status updates after each document
8. One specialist per task dispatch
