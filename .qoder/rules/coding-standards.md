# Cross-Language Coding Standards

Language-specific rules live in separate bibles: `coding-standards-rust.md`, `coding-standards-go.md`, `coding-standards-c.md`, `coding-standards-typescript.md`, `coding-standards-python.md`.

## Naming (All Languages)
- Descriptive names over abbreviations
- Prefix booleans with `is`, `has`, `should`
- Past tense for events (`created`, `deleted`, `received`)
- Constants in SCREAMING_SNAKE_CASE
- Domain terminology consistent across the codebase

## Documentation (All Languages)
- Document "why" not "what"
- Keep comments up to date — stale comments are worse than no comments
- TODO with issue number: `// TODO(paryty#123): ...`
- Remove commented-out code before merge

## Git (All Languages)
- Conventional commits: `feat:`, `fix:`, `docs:`, `chore:`, `test:`, `perf:`, `refactor:`
- Subject line under 72 characters
- Reference issue numbers
- One logical change per commit

## Code Organization (All Languages)
- Files under 500 lines
- Functions under 50 lines
- Extract complex logic into named functions
- Co-locate tests with source code
- Group related code together, unrelated code apart

## Error Philosophy (All Languages)
- Errors are values, not exceptions (where the language supports it)
- Propagate errors with context, never swallow silently
- Fail fast at boundaries, handle gracefully within
- Every error path must be tested

## Performance Philosophy (All Languages)
- Measure before optimizing — profile, don't guess
- No allocations in hot paths
- Pre-allocate when size is known
- Batch operations where possible
- Bounded state everywhere — caches, buffers, windows all have max sizes

## Bug Fix Discipline (All Languages)

**Principle: Fix once, never again.** Temporary fixes are worse than unfixed bugs.

This rule activates **automatically** whenever a bug, error, test failure, or unexpected behavior is reported — no slash command required. Any agent fixing anything must follow the full protocol.

When fixing any bug, follow the mandatory 7-step protocol:
1. **Reproduce** the bug reliably with a test before touching code
2. **Root Cause Analysis** — find WHY it exists, not just WHAT broke
3. **Class Elimination** — search entire codebase for the same anti-pattern and fix all instances
4. **Systemic Fix** — make the bug structurally impossible, not just checked at runtime
5. **Regression Test** — add a test that fails before and passes after the fix
6. **Environment Independence** — verify on Windows, WSL, Linux, after restart, under load
7. **Post-Mortem** — document root cause and why the fix is permanent

Full protocol: `bug-fix-discipline.md`

**Forbidden shortcuts:** symptom patching, `if nil` without asking why nil, `try/except` around the failure, fixing only the observed file, skipping regression tests, environment-specific workarounds, silencing error logs.
