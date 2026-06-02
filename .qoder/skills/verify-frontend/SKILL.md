---
name: verify-frontend
description: Run the full frontend verification pipeline for Paryty — TypeScript check, build, and tests. Use after any TypeScript/React code change.
---

# Verify Frontend — Paryty Frontend Verification Pipeline

## Execution Steps

### Gate 1: TypeScript Check
```bash
cd paryty-v1.0/frontend && npx tsc --noEmit 2>&1
```
Expected: Zero type errors.

### Gate 2: Build
```bash
cd paryty-v1.0/frontend && npm run build 2>&1
```
Expected: Clean build with no warnings.

### Gate 3: Unit Tests
```bash
cd paryty-v1.0/frontend && npx vitest run 2>&1
```
Expected: All tests pass.

### Gate 4: Lint
```bash
cd paryty-v1.0/frontend && npx eslint src/ 2>&1
```
Expected: Zero lint errors.

## Checklist
- [ ] No `any` type used
- [ ] All components are functional with hooks
- [ ] `data-testid` on interactive elements
- [ ] Zustand stores properly typed
- [ ] No prop drilling >3 levels

## Exit Protocol
- ALL gates pass: Report "Frontend verification passed"
- TypeScript fails: Report error with file:line, STOP
- Build fails: Report build errors, STOP
- Test fails: Report test name and assertion, STOP
- Lint fails: Report file:line and rule violation, STOP
