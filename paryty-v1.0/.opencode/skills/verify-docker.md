# Verify Docker — Container Image Verification

## Purpose
Validate Dockerfiles for best practices, security hardening, and optimization. Run after any Dockerfile change.

## Execution Steps

### Step 1: Lint Dockerfiles
Run: hadolint deploy/docker/Dockerfile.cluster 2>&1
Run: hadolint deploy/docker/Dockerfile.frontend 2>&1
Expected: No critical warnings. Report best practice suggestions.

### Step 2: Build Images
Run: docker build -f deploy/docker/Dockerfile.cluster -t paryty-cluster:verify cluster/ 2>&1
Run: docker build -f deploy/docker/Dockerfile.frontend -t paryty-frontend:verify frontend/ 2>&1
Expected: Images build successfully.

### Step 3: Scan for Vulnerabilities
Run: trivy image --severity HIGH,CRITICAL paryty-cluster:verify 2>&1
Run: trivy image --severity HIGH,CRITICAL paryty-frontend:verify 2>&1
Expected: No critical/high vulnerabilities.

### Step 4: Check Image Size
Run: docker images paryty-cluster:verify --format "{{.Size}}"
Run: docker images paryty-frontend:verify --format "{{.Size}}"
Expected: Cluster < 100MB, Frontend < 50MB.

### Step 5: Verify Multi-Stage Build
Check: Dockerfile uses multi-stage build
Check: Final stage uses minimal base image (alpine, distroless, scratch)
Check: Build dependencies not in final image

### Step 6: Security Hardening Check
Check: Non-root user specified (USER directive)
Check: No unnecessary packages installed
Check: Read-only filesystem where possible
Check: No secrets in image layers

## Best Practices Checklist

### Base Image
- [ ] Uses specific tag (not :latest)
- [ ] Uses minimal base (alpine, distroless, scratch)
- [ ] Base image is from trusted registry

### Multi-Stage Build
- [ ] Build stage separate from runtime stage
- [ ] Only necessary artifacts copied to runtime
- [ ] Build dependencies not in final image

### Security
- [ ] Non-root user specified
- [ ] No secrets in image
- [ ] No unnecessary packages
- [ ] Health check defined

### Optimization
- [ ] Layer caching optimized (frequently changing layers last)
- [ ] .dockerignore used
- [ ] Minimal number of layers
- [ ] No unnecessary files copied

### Paryty-Specific
- [ ] Cluster: Go binary statically linked (CGO_ENABLED=0)
- [ ] Cluster: CA certificates installed
- [ ] Frontend: nginx config included
- [ ] Frontend: Build artifacts in correct location

## Exit Protocol
- ALL checks pass: Report "Docker verification passed"
- Hadolint critical: Report rule and Dockerfile:line, STOP
- Build fails: Report error output, STOP
- Vulnerability found: Report CVE and severity, STOP
- Image too large: Report size and suggestion, STOP

## Notes
- Run after any Dockerfile change
- Vulnerability scanning requires Trivy installed
- Hadolint requires separate installation
