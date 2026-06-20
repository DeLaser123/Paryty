---
name: build-frontend
description: Orchestrate the full frontend build pipeline — dispatches 6 sub-specialists in defined handoff order through UX, Data, Processing, Topology, Security, and Performance gates.
---
# Build Frontend — Paryty Frontend Orchestration Pipeline

## Purpose
Execute the complete frontend build pipeline by coordinating 6 hyper-specialized agents in a precise handoff order. Each phase gates the next. Security and Performance specialists audit the final output and iterate until all targets are met.

## The 5-Phase Pipeline

```
Phase 1 (Parallel):    UX Engineer  +  Data Handler
                              │              │
                              ▼              │
Phase 2:              Processing Specialist  │
                              │              │
                              ▼              ▼
Phase 3:              Topology Specialist
                              │
                              ▼
Phase 4:              Security Audit (iterate ← specialists)
                              │
                              ▼
Phase 5:              Performance Profile (iterate ← specialists)
                              │
                              ▼
                       Final Verification Gates
```

## Specialist Dispatch Order

### Phase 1A: UX Engineer
Dispatch `frontend-ux-engineer` to build/refine UI components, CSS, motion, accessibility, responsive layout.
- **Input:** Feature requirements, visual references if provided
- **Output:** Component tree, CSS classes, DS token usage report
- **Contract with:** Data Handler (type selectors needed), Topology Specialist (canvas container dimensions)

### Phase 1B: Data Handler (parallel with UX)
Dispatch `frontend-data-engineer` to build API client, Zustand stores, React hooks, type definitions.
- **Input:** API contract, data requirements from feature spec
- **Output:** Store interfaces, API client functions, type definitions
- **Contract with:** UX (store selectors), Processing (raw data shapes)

### Phase 2: Processing Specialist
Dispatch `frontend-processing-engineer` after Data Handler completes.
- **Input:** Raw data shapes from Data Handler
- **Output:** Web Worker message protocol, SharedArrayBuffer layout, computation algorithms
- **Contract with:** Topology Specialist (layout positions via SharedArrayBuffer)

### Phase 3: Topology Specialist
Dispatch `frontend-topology-engineer` after UX and Processing complete.
- **Input:** Component structure from UX, layout positions from Processing, topology data from Data Handler
- **Output:** GPU rendering pipeline, LOD configuration, interaction model
- **Contract with:** UX (canvas dimensions, selection state)

### Phase 4: Security Audit
Dispatch `frontend-security-engineer` after ALL Phase 1-3 specialists complete.
- **Audits:** Static analysis (npm audit, eslint, CSP), browser runtime (console, network, storage, DOM), WebSocket/Worker security
- **Iterates:** With responsible specialist until zero CRITICAL/HIGH findings
- **Gate:** Must pass before Phase 5

### Phase 5: Performance Profiling
Dispatch `frontend-performance-engineer` after Security sign-off.
- **Profiles:** Bundle size, Lighthouse (FCP/LCP/TTI), runtime frame budget, memory heap, network efficiency
- **Iterates:** With responsible specialist until all budgets met
- **Gate:** Must pass before Final Verification

### Final Verification
```bash
cd paryty-v1.0/frontend && npx tsc --noEmit 2>&1
cd paryty-v1.0/frontend && npm run build 2>&1
cd paryty-v1.0/frontend && npx vitest run 2>&1
cd paryty-v1.0/frontend && npx eslint src/ 2>&1
```

## Hard Rules
1. Never skip a phase. All 5 phases execute in order.
2. Phase 1 specialists work in parallel (no data dependency between UX and Data).
3. Phase 2 waits for Phase 1 Data Handler (needs type contracts).
4. Phase 3 waits for Phase 1 UX (component structure) AND Phase 2 (layout positions).
5. Phase 4 audits ALL assembled code. Zero CRITICAL/HIGH findings before proceeding.
6. Phase 5 profiles ALL assembled code. All budgets met before proceeding.
7. Each specialist independently runs their domain verification gates.
8. Final verification gates confirm the complete pipeline is clean.
