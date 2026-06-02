# CI Pipeline — GitHub Actions Continuous Integration

## Purpose
Validate and run the CI pipeline for all Paryty components. Covers build matrix, test gates, lint gates, security scanning, and artifact generation.

## Pipeline Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    CI Pipeline                           │
├──────────┬──────────┬──────────┬──────────┬─────────────┤
│  Lint    │  Build   │  Test    │ Security │  Artifact   │
│  Gate    │  Gate    │  Gate    │  Gate    │  Gate       │
├──────────┼──────────┼──────────┼──────────┼─────────────┤
│ Rust     │ Rust     │ Unit     │ cargo    │ Binary      │
│ Go       │ Go       │ Race     │ audit    │ Container   │
│ TS       │ TS       │ Coverage │ gosec    │ Frontend    │
│ Proto    │ Proto    │ Bench    │ npm      │ Proto stubs │
└──────────┴──────────┴──────────┴──────────┴─────────────┘
```

## Execution Steps

### Step 1: Validate Workflow Syntax
Run: gh workflow view ci.yml 2>&1
Expected: Workflow is valid YAML with correct triggers.

### Step 2: Validate Build Matrix
Run: gh workflow view ci.yml --yaml 2>&1
Expected: Matrix includes Rust, Go, Node.js with correct versions.

### Step 3: Run CI Locally (Act)
Run: act -j lint 2>&1
Expected: Lint job completes successfully.

### Step 4: Check Workflow Status
Run: gh run list --workflow=ci.yml --limit=5 2>&1
Expected: Recent runs show green status.

## Pipeline Gates

### Gate 1: Lint (Parallel)
- **Rust**: `cargo fmt --check && cargo clippy -- -D warnings`
- **Go**: `golangci-lint run ./...`
- **TypeScript**: `npx eslint src/ && npx tsc --noEmit`
- **Proto**: `buf lint && buf breaking`

### Gate 2: Build (Parallel)
- **Rust**: `cargo build --release`
- **Go**: `go build ./cmd/...`
- **TypeScript**: `npm run build`
- **Proto**: `buf generate`

### Gate 3: Test (Sequential after Build)
- **Rust**: `cargo test`
- **Go**: `go test -race -count=1 ./...`
- **TypeScript**: `npx vitest run`
- **Integration**: `go test -tags=integration ./...`

### Gate 4: Security (Parallel after Test)
- **Rust**: `cargo audit`
- **Go**: `govulncheck ./...`
- **TypeScript**: `npm audit --audit-level=high`
- **Container**: `trivy image --severity HIGH,CRITICAL`

### Gate 5: Artifact (After all gates pass)
- **Binary**: Upload Rust/Go binaries
- **Container**: Build and push container images
- **Frontend**: Upload frontend build artifacts
- **Proto**: Upload generated stubs

## Exit Protocol
- ALL gates pass: Report "CI pipeline passed"
- Lint fails: Report file:line and violation, STOP
- Build fails: Report compilation errors, STOP
- Test fails: Report test name and assertion, STOP
- Security fails: Report CVE or vulnerability, STOP
- Artifact fails: Report upload error, STOP

## Notes
- CI runs on every push and pull request
- Parallel jobs reduce total pipeline time
- Security gate is advisory on PRs, blocking on main
- Artifact upload only on main branch pushes
