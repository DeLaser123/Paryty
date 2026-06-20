---
description: Run full Python verification pipeline — compile, type check, lint, test, coverage
agent: python-ml-engineer
---
# Verify Python — Paryty Intelligence Layer Verification Pipeline

Run the full Python verification pipeline. After every Python code change, all gates must pass.

## Execution Steps

### Gate 1: Compile Check
```bash
cd <intelligence-service-dir> && python -m py_compile *.py 2>&1
```

### Gate 2: Type Check (Strict)
```bash
cd <intelligence-service-dir> && mypy --strict *.py 2>&1
```
Expected: Zero type errors. All functions must have type hints.

### Gate 3: Lint
```bash
cd <intelligence-service-dir> && ruff check . 2>&1
```
Expected: Zero lint errors.

### Gate 4: Unit Tests
```bash
cd <intelligence-service-dir> && python -m pytest -v 2>&1
```

### Gate 5: Coverage Report
```bash
cd <intelligence-service-dir> && python -m pytest --cov=. --cov-report=term-missing 2>&1
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
- [ ] structlog for all logging
- [ ] Pydantic validation at gRPC boundary
- [ ] OTel SDK used for metrics

## Exit Protocol
- ALL gates pass: Report "Python verification passed" with raw output
- Any gate fails: Report the specific failure with file:line, STOP

Show raw, unfiltered output from each gate.
