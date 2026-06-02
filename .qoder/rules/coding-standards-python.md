# Python Coding Bible — Memory Safety & Extreme Performance

Sources: Google Python Style Guide, "Fluent Python" (Ramalho), Netflix ML Engineering, "Python Cookbook" (Beazley/Jones), scikit-learn/PyTorch production patterns.

## I. Type Safety & Correctness

### Type Hints (Mandatory)
1. **Type hints on all function signatures.** `def process(metrics: list[Metric]) -> AggregatedResult:`
2. **Use `mypy --strict` in CI.** No `Any` without explicit `# type: ignore[no-any]` justification.
3. **`typing.Protocol` for structural subtyping** — duck typing with static checks.
4. **`typing.TypeVar` with bounds** for generic functions: `T = TypeVar('T', bound=HasId)`.
5. **`dataclasses` or `pydantic` for structured data** — not raw dicts.
6. **`Enum` for finite sets of values** — not string literals scattered through code.
7. **`Optional[X]` means "may be None"**, not "optional parameter." Use `X | None` in Python 3.10+.

### Input Validation
8. **Validate all external data at the boundary.** gRPC request, file read, environment variable.
9. **`pydantic` for request/response validation** in gRPC services — auto-validates, auto-documents.
10. **Never trust data from files or databases.** Validate schema on read.

## II. Memory Management

### Avoiding Memory Leaks
11. **Never hold references longer than needed.** Delete large DataFrames after use: `del df`.
12. **Use generators for large sequences.** `yield` instead of building full lists in memory.
13. **`collections.deque` with `maxlen`** for bounded buffers (rolling windows).
14. **`weakref.WeakValueDictionary` for caches** — auto-evicts unreachable entries.
15. **Close files and connections explicitly.** Use `with` statements — never leave open handles.
16. **Profile memory with `tracemalloc` or `memory_profiler`** before production deployment.

### DataFrame / NumPy Efficiency
17. **Vectorized operations over loops.** `df['col'] * 2` not `for x in df['col']`.
18. **Specify dtypes explicitly.** `pd.DataFrame(data, dtype='float32')` — halves memory vs float64.
19. **Use categorical types for low-cardinality columns.** `df['status'] = df['status'].astype('category')`.
20. **`parquet` for serialization** — 10x smaller than CSV, preserves dtypes, supports column pruning.

## III. Performance

### Hot Path Optimization
21. **`numpy` for numerical computation** — never pure Python loops for math.
22. **`numba` JIT for hot loops** that can't be vectorized — `@njit` decorator.
23. **`multiprocessing` for CPU-bound work** (GIL bypass). `threading` only for I/O-bound.
24. **`asyncio` for I/O-bound services** — gRPC async, concurrent HTTP requests.
25. **Batch predictions.** ML model inference: batch N inputs, not 1-at-a-time.
26. **Cache model artifacts.** Load once at startup, reuse across requests. Never load per-request.
27. **`lru_cache` for pure functions** with repeated inputs. Bounded to prevent unbounded growth.

### Profiling
28. **`cProfile` for CPU profiling** — identify hot functions.
29. **`line_profiler` for line-by-line timing** — pinpoint slow lines in hot functions.
30. **`py-spy` for production profiling** — no code changes needed, sampling profiler.

## IV. Concurrency & gRPC Services

31. **`grpc.aio` for async gRPC servers** — handles concurrent requests without thread pool exhaustion.
32. **Thread pool for CPU-bound model inference** — `concurrent.futures.ProcessPoolExecutor`.
33. **Semaphore for resource-limited operations** — max N concurrent model predictions.
34. **Graceful shutdown:** Handle SIGTERM, drain in-flight requests, flush model state.
35. **Health check endpoint** — return model version, last prediction time, memory usage.

## V. ML Model Management

36. **File-based model persistence for V1.0.** `joblib.dump(model, path)` / `joblib.load(path)`.
37. **Model versioning:** `{model_name}_v{version}.joblib` — never overwrite.
38. **Track model metrics:** MAPE, RMSE, R² for each model version. Log on every training run.
39. **A/B test new models** — run alongside production model, compare metrics, promote winner.
40. **Drift detection:** Monitor prediction distribution. Alert when distribution shifts >2σ from training data.

## VI. Error Handling

41. **Custom exception hierarchy.** `class ModelError(Exception): ...`, `class DataError(Exception): ...`.
42. **Never bare `except:`.** Always specify: `except ValueError as e:`.
43. **Log exceptions with full traceback.** `logger.exception("prediction failed")` — includes stack trace.
44. **Fail fast on configuration errors.** Validate all config at startup, not on first use.

## VII. Logging & Observability

45. **`structlog` or `logging` with JSON formatter** — structured logs for ingestion pipeline.
46. **Log model prediction latency.** `logger.info("prediction", model=version, latency_ms=elapsed)`.
47. **OpenTelemetry for tracing** — span per prediction request, attributes for model version and input shape.

## VIII. Testing

48. **`pytest` for all tests.** Not `unittest`. Fixtures for setup, parametrize for coverage.
49. **`pytest-cov` for coverage** — minimum 80% for business logic.
50. **`hypothesis` for property-based testing** — finds edge cases in numerical code.
51. **Test model accuracy on known datasets.** Backtesting with historical data.
52. **Test prediction latency.** Assert <100ms for single prediction, <1s for batch.

## IX. Code Style (Google Python Style Guide)

53. **`ruff` for linting and formatting** — 10-100x faster than flake8+black+isort.
54. **Google-style docstrings** for all public functions:
    ```python
    def predict(metrics: np.ndarray) -> Forecast:
        """Generate forecast from input metrics.

        Args:
            metrics: Array of shape (n_samples, n_features).

        Returns:
            Forecast with point predictions and confidence intervals.

        Raises:
            ValueError: If input shape is invalid.
        """
    ```
55. **snake_case for functions/variables, PascalCase for classes.**
56. **Constants in SCREAMING_SNAKE_CASE:** `MAX_PREDICTIONS = 1000`.

## X. Forbidden Patterns

57. **No bare `except:`.** Always specify exception type.
58. **No `import *`.** Explicit imports only.
59. **No mutable default arguments.** `def f(x=[])` is a bug. Use `def f(x=None)`.
60. **No global mutable state.** Use class instances or function closures.
61. **No `print()` in production code.** Use `logger.info()`.
62. **No `pickle` for untrusted data.** Use `json` or `pydantic` for external data.
63. **No blocking I/O in async functions.** Use `asyncio.to_thread()` for blocking calls.
64. **No `Any` type without justification comment.**
