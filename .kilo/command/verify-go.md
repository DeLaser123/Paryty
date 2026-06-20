---
description: Run full Go verification pipeline — build, vet, test with race detector
agent: go-cluster-engineer
---
# Verify Go — Paryty Cluster Verification Pipeline

Run the full Go verification pipeline for the Paryty Cluster. After every Go code change, all gates must pass.

## Execution Steps

### Gate 1: Build
```bash
cd paryty-v1.0/cluster && go build ./... 2>&1
```
Expected: Clean compilation.

### Gate 2: Vet
```bash
cd paryty-v1.0/cluster && go vet ./... 2>&1
```
Expected: Zero issues.

### Gate 3: Test with Race Detector
```bash
cd paryty-v1.0/cluster && go test -race -count=1 ./... 2>&1
```
Expected: All tests pass with race detector enabled.

## Coverage Requirements
| Package | Minimum |
|---|---|
| internal/processing/ | 80% |
| internal/storage/ | 75% |
| internal/stream/ | 75% |
| internal/api/ | 70% |
| internal/models/ | 90% |

## Checklist
- [ ] No ignored errors
- [ ] All functions accept context.Context
- [ ] No goroutine leaks
- [ ] No panic() in library code
- [ ] slog for all logging

## Exit Protocol
- ALL gates pass: Report "Go verification passed" with raw output
- Build fails: Report compilation errors, STOP
- Vet fails: Report file:line and issue, STOP
- Test fails: Report test name, assertion, file:line, STOP
- Race detected: Report goroutine stacks, STOP

Show raw, unfiltered output from each gate. Never summarize or redact.
