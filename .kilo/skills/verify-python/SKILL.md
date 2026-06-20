---
name: verify-python
description: Run the full Python verification pipeline — compile, type check, lint, test, coverage. Use after any Python code change.
---
# Verify Python — Paryty Intelligence Layer Verification Pipeline

## Execution Steps

### Gate 1: Compile Check
```bash
python -m py_compile *.py 2>&1
```

### Gate 2: Type Check (Strict)
```bash
mypy --strict *.py 2>&1
```

### Gate 3: Lint
```bash
ruff check . 2>&1
```

### Gate 4: Unit Tests
```bash
python -m pytest -v 2>&1
```

### Gate 5: Coverage Report
```bash
python -m pytest --cov=. --cov-report=term-missing 2>&1
```
Expected: Overall coverage >= 80%.

## Exit Protocol
- ALL pass: "Python verification passed"
- Any fail: Report specific failure with file:line, STOP
- Show raw, unfiltered output from each gate
