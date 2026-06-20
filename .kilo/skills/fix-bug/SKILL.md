---
name: fix-bug
description: Orchestrate a disciplined bug fix for Paryty — walks through the full 7-step Fix Once, Never Again protocol with specialist agent dispatch and verification.
---
# Fix Bug — Paryty Bug Fix Orchestrator

## Purpose
Orchestrate a complete, disciplined bug fix following the "Fix Once, Never Again" protocol. No temporary fixes. No symptom patches.

## Execution Steps

### Step 1: Reproduce (Before Any Code Change)
Write a test or command that reliably triggers the bug BEFORE touching code.

### Step 2: Root Cause Analysis
Apply the 5 Whys technique. Distinguish symptom, proximate cause, root cause.

### Step 3: Class Elimination
Search ENTIRE codebase for the same anti-pattern. Fix ALL instances.

### Step 4: Systemic Fix
Make the bug structurally impossible (types > guards > checks).

### Step 5: Regression Test
The reproduction test becomes the permanent guardian.

### Step 6: Environment Independence
Verify on Windows, WSL, Linux, after restart, under load.

### Step 7: Post-Mortem
Document root cause, why permanent, what regression covers.

### Step 8: Verification Gates
Run /verify-rust, /verify-go, /verify-frontend, or /verify-python as applicable. All must pass.

## Agent Dispatch Rules
| Bug Domain | Agent |
|---|---|
| Rust (agent/) | rust-agent-engineer |
| eBPF | ebpf-engineer |
| Go (cluster/) | go-cluster-engineer |
| TypeScript (frontend/) | frontend-engineer |
| Python (intelligence) | python-ml-engineer |
| Proto/cross-component | integration-engineer |
| Performance | performance-engineer |
| Security | security-reviewer |

## Hard Rules
1. Never skip a step
2. Never fix without reproducing first
3. Never fix just the symptom
4. Never fix just one file
5. Never skip the regression test
6. Never environment-specific workarounds
7. Never declare fixed without verification passing
