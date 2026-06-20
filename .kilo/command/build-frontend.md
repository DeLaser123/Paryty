---
description: Orchestrate the full frontend build pipeline — dispatches 6 specialists in defined handoff order with security and performance gates
agent: frontend-engineer
subtask: true
---
# Build Frontend — Paryty Frontend Orchestration Pipeline

Execute the complete frontend build pipeline by dispatching 6 sub-specialists in strict handoff order.

## Pipeline Overview

```
Phase 1 (Parallel): UX Engineer + Data Handler
       ↓
Phase 2: Processing Specialist (after Data Handler)
       ↓
Phase 3: Topology Specialist (after UX + Processing)
       ↓
Phase 4: Security Specialist (audit, iterate)
       ↓
Phase 5: Performance Supervisor (profile, iterate)
       ↓
Complete: Run full verification gates
```

## Dispatch Instructions

### Phase 1: Start UX and Data Handler in Parallel

**UX Engineer task:** "Build/refine the UI components for [feature]. Load the Paryty Design System from tokens.css, globals.css, and existing components. Compose from DS primitives first, extend CSS only if composition fails. All UI must be responsive by default. Apply motion tokens for interactions. Eliminate all native browser primitives. Add data-testid and ARIA attributes. Run tsc --noEmit and build after changes. Report: (1) what was composed from primitives, (2) what was extended with new CSS, (3) any new React components created, (4) responsive breakpoints used."

**Data Handler task:** "Build/refine the data layer for [feature]. Implement API client functions with discriminated union return types. Create/update Zustand stores with selector-based subscriptions. Wire up WebSocket/SSE if real-time data is needed. Define type contracts in types/. Ensure auth token handling is secure (httpOnly cookie, never localStorage). Run tsc --noEmit and tests after changes. Report: (1) new/modified API client functions, (2) new/modified Zustand stores, (3) new/modified types, (4) transport security measures."

### Phase 2: Processing Specialist

**Processing task:** "Build/refine heavy computation for [feature] in Web Workers. Use SharedArrayBuffer for zero-copy data transfer from Data Handler. Pre-allocate all buffers. Validate all incoming message payloads. Implement timeout on every computation. Provide ProcessingCapabilities detection for fallback. Run tsc --noEmit and processing tests. Report: (1) new/modified worker message types, (2) computation algorithms used, (3) buffer allocation strategy, (4) fallback behavior."

### Phase 3: Topology Specialist

**Topology task:** "Build/refine GPU-accelerated visualization for [feature]. Use PixiJS instanced rendering for 10K+ nodes. Apply visual encoding hierarchy (position, color, size, glow, particles). Implement LOD by zoom level. Pre-allocate all GPU objects (never allocate in render loop). Handle WebGL context loss/recovery. Report: (1) rendering approach and draw call count, (2) LOD configuration, (3) GPU memory budget status, (4) frame rate with target node count."

### Phase 4: Security Specialist

**Security task:** "Audit the assembled frontend for [feature]. Run static analysis (npm audit, eslint, tsc). Check CSP compliance. Audit browser console/network/storage/DOM. Validate WebSocket and Web Worker security. Report findings with exact file:line references and specialist assignments. Iterate with specialists until zero CRITICAL and zero HIGH findings. Report: (1) findings by severity, (2) resolved issues, (3) remaining LOW/INFO items."

### Phase 5: Performance Supervisor

**Performance task:** "Profile the assembled frontend for [feature]. Analyze bundle size. Run Lighthouse audit. Profile runtime frame budget. Take heap snapshots for memory leak detection. Check network efficiency. Report findings with profiling evidence and specialist assignments. Iterate with specialists until all budgets are met. Report: (1) findings by severity, (2) before/after metrics, (3) budget compliance status."

### Final Verification

Run the complete verification pipeline:
```bash
cd paryty-v1.0/frontend && npx tsc --noEmit 2>&1
cd paryty-v1.0/frontend && npm run build 2>&1
cd paryty-v1.0/frontend && npx vitest run 2>&1
cd paryty-v1.0/frontend && npx eslint src/ 2>&1
```

## Hard Rules

1. Never skip a phase.
2. Phase 1 specialists work in parallel (no data dependency between UX and Data).
3. Phase 2 MUST wait for Phase 1 Data Handler completion (needs type contracts).
4. Phase 3 MUST wait for Phase 1 UX (component structure) AND Phase 2 Processing (layout positions).
5. Phase 4 audits ALL assembled code. Found vulnerabilities MUST be fixed before proceeding.
6. Phase 5 profiles ALL assembled code. Performance regressions MUST be fixed before proceeding.
7. Each specialist runs their domain verification gates independently.
8. The orchestrator verifies the complete pipeline at the end.
