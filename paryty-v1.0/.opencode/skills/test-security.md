# Test Security — Security Scanning and Vulnerability Testing

## Purpose
Run security scans across all Paryty components: dependency vulnerability scanning, static analysis, container scanning, secret detection, and SAST.

## Execution Steps

### Step 1: Rust Dependency Audit
Run: cd agent && cargo audit 2>&1
Expected: No known vulnerabilities. Report CVE details if found.

### Step 2: Rust Dependency License Check
Run: cd agent && cargo deny check licenses 2>&1
Expected: All dependencies have compatible licenses (MIT, Apache-2.0, BSD).

### Step 3: Go Vulnerability Scan
Run: cd cluster && govulncheck ./... 2>&1
Expected: No known vulnerabilities. Report CVE details if found.

### Step 4: Go Static Analysis (gosec)
Run: cd cluster && gosec ./... 2>&1
Expected: No high/critical issues. Report medium issues.

### Step 5: npm Audit (Frontend)
Run: cd frontend && npm audit --audit-level=high 2>&1
Expected: No high/critical vulnerabilities.

### Step 6: Container Image Scanning (Trivy)
Run: trivy image paryty-cluster:latest 2>&1
Run: trivy image paryty-agent:latest 2>&1
Run: trivy image paryty-frontend:latest 2>&1
Expected: No critical/high vulnerabilities in container images.

### Step 7: Secret Detection (gitleaks)
Run: gitleaks detect --source . --report-format json --report-path gitleaks-report.json 2>&1
Expected: No secrets detected in repository.

### Step 8: Dockerfile Security Check
Run: hadolint deploy/docker/Dockerfile.cluster 2>&1
Run: hadolint deploy/docker/Dockerfile.frontend 2>&1
Expected: No critical warnings. Report best practice suggestions.

### Step 9: Dependency License Inventory
Run: cd cluster && go-licenses csv ./... 2>&1
Run: cd frontend && npx license-checker --summary 2>&1
Expected: Generate license inventory for compliance.

## Security Checklist

### Dependency Security
- [ ] No known CVEs in Rust dependencies (cargo audit)
- [ ] No known CVEs in Go dependencies (govulncheck)
- [ ] No known CVEs in npm dependencies (npm audit)
- [ ] All licenses compatible (cargo deny, go-licenses)

### Static Analysis
- [ ] No high/critical gosec findings
- [ ] No unsafe Rust code without SAFETY comments
- [ ] No hardcoded credentials
- [ ] No SQL injection vectors
- [ ] No path traversal vectors

### Container Security
- [ ] No critical vulnerabilities in base images
- [ ] No root user in containers
- [ ] No unnecessary packages installed
- [ ] Read-only filesystem where possible
- [ ] Non-root UID specified

### Secret Detection
- [ ] No API keys in code
- [ ] No passwords in code
- [ ] No private keys in code
- [ ] No tokens in code
- [ ] .env files in .gitignore

## Exit Protocol
- ALL checks pass: Report "All security checks passed"
- CVE found: Report CVE ID, severity, affected package, and fix version, STOP
- Secret detected: Report file, line, and secret type (redacted), STOP
- Container vulnerability: Report image, vulnerability, and severity, STOP
- License incompatibility: Report package and license, STOP

## Tool Installation
```bash
# Rust
cargo install cargo-audit
cargo install cargo-deny

# Go
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install github.com/google/go-licenses@latest

# Container
winget install Trivy
winget install Hadolint

# Secrets
winget install Gitleaks
```

## Notes
- Run security scans before every release
- CVE findings require immediate triage
- Secret detection should run in CI pipeline
- License compliance required for all dependencies
