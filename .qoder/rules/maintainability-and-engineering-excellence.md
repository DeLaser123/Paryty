# Maintainability & Engineering Excellence

**Pillar 6 of Agent Governance. Active on every code change. Code must be maintainable by humans AND AI agents — without sacrificing performance, memory efficiency, safety, or power.**

## Core Principle

**Code that works is worthless if it cannot be safely changed.** The value of software is not in its current behavior — it's in its capacity to evolve. A system that works today but cannot be modified tomorrow without breaking is a liability, not an asset.

But maintainability is NOT the only quality dimension. You must achieve ALL of these simultaneously:

| Quality | Standard |
|---------|----------|
| **Maintainable** | Any engineer — human or AI — can read, understand, modify, and extend without breaking unrelated code |
| **Performant** | Hot paths are allocation-free, data-oriented, cache-friendly, zero-copy where possible |
| **Memory-Efficient** | Right-sized types, no unnecessary heap allocation, bounded state, predictable layout |
| **Safe** | Type system enforces invariants, invalid states are unrepresentable, error paths are exhaustive |
| **Powerful** | System handles real-world scale — 10K-100K agents, real-time streaming, multi-tenant isolation |

**These qualities are NOT in tension. They coexist in the right design. If you think you must sacrifice one for another, you have not found the right design yet.**

## The Extreme Balance Rule

When you believe maintainability conflicts with another quality:

1. **Do NOT sacrifice performance for readability.** Find the design where both coexist.
2. **Do NOT sacrifice safety for simplicity.** The type system IS the simplification tool.
3. **Do NOT sacrifice memory efficiency for clarity.** Data-oriented design is both clear and efficient.
4. **Do NOT sacrifice power for approachability.** Well-named abstractions can hide complexity without losing capability.

**The test:** If a change improves one dimension but degrades another, the design is wrong. Find the pattern where the change improves both — or improves one without degrading the other. That pattern always exists.

## What "Maintainable" Actually Means

Maintainability is not comments. Maintainability is not documentation. Maintainability is **structural**: the code's shape, naming, boundaries, and invariants make it safe to change.

### The Next-Person Test

Every function, module, and system boundary must pass this test:

> "An engineer with no prior context can read this code and answer: What does it do? What are its inputs? What are its outputs? What can go wrong? Where are its boundaries? What must NOT be changed without understanding the full system?"

If any answer requires reading more than the immediate function and its direct dependencies, the code is not maintainable.

### The Blast Radius Test

Every change must pass this test:

> "Modifying this function/type/module should only affect code that directly depends on it. Code in unrelated modules, different tenants, different pipeline stages, or different components should be completely unaffected."

If a change to one module breaks code in an unrelated module, the architecture has a blast radius problem. The coupling must be found and eliminated.

## Maintainability Patterns by Quality Dimension

### Pattern 1: Domain-Named Types (Maintainable + Safe)

Code must express domain concepts, not generic data structures.

**Wrong:**
```rust
fn process(data: HashMap<String, Vec<u8>>) -> Result<HashMap<String, String>, String>
```

**Right:**
```rust
fn process(batch: &MetricBatch) -> Result<AggregatedMetrics, ProcessingError>
```

The type names tell you WHAT the system is doing. The error type tells you WHAT can go wrong. A new engineer (or AI agent) immediately knows the domain.

**This is also safe** — the type system enforces that you cannot pass a `TopologyUpdate` where a `MetricBatch` is expected. The compiler catches the mistake.

### Pattern 2: Named Constants Over Magic Values (Maintainable + Safe + Performant)

Every value that has domain meaning must be a named constant or type.

**Wrong:**
```go
if len(connections) > 10000 {
    evictOldest(connections, 1000)
}
```

**Right:**
```go
const MaxConcurrentConnections = 10_000
const EvictionBatchSize = 1_000

if len(connections) > MaxConcurrentConnections {
    evictOldest(connections, EvictionBatchSize)
}
```

The constants document intent. They're searchable. They're testable. And they compile to the same code — zero performance cost.

### Pattern 3: Exhaustive State Machines (Maintainable + Safe)

Complex state must be modeled as enums (Rust), discriminated unions (TypeScript), or typed interfaces (Go) — not as string comparisons or integer flags.

