---
name: verify-go
description: Run the full Go verification pipeline — build, vet, test with race detector. Use after any Go code change.
---
# Verify Go — Paryty Cluster Verification Pipeline

## Execution Steps

### Gate 1: Build
```bash
cd paryty-v1.0/cluster && go build ./... 2>&1
```

### Gate 2: Vet
```bash
cd paryty-v1.0/cluster && go vet ./... 2>&1
```

### Gate 3: Test with Race Detector
```bash
cd paryty-v1.0/cluster && go test -race -count=1 ./... 2>&1
```

## Exit Protocol
- ALL pass: "Go verification passed"
- Any fail: Report specific failure with file:line, STOP
- Show raw, unfiltered output from each gate
