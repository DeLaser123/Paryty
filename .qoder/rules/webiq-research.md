# WebIQ Research-First Protocol

**Pillar 5 of Agent Governance. Active on every task involving external libraries, APIs, or frameworks. Training data is stale; the web is current.**

## Core Principle

**Your training data has a knowledge cutoff. The web does not.** Before implementing any integration with an external library, API, or framework, you MUST research the current state of that technology via web search. Do not rely on your training data for:
- Current API signatures
- Current version numbers
- Current configuration options
- Current known issues or breaking changes
- Current best practices

**"I know this from my training data" is a liability, not an asset, when dealing with fast-moving libraries.**

## When Research Is MANDATORY

Research is not optional in these situations. If you skip research and implement based on training data alone, you are violating this rule.

| Situation | Research Required |
|-----------|-------------------|
| Using a library you haven't read the docs for in THIS session | WebSearch for current docs |
| Calling an API from an external crate/package/module | Read current API reference |
| Configuring a tool (Dragonfly, QuestDB, Redpanda, etc.) | Search for current configuration docs |
| Dealing with version-specific behavior | Search for the specific version's docs |
| Encountering an error from an external tool | Search for the error message and known solutions |
| Implementing a protocol (gRPC, Protobuf, ILP, etc.) | Search for current spec and implementation details |
| Using a Rust crate | Check crates.io for current version, read current docs |
| Using a Go module | Check pkg.go.dev for current API |
| Using an npm package | Check npmjs.com for current version and API |
| Using a Python package | Check PyPI for current version, read current docs |
| Cross-compiling or targeting a specific platform | Search for current platform-specific requirements |

## The Research Workflow

### Step 1: Identify What You Need to Know
Before searching, be specific:
- "What is the current tonic API for bidirectional streaming in version 0.12?"
- "What is the current QuestDB ILP protocol format?"
- "What are the current Redpanda Podman rootless requirements?"

### Step 2: Search
Use available tools in order of specificity:
1. **context7 MCP** (`resolve-library-id` + `query-docs`): Best for library-specific documentation
2. **WebSearch**: Best for current information, known issues, community solutions
3. **WebFetch**: Best for reading a specific documentation page you found via search
4. **Read source code**: Best when docs are insufficient — read the actual library source

### Step 3: Verify the Information
- **Check the version**: Make sure the docs/API you found match the version in your `Cargo.toml` / `go.mod` / `package.json`
- **Check the date**: Prefer recent sources (within the last year)
- **Cross-reference**: If possible, verify against multiple sources (official docs + source code)

### Step 4: Document Your Findings
In your response, cite your sources:
```
Based on the tonic 0.12 documentation (https://docs.rs/tonic/0.12/...), 
the bidirectional streaming API uses [specific API].
```

### Step 5: Implement Based on Research
Write code based on the current documentation, not on your memory of how the API used to work.

## Research Tools Priority

| Tool | Use When | How |
|------|----------|-----|
| context7 MCP | You know the library name and need its docs | `resolve-library-id` → `query-docs` |
| WebSearch | You need current info, known issues, or don't know the exact docs URL | Search with specific terms |
| WebFetch | You have a specific URL to read | Fetch the page content |
| Read (source) | Docs are insufficient, need to see actual implementation | Read the library's source files |
| Bash (cargo doc, go doc) | Quick local API reference | Run doc commands for installed packages |

## Common Hallucination Scenarios and Prevention

### Scenario 1: API That Doesn't Exist
**Hallucination**: Calling `client.stream_metrics()` when the actual API is `client.send_metrics_stream()`
**Prevention**: Read the current API docs before writing the call

### Scenario 2: Wrong Version Behavior
**Hallucination**: Using tonic 0.8 API patterns with tonic 0.12
**Prevention**: Check the version in Cargo.toml, search for that version's docs

### Scenario 3: Configuration That Doesn't Work
**Hallucination**: Setting a config option that was removed or renamed
**Prevention**: Search for current configuration documentation

### Scenario 4: Non-Existent Feature
**Hallucination**: Using a feature flag or option that doesn't exist in the current version
**Prevention**: Check the actual source code or current docs

### Scenario 5: Platform-Specific Assumption
**Hallucination**: Assuming a Linux-only API works on Windows
**Prevention**: Search for platform compatibility information

### Scenario 6: Breaking Change Ignorance
**Hallucination**: Using an API that was deprecated or changed in a recent version
**Prevention**: Search for "[library] breaking changes [version]" or read the changelog

## Research Evidence in Completion Reports

When a task involves external libraries, the completion report (see `proof-of-completion.md`) MUST include:

```
### Research Conducted:
- Library: [name and version from Cargo.toml/go.mod/package.json]
- Sources consulted: [URLs or documentation references]
- Key findings: [relevant API details, version-specific behavior, known issues]
- How findings influenced implementation: [specific decisions based on research]
```

## The "Don't Guess, Search" Heuristic

When you're about to write code that calls an external API and you're not 100% certain of the signature:

1. **STOP** typing the call
2. **SEARCH** for the current documentation
3. **READ** the actual API reference
4. **IMPLEMENT** based on what you read
5. **VERIFY** by compiling and running

**The 30 seconds you spend searching will save 30 minutes of debugging a hallucinated API.**

## Research Is Not a Sign of Weakness

Research is a sign of engineering discipline. The best human engineers look things up constantly. They don't rely on memory for API signatures, version compatibility, or configuration options. Neither should you.

**An agent that researches before implementing is more trustworthy than one that "knows" from stale training data.**

## Relationship to Other Rules

- **Parent rule**: `agent-governance.md` (The Paryty Truth Protocol)
- **Companion rule**: `truth-over-perfection.md` (research is how you handle "I don't know")
- **Companion rule**: `verify-before-assert.md` (research findings must be verified by compilation)
- **Companion rule**: `anti-deception.md` (detects hallucinated APIs and stale-data claims)
- **Complements**: `locked-decisions.md` (research validates that locked choices are still viable)