**Wrong:**
```typescript
if (status === 'connecting' || status === 'reconnecting') { ... }
```

**Right:**
```typescript
type ConnectionState =
  | { kind: 'idle' }
  | { kind: 'connecting'; attempt: number }
  | { kind: 'connected'; session: Session }
  | { kind: 'reconnecting'; lastError: Error; attempt: number }
  | { kind: 'failed'; error: Error };

switch (connection.kind) {
  case 'idle': /* ... */ break;
  case 'connecting': /* ... */ break;
  case 'connected': /* ... */ break;
  case 'reconnecting': /* ... */ break;
  case 'failed': /* ... */ break;
  default: const _exhaustive: never = connection;
}
```

Every state is explicit. Adding a new state forces you to handle it everywhere. The compiler prevents you from forgetting. A new engineer sees every possible state in one place.

### Pattern 4: Data-Oriented Design (Maintainable + Performant + Memory-Efficient)

Structure data for how it's accessed, not for how the domain is modeled in the abstract.

**Wrong (object-oriented, cache-hostile):**
```rust
struct Agent {
    id: String,
    metrics: Metrics,      // heap-allocated
    connections: Vec<Connection>, // heap-allocated, scattered
}
```

**Right (data-oriented, cache-friendly):**
```rust
struct AgentStore {
    ids: Vec<AgentId>,           // contiguous
    cpu_usage: Vec<f32>,         // contiguous, SIMD-friendly
    memory_usage: Vec<u64>,      // contiguous
    connection_counts: Vec<u32>, // contiguous
}
```

Columnar layout means iterating over one field touches contiguous memory. Cache lines are utilized. The compiler can vectorize. AND the structure is clearly named — every field has a domain-meaningful name. It's both faster AND more readable to someone who understands data-oriented design.

### Pattern 5: Explicit Dependencies (Maintainable + Safe)

Dependencies must be visible at the function boundary, not hidden inside.

**Wrong:**
```go
func ProcessMetrics(batch *pb.MetricBatch) error {
    db := getGlobalDB()           // hidden dependency
    cache := getGlobalCache()     // hidden dependency
    logger := getGlobalLogger()   // hidden dependency
    tenant := getTenantFromContext() // hidden behavior
    // ...
}
```

**Right:**
```go
func ProcessMetrics(
    ctx context.Context,
    batch *pb.MetricBatch,
    store storage.MetricStore,
    cache topology.Cache,
    logger *slog.Logger,
) error {
    tenant := auth.TenantFromContext(ctx)
    // ...
}
```

Every dependency is visible at the call site. You can test it by passing mocks. You can trace data flow by reading the signature. A new engineer knows exactly what this function needs.

### Pattern 6: Small, Focused Modules (Maintainable + Safe)

Each module must have a single responsibility and a clear boundary.

**The Module Contract:**
- A module exposes a public API (functions, types, traits/interfaces)
- A module hides its implementation details
- A module depends only on its direct dependencies' public APIs
- A module's tests verify its public API behavior

**Wrong:** A 2000-line `processing.go` file that handles aggregation, correlation, enrichment, error handling, and metrics emission.

**Right:** Separate packages — `aggregator/`, `correlator/`, `enricher/` — each with a focused responsibility, clear types, and its own tests.

### Pattern 7: Consistent Error Patterns (Maintainable + Safe)

Errors must follow the same pattern everywhere in the codebase.

| Language | Pattern |
|----------|---------|
| Rust | `thiserror` enum with `#[non_exhaustive]`, `From<InnerError>` for `?` propagation |
| Go | `fmt.Errorf("context: %w", err)` wrapping, sentinel errors with `errors.Is`/`errors.As` |
| TypeScript | Discriminated union: `{ ok: true; data: T } \| { ok: false; error: ApiError }` |
| Python | Custom exception hierarchy, `raise ... from e` for chaining |
| C | Return codes with `goto cleanup` for resource management |

When every function uses the same error pattern, a new engineer can predict how errors flow through the system without reading each function.

### Pattern 8: Bounded Complexity (Maintainable + Performant + Safe)

Every function, module, and system boundary must have bounded complexity.

