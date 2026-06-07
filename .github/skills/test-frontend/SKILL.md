---
name: test-frontend
description: Run comprehensive frontend tests for Paryty — Vitest unit tests, visual regression, and accessibility checks.
---

# Test Frontend — Comprehensive Frontend Testing

## Execution Steps

### Step 1: Unit Tests (Vitest)
```bash
cd paryty-v1.0/frontend && npx vitest run 2>&1
```

### Step 2: TypeScript Check
```bash
cd paryty-v1.0/frontend && npx tsc --noEmit 2>&1
```

### Step 3: Build Verification
```bash
cd paryty-v1.0/frontend && npm run build 2>&1
```

### Step 4: Lint
```bash
cd paryty-v1.0/frontend && npx eslint src/ 2>&1
```

## Test Categories

### Component Tests
- Topology view rendering with nodes/edges
- Node selection and deselection
- Hover tooltip display
- Zoom level transitions
- Timeline slider interaction

### Store Tests
- Zustand store actions (add/remove/update nodes)
- State persistence across renders
- WebSocket data integration

### Visual Tests
- Color encoding correctness (health → green/yellow/red)
- Particle animation smoothness
- Edge rendering with throughput values

### Accessibility Tests
- Keyboard navigation reaches all interactive elements
- Color contrast meets WCAG 2.1 AA
- Screen reader announces node selection

## Exit Protocol
- ALL pass: "All frontend testing gates passed"
- Test fails: Report name + assertion, STOP
- TypeScript error: Report file:line, STOP
- Build fails: Report errors, STOP
- Lint fails: Report violations, STOP
