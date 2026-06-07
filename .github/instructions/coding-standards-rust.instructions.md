---
description: "Rust coding standards. Use when writing Rust code."
applyTo: "**/*.rs"
---

# Rust Coding Bible — Memory Safety & Extreme Performance

Sources: Rust API Guidelines (official), The Rustonomicon, "Rust Atomics and Locks" (Mara Bos), "The Rust Performance Book" (Nicholas Cameron), Cloudflare Pingora, Discord Rust practices, "Programming Rust" (Blandy/Orendorff).

## I. Memory Safety & Ownership (Non-Negotiable)

### Ownership & Borrowing
1. **Prefer borrowing over ownership.** Take `&T` or `&mut T` unless the function genuinely needs to own the data.
2. **Prefer `&str` over `String` in function parameters.** Callers don't need to allocate.
3. **Prefer `&[T]` over `Vec<T>` in function parameters.** Accept any contiguous slice.
4. **Use `Cow<'_, str>` when a function may or may not allocate.** Avoids unnecessary clones.
5. **Never clone to satisfy the borrow checker.** Restructure code instead. Clone is a last resort.
6. **`Drop` impls must be infallible.** Never panic in `Drop`. Use `Drop` for deterministic resource cleanup (file handles, sockets, map entries).

### Unsafe Code (Rustonomicon)
7. **Every `unsafe` block MUST have a `// SAFETY:` comment** documenting: What invariant is upheld, Why it cannot be violated, What happens if it IS violated.
8. **Minimize `unsafe` surface area.** Wrap unsafe operations in safe abstractions. The safe API must uphold all invariants.
9. **Never trust user input in `unsafe` blocks.** Validate all inputs before crossing the unsafe boundary.
10. **`unsafe impl Send` / `Sync` requires proof.** Document why the type is safe to send/share across threads.
11. **Prefer `MaybeUninit<T>` over `mem::zeroed()` for uninitialized memory.** `zeroed()` is UB for types where zero is not a valid bit pattern.

### Smart Pointers
12. **`Box<T>` for single ownership of heap data.** Recursive types, trait objects, large stack items.
13. **`Rc<T>` / `Arc<T>` for shared ownership.** `Rc` for single-threaded, `Arc` for multi-threaded. Never `Rc` in async code.
14. **`Arc<Mutex<T>>` is a code smell.** Consider restructuring to avoid shared mutable state. Prefer message passing (channels) or lock-free structures.
15. **`Pin<Box<T>>` for self-referential types and Futures.** Never move a pinned value.

## II. Async Runtime (Tokio — Paryty Standard)

16. **`tokio::main` with `multi_thread` runtime** for services. `current_thread` only for tests.
17. **Never `block_on` inside an async context.** Use `tokio::task::spawn_blocking` for blocking operations.
18. **`tokio::select!` for concurrent operations** with cancellation — prefer over spawning tasks for simple fan-out.
19. **All async functions accept `CancellationToken` or check `ctx` for cooperative cancellation.**
20. **Bounded concurrency with `tokio::sync::Semaphore`.** Never spawn unbounded tasks — use a semaphore or task pool.
21. **`tokio::sync::mpsc` with bounded capacity** for inter-task communication. Always specify buffer size.
22. **Use `tokio::time::timeout` on all external I/O.** No operation runs without a deadline.
23. **Never hold a `MutexGuard` across `.await`.** Use `tokio::sync::Mutex` if you must, but prefer restructuring.

## III. Error Handling (Rust API Guidelines)

24. **`thiserror` for library error types.** Derive `Error`, `Display`, `From` for ergonomic error conversion.
25. **`anyhow` for application-level error handling** (main binary, CLI). Not in library crates.
26. **Never `unwrap()` in production code.** Use `?` operator, `match`, or `if let`. `unwrap()` is for tests and provably-safe code only.
27. **Never `expect()` without a message explaining why it cannot fail.** `expect("config is validated at startup")`.
28. **Custom error types with `#[non_exhaustive]`** — allows adding variants without breaking changes.
29. **Error enum variants named for what went wrong:** `InvalidConfig`, `ConnectionLost`, `Timeout`.
30. **Implement `From<InnerError>` for outer error types** — enables `?` operator across module boundaries.

## IV. Performance (The Rust Performance Book + Cloudflare)

