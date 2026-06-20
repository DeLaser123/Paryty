---
name: verify-frontend
description: Run the full frontend verification pipeline — TypeScript check, build, test, lint. Use after any TypeScript/React code change.
---
# Verify Frontend — Paryty Frontend Verification Pipeline

## Execution Steps

### Gate 1: TypeScript Check
```bash
cd paryty-v1.0/frontend && npx tsc --noEmit 2>&1
```

### Gate 2: Build
```bash
cd paryty-v1.0/frontend && npm run build 2>&1
```

### Gate 3: Unit Tests
```bash
cd paryty-v1.0/frontend && npx vitest run 2>&1
```

### Gate 4: Lint
```bash
cd paryty-v1.0/frontend && npx eslint src/ 2>&1
```

## Exit Protocol
- ALL pass: "Frontend verification passed"
- Any fail: Report specific failure with file:line, STOP
- Show raw, unfiltered output from each gate
