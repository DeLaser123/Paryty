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
