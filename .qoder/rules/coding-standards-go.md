# Go Coding Bible — Memory Safety & Extreme Performance

Sources: Google Go Style Guide, Uber Go Style Guide, "100 Go Mistakes" (Harsanyi), "Concurrency in Go" (Cox-Buday), "The Go Programming Language" (Donovan/Kernighan), Netflix production practices, Cloudflare Go performance patterns.

## I. Memory Safety & Resource Management

### Goroutine Lifecycle (Netflix: "The Silent Production Killer")
1. **Every goroutine must have a clear shutdown path.** Use `errgroup.Group` to supervise all goroutines.
2. **Never fire-and-forget goroutines.** Always track them with `errgroup` or `sync.WaitGroup`.
3. **Context cancellation is the shutdown signal.** All goroutines select on `ctx.Done()`.
4. **Bounded channel capacity.** Always specify buffer size. Unbounded channels leak memory under load.
5. **Goroutine stack is ~2KB minimum.** 10K leaked goroutines = 20MB leaked memory.
6. **Use `goleak` in tests** (`go.uber.org/goleak`) to detect goroutine leaks.
7. **Never launch goroutines in `init()`.** They cannot be shut down cleanly.

### Mutex & Synchronization (Uber Style Guide)
8. **Zero-value mutex is valid.** `var mu sync.Mutex` — never `new(sync.Mutex)`.
9. **Never embed mutex in exported structs.** Use a named field: `mu sync.Mutex`.
10. **Never hold a mutex across I/O operations.** Lock, compute, unlock — no network calls while holding.
11. **Use `sync.RWMutex` when reads vastly outnumber writes.** But benchmark first — `RWMutex` is larger and slower for write-heavy workloads.
12. **`sync.Once` for lazy initialization.** Never use `init()` for complex setup.
13. **`sync.Pool` for frequently allocated objects** — reduces GC pressure in hot paths.

### Pointer & Interface Safety
14. **Verify interface compliance at compile time:** `var _ http.Handler = (*Handler)(nil)`
15. **Never use pointers to interfaces.** Interfaces already contain a pointer internally.
16. **Handle type assertion failures:** Always use the comma-ok form: `v, ok := x.(T)`.
17. **nil is a valid slice.** Don't check `len(s) == 0` when `s == nil` suffices — or just use `len(s) == 0` for both.

## II. Error Handling (Google Style Guide)

18. **Never ignore errors.** Handle or explicitly ignore with `_ =` and a comment explaining why.
19. **Wrap errors with context:** `fmt.Errorf("processing metric for tenant %s: %w", tenantID, err)`
20. **Use `%w` for error wrapping** (not `%v` or `%s`) to preserve the error chain.
21. **Handle errors once.** Either handle the error or return it — never both.
22. **Custom error types with `errors.Is` and `errors.As`** for type-safe error checking.
23. **Never `panic()` in library code.** Only in `main()` for unrecoverable startup failures.
24. **Error naming:** `ErrNotFound`, `ErrTimeout` — exported, PascalCase, prefixed with `Err`.

## III. Concurrency Patterns

### Context Propagation (Google Style Guide)
25. **`context.Context` is the first parameter of every function.** `func process(ctx context.Context, ...)`.
26. **Pass context explicitly.** Never store it in a struct or global.
27. **Use `context.WithTimeout` for all external calls.** No operation should run without a deadline.
28. **Check `ctx.Done()` in loops:** `select { case <-ctx.Done(): return ctx.Err(); default: }`.

### Fan-Out / Fan-In (Uber)
29. **`errgroup.WithContext` for parallel work with shared cancellation.**
30. **Pre-allocate result slices** when the number of workers is known.
31. **Collect errors from all goroutines** — don't lose failures.

### Channel Patterns
32. **Channel size is one or none.** Buffered channels should have a clear reason for their size.
33. **Close channels from the sender side only.** Receivers detect closure via the comma-ok form.
34. **Use `select` with `default` for non-blocking sends/receives.**

## IV. Performance (Uber + Cloudflare)

35. **Prefer `strconv` over `fmt.Sprintf` for conversions.** `strconv.Itoa` is 10x faster.
36. **Avoid repeated `string` ↔ `[]byte` conversions.** Convert once, reuse.
37. **Pre-allocate slices with `make([]T, 0, n)`** when capacity is known.
38. **Pre-allocate maps with `make(map[K]V, n)`** to avoid rehashing.
39. **`sync.Pool` for hot-path objects** — but reset objects before returning to pool.
40. **Escape analysis:** `go build -gcflags="-m"` — no unexpected heap allocations in hot paths.
41. **Batch operations** where possible (database writes, Redpanda produces, Dragonfly pipelines).
42. **Use `strings.Builder` for string concatenation** — not `+` operator in loops.

## V. Logging (Google)

43. **`log/slog` for structured logging.** Never `fmt.Println`, `log.Printf`, or `fmt.Sprintf` for logs.
44. **Structured fields:** `slog.Info("metric collected", "service", "cluster", "tenant", tenantID)`
45. **Log levels:** ERROR (action required), WARN (attention needed), INFO (business events), DEBUG (dev only).

## VI. Testing (Google + Uber)

46. **Table-driven tests for all unit tests.** Name each case, use `t.Run` for sub-tests.
47. **`testify/assert` or `testify/require` for assertions** — clearer failure messages than `if got != want`.
48. **`testify/mock` for mocking** — verify call counts, arguments, and return values.
49. **Integration tests behind build tag:** `//go:build integration`.
50. **Race detector is mandatory:** `go test -race -count=1 ./...` — zero tolerance for races.
51. **Coverage target: 80% for business logic, 70% for infrastructure, 90% for critical paths.**

## VII. Package Design (Google)

52. **Package names: lowercase, single word, no underscores.** `processing` not `data_processing`.
53. **Avoid `init()` functions.** They run in undefined order and can't be tested.
54. **Avoid global mutable state.** Use dependency injection via constructors.
55. **Interfaces at the consumer, not the producer.** Define interfaces where they're used.
56. **Small interfaces: 1-3 methods.** `io.Reader`, `io.Writer`, `fmt.Stringer` — the Go way.

## VIII. Forbidden Patterns

57. **No `panic()` in library code.**
58. **No ignored errors** without `_ =` and explanatory comment.
59. **No goroutine leaks** — every goroutine supervised.
60. **No unbounded channels** — always specify capacity.
61. **No `init()` for complex initialization** — use constructors.
62. **No embedding types in public structs** — explicit composition.
63. **No using built-in names** (`new`, `make`, `len`, `cap`) as variable names.
64. **No fire-and-forget goroutines.**
65. **No mutex held across I/O.**

## IX. Data Lifecycle

66. **Idempotent operations.** Same input → same output. Always.
67. **Bounded state.** Max items per window, per cache, per buffer. Evict when full.
68. **Dead letter queue for failures.** Never silently drop messages.
69. **Tenant isolation at every layer.** Separate processing, separate storage.
70. **Graceful shutdown:** SIGTERM → drain in-flight → flush buffers → exit.