| Boundary | Maximum |
|----------|---------|
| Function length | 50 lines (60 for BPF, per NASA JPL) |
| Function parameters | 5 (use a config struct beyond that) |
| Module public API | 10-15 functions (if more, split the module) |
| Nesting depth | 3 levels (extract inner blocks into functions) |
| Cyclomatic complexity | 10 per function |

These are not style preferences — they are cognitive load limits. A function with 8 levels of nesting is not maintainable because a human cannot hold the control flow in working memory. A module with 50 public functions is not maintainable because a human cannot find the right one.

### Pattern 9: Self-Documenting Structure (Maintainable without Comment Overhead)

The code's structure should make comments unnecessary for the "what." Comments explain the "why."

**Wrong (comment explains what the code does):**
```go
// Increment the counter by one
counter++
```

**Right (code is self-documenting, comment explains why):**
```go
// Retry budget prevents cascading failures when downstream is slow.
// Reset on successful response to allow fresh retries after recovery.
retryBudget.Reset()
```

**Self-documenting techniques:**
- Named types that express domain concepts (not `int`, `string`, `map`)
- Named constants that express business rules (not magic numbers)
- Function names that express intent (`collectProcessMetrics`, not `handleData`)
- Variable names that express domain entities (`tenantConnections`, not `tc`)
- Type signatures that express contracts (inputs, outputs, errors)

### Pattern 10: Progressive Disclosure (Maintainable + Powerful)

A module should reveal its complexity progressively — simple interface for simple use, deeper access for advanced use.

**Wrong:** One giant struct with 30 fields that you must configure.

**Right:**
```go
// Simple: just connect
client, err := cluster.Connect(ctx, "localhost:8080")

// Advanced: configure everything
client, err := cluster.Connect(ctx, "localhost:8080",
    cluster.WithTLS(tlsConfig),
    cluster.WithRetry(cluster.RetryPolicy{MaxRetries: 3}),
    cluster.WithTenant("production"),
    cluster.WithCompression(cluster.Zstd),
)
```

The simple path is simple. The advanced path is explicit. Both use the same underlying function. A new engineer starts with the simple path and discovers advanced options as needed.

## Anti-Maintainability Patterns (Forbidden)

These patterns produce code that works but cannot be safely maintained:

| Pattern | Why It's Unmaintainable | What To Do Instead |
|---------|------------------------|-------------------|
| **Over-abstraction** | 5 layers of indirection for a simple operation | Use direct function calls until repetition proves the need for abstraction |
| **God functions** | 200-line functions that do everything | Split into focused functions with clear names |
| **Implicit coupling** | Module A silently depends on Module B's internal state | Explicit dependency injection at boundaries |
| **Stringly-typed logic** | `if type == "metric"` scattered through code | Enums, discriminated unions, typed interfaces |
| **Copy-paste without extraction** | Same 20 lines in 5 places | Extract to a shared function with a domain-meaningful name |
| **Premature abstraction** | Generic patterns before the second use case exists | Wait for the third use case, then abstract |
| **Hidden invariants** | "This function must be called before that one" | Encode ordering in types or initialization patterns |
| **Clever code** | One-liners that require mental gymnastics | Break into named steps |
| **Inconsistent patterns** | Same problem solved differently in different files | Standardize on one pattern per problem category |
| **Comment-driven documentation** | Explaining bad code with comments instead of rewriting it | Rewrite the code to be self-explanatory |

## The "Would I Want to Debug This at 3 AM?" Test

Before committing any code, apply this test:

> "If this code fails in production at 3 AM, and I have no memory of writing it, can I:
> 1. Find the failure point from the error message?
> 2. Understand what the code was trying to do?
> 3. Identify the boundary between working and broken code?
> 4. Fix it without understanding the entire system?"

If any answer is "no," the code needs restructuring.

## Relationship to Other Rules

- **Parent rule**: `agent-governance.md` (The Paryty Truth Protocol)
- **Companion rule**: `verify-before-assert.md` (verification proves the code works; this rule proves it can be maintained)
- **Companion rule**: `constraint-inviolability.md` (maintainability is a constraint that must not be sacrificed)
- **Complements**: `coding-standards-*.md` (language-specific style and patterns; this rule governs cross-cutting structural quality)
- **Complements**: `architecture.md` (system-level boundaries; this rule governs code-level boundaries)
- **Complements**: `bug-fix-discipline.md` (bug fixes must not introduce maintainability debt)
