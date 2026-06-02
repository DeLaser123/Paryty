# Verify Go — Paryty Cluster Go Compilation Check

## Purpose
Run the full Go verification pipeline on Paryty cluster or Go SDK code. Execute after any Go code change in cluster/ or agent/go_sdk/.

## Execution Steps

### Step 1: Build Check
Run: go build ./... 2>&1
Expected: Compilation succeeds with zero errors.

### Step 2: Vet
Run: go vet ./... 2>&1
Expected: Zero issues.

### Step 3: Race Tests
Run: go test -race -count=1 ./... 2>&1
Expected: All tests pass, no race conditions detected.

### Step 4: Escape Analysis (Hot Paths Only)
Run: go build -gcflags="-m" ./... 2>&1 | Select-String "escapes to heap"
Expected: Verify no unexpected heap escapes in hot paths (ingestion, processing).

## Exit Protocol
- ALL gates pass: Report "All verification gates passed"
- Any gate fails: Report which gate, the full error, and STOP
- Race detected: Report data race details (goroutine stacks), STOP

## Notes
- Must run from cluster/ or agent/go_sdk/ directory
- Race tests require CGO_ENABLED=1 on some systems
