---
name: code-review
description: Structured code review for Paryty — checks correctness, security, performance, error handling, and adherence to architecture rules and coding standards.
---
# Code Review — Paryty Structured Review

## Review Dimensions

### 1. Architecture Compliance
- No layer skipping
- No circular dependencies
- Approved protocols only
- Multi-tenancy maintained

### 2. Coding Standards
Apply the relevant language bible from `.qoder/rules/coding-standards-*.md`.

### 3. Security Review
- No credentials in code/logs
- Input validated/sanitized
- Tenant isolation
- TLS/mTLS enforced

### 4. Performance Review
- No N+1 queries
- No allocations in hot paths
- Bounded state

### 5. Testing Review
- Unit tests for new logic
- Edge cases covered
- Integration tests for cross-component changes

### 6. Locked Decisions Compliance
Cross-reference with `.qoder/rules/locked-decisions.md`.

### 7. Bug Fix Quality
Cross-reference with `.qoder/rules/bug-fix-discipline.md`.

## Finding Severity
- Critical: Must fix before merge
- High: Should fix before merge
- Medium: Follow-up
- Low: Nice to have

## Output Format
```
Code Review Summary
Scope: [files]
Component: [component]
Bible Applied: [bible]

### Critical
1. [file:line] — description — suggested fix

### High
1. [file:line] — description — suggested fix

### Verdict: [APPROVED / CHANGES REQUESTED]
```
