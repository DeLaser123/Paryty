---
name: verify-python
description: "Run the full Python verification pipeline for Paryty Intelligence Layer: compile, type check, lint, test, and coverage. Use after any Python code change."
---

# Verify Python: Paryty Intelligence Layer Verification Pipeline

## Execution Steps

### Gate 1: Compile Check
```bash
cd <service-dir> && python -m py_compile *.py 2>&1
```
Expected: Zero syntax errors.

### Gate 2: Type Check (Strict)
```bash
cd <service-dir> && mypy --strict *.py 2>&1
```
Expected: Zero type errors. All functions must have type hints.

### Gate 3: Lint
```bash
cd <service-dir> && ruff check . 2>&1
```
Expected: Zero lint errors.

### Gate 4: Unit Tests
```bash
cd <service-dir> && python -m pytest -v 2>&1
```
Expected: All tests pass.

### Gate 5: Coverage Report
```bash
cd <service-dir> && python -m pytest --cov=. --cov-report=term-missing 2>&1
```
Expected: Overall coverage >= 80%.

## Coverage Requirements
| Module | Minimum |
|---|---|
| forecasting/ | 85% |
| anomaly_detection/ | 85% |
| drift_detection/ | 80% |
| model_store/ | 80% |
| gRPC service handlers | 75% |

## Checklist
- [ ] No bare except without exception type
- [ ] No import * anywhere
- [ ] All functions have type hints
- [ ] No pickle for untrusted data
- [ ] structlog for all logging (not print)
- [ ] Model artifacts cached at startup (not loaded per-request)
- [ ] Pydantic validation at gRPC boundary
- [ ] OTel SDK used for metrics (not language-native)

## Exit Protocol
- ALL gates pass: Report "Python verification passed"
- Compile fails: Report file:line and syntax error, STOP
- Type check fails: Report file:line and type error, STOP
- Lint fails: Report file:line and rule violation, STOP
- Test fails: Report test name and assertion, STOP
- Coverage below threshold: Report modules below, STOP