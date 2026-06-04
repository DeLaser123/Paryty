# Verify Before Assert

**Pillar 4 of Agent Governance. Active on every claim. No assertion without tool execution.**

## Core Principle

**You cannot claim something works unless you have run the tool that proves it.** "Should work" is not a valid claim. "I think this compiles" is not a valid claim. "This follows the pattern from the other file" is not a valid claim.

The only valid claim is: "I ran [tool] and it produced [raw output], which confirms [specific assertion]."

## The Assertion-Proof Table

Every assertion type requires a specific tool invocation. Making an assertion without running the corresponding tool is a violation.

| Assertion | Required Tool | Required Output |
|-----------|--------------|-----------------|
| "This code compiles" | `cargo build` / `go build` / `npm run build` / `python -m py_compile` | Exit code 0 with raw output |
| "Tests pass" | `cargo test` / `go test -v -race` / `npm test` / `pytest -v` | Raw output showing pass/fail per test |
| "No lint warnings" | `cargo clippy -- -D warnings` / `golangci-lint run` / `eslint .` / `ruff check` | Exit code 0 with raw output |
| "Types are correct" | `cargo check` / `go vet` / `tsc --noEmit` / `mypy .` | Exit code 0 with raw output |
| "This API exists in [library]" | WebSearch + WebFetch / context7 / Read library source | Documentation or source reference |
| "This config is valid" | Run the config parser/loader | Successful parse output |
| "This protobuf compiles" | `buf generate` or `protoc` | Exit code 0 with generated files |
| "This Dockerfile builds" | `podman build` or `docker build` | Successful build output |
| "This Helm chart is valid" | `helm template` / `helm lint` | Exit code 0 with rendered output |
| "This K8s manifest applies" | `kubectl apply --dry-run=client` | Successful dry-run output |
| "This dependency exists" | Check `Cargo.toml` / `go.mod` / `package.json` | Dependency listed with correct version |

## Mandatory Verification Workflow

For ANY code change, follow this exact sequence:

### 1. Before Making Changes
```
Read the current state of the file(s) to understand what exists.
Do NOT assume you remember the contents from earlier in the session.
```

### 2. After Making Changes
```
Step A: Build verification
  → Run the language-specific build command
  → Show raw output
  → If build fails: fix the error, re-run, show new output

Step B: Type/lint verification
  → Run the language-specific type check and linter
  → Show raw output
  → If failures: fix them, re-run, show new output

Step C: Test verification
  → Run the test suite relevant to the changed code
  → Show raw output including per-test results
  → If failures: investigate, fix, re-run, show new output

Step D: Integration verification (if applicable)
  → If the change crosses a language boundary, verify BOTH sides
  → If the change affects a proto file, verify codegen for all languages
```

### 3. In the Completion Report
```
Include ALL raw output from steps A-D.
Do not summarize. Do not redact. Do not select only passing results.
```

## The "Read Before Write" Rule

Before modifying any file:

1. **Read the file first** — even if you think you know its contents from earlier in the session
2. **Read related files** — imports, dependencies, callers, tests
3. **Verify assumptions** — if you assume a function exists, search for it; if you assume a type, check the definition

**Never modify a file based on memory alone.** Files change. Other agents modify files. Your memory of a file from 10 messages ago may be wrong.

## The "Run Before Claim" Rule

Before claiming anything about runtime behavior:

1. **Compilation is not optional** — never claim code compiles without running the compiler
2. **Tests are not optional** — never claim tests pass without running the test runner
3. **Lint is not optional** — never claim code is clean without running the linter
4. **Type checks are not optional** — never claim types are correct without running the type checker

### Specific Prohibitions:

| Prohibited Claim | Why |
|-----------------|-----|
| "This should compile" | "Should" is not "does." Run the compiler. |
| "Tests should pass" | "Should" is not "did." Run the tests. |
| "I've verified this locally" | No output shown = no verification happened |
| "The code follows the pattern" | Patterns can be wrong. Run the tools. |
| "This is a simple change" | Simple changes break things too. Run the tools. |
| "No need to test this" | Every change needs verification. Run the tools. |
| "The fix is straightforward" | Straightforward fixes have straightforward bugs. Run the tools. |

## Verification for Multi-Language Changes

Paryty is a multi-language project. Changes often cross boundaries.

### Protobuf Changes:
```
1. Edit .proto file
2. Run buf generate (or protoc)
3. Verify Rust codegen: cargo build in agent/
4. Verify Go codegen: go build in cluster/
5. Verify TypeScript codegen: npm run build in frontend/
6. Show all raw output
```

### Configuration Changes:
```
1. Edit config file
2. Verify the config parser accepts it
3. Verify the service starts with the new config
4. Show all raw output
```

### Dependency Changes:
```
1. Edit Cargo.toml / go.mod / package.json
2. Run dependency resolution (cargo update / go mod tidy / npm install)
3. Run full build
4. Run full test suite
5. Show all raw output
```

## The "Verify Fresh" Principle

Verification must be fresh — from the current session, after the current changes.

- **Stale verification**: Running tests once at the beginning and claiming they still pass after 20 code changes
- **Fresh verification**: Running tests after the last code change and showing the output

**If you make more changes after your last verification, your verification is stale. Re-run.**

## Relationship to Other Rules

- **Parent rule**: `agent-governance.md` (The Paryty Truth Protocol)
- **Companion rule**: `proof-of-completion.md` (verification output is the evidence for completion)
- **Companion rule**: `truth-over-perfection.md` (verification is how you back up your claims)
- **Companion rule**: `anti-deception.md` (detects unverified claims)
- **Complements**: `bug-fix-discipline.md` (bug fixes require regression test verification)
- **Complements**: `coding-standards*.md` (coding standards require lint/type verification)
