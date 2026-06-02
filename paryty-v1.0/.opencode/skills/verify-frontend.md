# Verify Frontend — Paryty TypeScript Build Check

## Purpose
Run the full TypeScript/frontend verification pipeline. Execute after any TS/TSX/CSS change in frontend/.

## Execution Steps

### Step 1: Type Check
Run: npx tsc --noEmit 2>&1
Expected: Zero type errors.

### Step 2: Lint
Run: npx eslint src/ 2>&1
Expected: Zero errors (warnings OK but report them).

### Step 3: Build
Run: npm run build 2>&1
Expected: Successful production build.

### Step 4: WebGPU Check (Manual)
Verify: WebGPU detection in engine/index.ts has proper WebGL 2.0 fallback.
Check: navigator.gpu detection and fallback initialization.

## Exit Protocol
- ALL gates pass: Report "All verification gates passed"
- Type errors: Report file:line and error message, STOP
- ESLint errors: Report file:line and rule violation, STOP
- Build failure: Report full error output, STOP

## Notes
- Must run from frontend/ directory
- WebGPU check is advisory on non-WebGPU systems
