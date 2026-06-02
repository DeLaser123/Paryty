---
name: verify-go
description: Run the full Go verification pipeline for the Paryty Cluster — build, vet, test with race detector. Use after any Go code change.
---

# Verify Go — Paryty Cluster Verification Pipeline

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

### Gate 3: Unit Tests with Race Detector
```bash
cd paryty-v1.0/cluster && go test -race -count=1 ./... 2>&1
```
Expected: All tests pass with race detector enabled.

### Gate 4: Integration Tests (requires infrastructure)
```bash
cd paryty-v1.0/cluster && go test -v -tags=integration -count=1 ./... 2>&1
```
Expected: All integration tests pass. Requires Redpanda, Dragonfly, QuestDB running.

### Gate 5: Coverage Report
```bash
cd paryty-v1.0/cluster && go test -coverprofile=coverage.out -covermode=atomic ./... 2>&1
cd paryty-v1.0/cluster && go tool cover -func=coverage.out | Select-String -Pattern "total"
```
Expected: Overall coverage >= 80%.

## Coverage Requirements
| Package | Minimum |
|---|---|
| internal/processing/ | 80% |
| internal/storage/ | 75% |
| internal/stream/ | 75% |
| internal/api/ | 70% |
| internal/models/ | 90% |

## Checklist
- [ ] No ignored errors without `_ =`
- [ ] All functions accept context.Context
- [ ] No goroutine leaks
- [ ] No panic() in library code
- [ ] slog for all logging

## Exit Protocol
- ALL gates pass: Report "Go verification passed"
- Build fails: Report compilation errors, STOP
- Vet fails: Report file:line and issue, STOP
- Test fails: Report test name, assertion, file:line, STOP
- Race detected: Report goroutine stacks, STOP
- Coverage below threshold: Report packages below, STOP
