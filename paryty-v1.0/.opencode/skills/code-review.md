# Code Review — Paryty Code Review Checklist

## Purpose
Systematic code review process for all Paryty components. Covers correctness, security, performance, architecture, and API design.

## Review Process

### Step 1: Understand the Change
- Read the PR description and linked issues
- Understand what problem is being solved
- Check if the approach aligns with architecture docs

### Step 2: Review Checklist

#### Correctness
- [ ] Code does what the PR description says
- [ ] Edge cases are handled
- [ ] Error handling is comprehensive
- [ ] No silent failures (errors are logged/returned)
- [ ] Concurrency is correct (no races, deadlocks)
- [ ] Resource cleanup (defer Close, Drop, finally)

#### Security
- [ ] No hardcoded credentials or secrets
- [ ] Input validation on all external data
- [ ] No SQL injection vectors
- [ ] No path traversal vectors
- [ ] TLS for all network communication
- [ ] No unsafe code without SAFETY comments
- [ ] No credentials in logs

#### Performance
- [ ] No unnecessary allocations in hot paths
- [ ] Bounded queues and buffers
- [ ] Appropriate data structures
- [ ] No N+1 queries
- [ ] Pagination for large result sets
- [ ] Caching where appropriate

#### Architecture
- [ ] Follows component boundaries
- [ ] Correct layer (ingestion/processing/query/storage)
- [ ] No circular dependencies
- [ ] Interfaces over implementations
- [ ] Single responsibility principle

#### API Design
- [ ] REST endpoints follow conventions
- [ ] gRPC methods follow proto definitions
- [ ] Error codes are meaningful
- [ ] Request/response schemas documented
- [ ] Backward compatibility maintained

#### Testing
- [ ] Unit tests for new code
- [ ] Integration tests for cross-component changes
- [ ] Edge cases covered in tests
- [ ] No flaky tests introduced
- [ ] Coverage meets minimum threshold

#### Documentation
- [ ] Code comments explain "why" not "what"
- [ ] Public APIs documented
- [ ] Complex algorithms explained
- [ ] Configuration changes documented

### Step 3: Language-Specific Checks

#### Rust
- [ ] No unwrap() in library code
- [ ] Proper error propagation with ? operator
- [ ] Lifetime annotations correct
- [ ] No unnecessary cloning
- [ ] Unsafe blocks have SAFETY comments
- [ ] Clippy warnings addressed

#### Go
- [ ] Error handling (no ignored errors)
- [ ] Context propagation
- [ ] Goroutine lifecycle managed
- [ ] No mutex held across I/O
- [ ] Defer for cleanup
- [ ] Table-driven tests

#### TypeScript
- [ ] Proper TypeScript types (no any)
- [ ] React hooks rules followed
- [ ] No direct DOM manipulation
- [ ] State management via Zustand
- [ ] API calls via established patterns

### Step 4: Component-Specific Checks

#### Agent (Rust)
- [ ] Metal scraper: minimal syscalls
- [ ] eBPF: no panics in kernel context
- [ ] Communication: reconnection logic correct
- [ ] Edge buffer: bounded memory, TTL eviction

#### Cluster (Go)
- [ ] Ingestion: tenant isolation
- [ ] Processing: idempotent operations
- [ ] Storage: correct tier routing
- [ ] Query: pagination and limits

#### Frontend (TypeScript)
- [ ] GPU rendering: no main-thread blocking
- [ ] State: Zustand for global state
- [ ] API: proper error handling
- [ ] Performance: 60fps target

## Exit Protocol
- ALL checks pass: Approve PR
- Critical issue: Request changes, block merge
- Minor issue: Comment with suggestion
- Question: Ask for clarification

## Notes
- Review within 24 hours of PR creation
- At least one approval required
- Critical changes require two approvals
- Security-sensitive changes require security-auditor review