### Allocation Avoidance
31. **Pre-allocate `Vec` with `Vec::with_capacity(n)`** when size is known or estimable.
32. **Reuse allocations across iterations.** Clear and reuse `Vec`, `String`, `HashMap` in loops.
33. **`smallvec::SmallVec` for small collections** — avoids heap allocation for common small sizes.
34. **Avoid `Box<dyn Trait>` in hot paths** — vtable dispatch prevents inlining. Use generics or enums.
35. **`&str` over `String`, `&[T]` over `Vec<T>`** — avoids allocation at API boundaries.

### Zero-Cost Abstractions
36. **Use iterators with `.collect::<Vec<_>>()`** — the compiler optimizes iterator chains to equivalent hand-written loops.
37. **`#[inline]` judiciously** — for small, frequently-called functions in hot paths. Profile before inlining.
38. **Prefer `enum` over `dyn Trait`** — static dispatch, better cache locality, exhaustive matching.
39. **Use `const fn` for compile-time computation** — configuration parsing, lookup tables.

### Concurrency Performance
40. **`parking_lot::Mutex` over `std::sync::Mutex`** — faster, no poisoning, smaller memory footprint.
41. **`dashmap::DashMap` for concurrent HashMap** — sharded locks, minimal contention.
42. **`crossbeam` channels over `std::sync::mpsc`** — multi-producer, bounded variants, better performance.
43. **Lock-free data structures (`arc_swap`, `atomic_cell`) for read-heavy workloads.**
44. **`rayon` for data parallelism** — `.par_iter()` for CPU-bound batch processing.

### Profiling
45. **`cargo bench` + Criterion.rs for benchmarks.** Never optimize without measurements.
46. **`perf` + `flamegraph` crate for CPU profiling** — identify hot functions.
47. **`heaptrack` or `dhat` for heap profiling** — find allocation hotspots.
48. **`cargo build --release` with LTO** (`lto = true` in Cargo.toml) for production builds.

## V. Logging & Observability

49. **`tracing` crate for structured logging.** Not `log`, not `env_logger`. `tracing` supports async spans.
50. **`tracing::instrument` on all public functions** in service boundaries (gRPC handlers, pipeline stages).
51. **Structured fields:** `tracing::info!(tenant_id = %id, metrics_count = count, "batch processed")`
52. **Log levels:** ERROR (action required), WARN (attention needed), INFO (business events), DEBUG (dev only), TRACE (extreme detail).
53. **Never log secrets, tokens, or PII.** Sanitize all logged data.

## VI. Testing

54. **`#[cfg(test)] mod tests` in every module.** Unit tests co-located with source.
55. **`#[tokio::test]` for async tests** — uses Tokio runtime.
56. **Property-based testing with `proptest`** — generates random inputs, finds edge cases.
57. **Integration tests in `tests/` directory** — test public API, not internals.
58. **`cargo test -- --test-threads=1`** when tests share resources (database, file system).
59. **`#[should_panic]` for testing panic conditions** — verify error paths.
60. **Coverage: 80% for business logic, 90% for critical paths** (eBPF data handling, gRPC serialization).

## VII. Forbidden Patterns

61. **No `unwrap()` in production code.** Use `?` or `match`.
62. **No `clone()` to satisfy the borrow checker.** Restructure first.
63. **No `unsafe` without `// SAFETY:` comment.**
64. **No `block_on` inside async context.** Use `spawn_blocking`.
65. **No unbounded channels or task spawning.** Always bound concurrency.
66. **No `Box<dyn Trait>` in hot paths.** Use enums or generics.
67. **No `panic!()` in library code.** Return `Result`.
68. **No holding `MutexGuard` across `.await`.**

## VIII. Configuration & Build

69. **`serde` with `#[serde(deny_unknown_fields)]`** for config parsing — catches typos in YAML/JSON.
70. **Validate all configuration at startup.** Fail fast with clear error messages before accepting traffic.
71. **`build.rs` for code generation** (protobuf, build metadata). Not for complex build logic.
72. **Workspace-level `Cargo.toml` with shared dependency versions.** Prevent version drift across crates.
73. **`cargo deny` for license and dependency auditing** in CI.
74. **`cargo clippy -- -D warnings` in CI.** Zero clippy warnings tolerated.

