# Paryty Coding Standards Rules

## Rust Rules (agent/)

### Mandatory
- No `unwrap()` in library code — use `?` operator or `expect()` with context
- Every `unsafe` block must have `// SAFETY:` comment explaining why it's safe
- All public items must have doc comments
- Use `thiserror` for error types, `anyhow` for application errors
- Use `tracing` for logging, not `println!` or `log` crate

### Forbidden
- `unsafe` without SAFETY comment
- `unwrap()` in library code
- `println!` in production code
- `dbg!()` in production code
- `todo!()` in committed code (use with issue number)

### Required Patterns
```rust
// Error handling
fn process() -> Result<T, MyError> {
    let value = operation().map_err(|e| MyError::Operation(e))?;
    Ok(value)
}

// Logging
tracing::info!(service = "agent", "Metric collected");
tracing::error!(error = %err, "Collection failed");

// Configuration
#[derive(Debug, Deserialize)]
struct Config {
    #[serde(default = "default_interval")]
    interval: Duration,
}
```

## Go Rules (cluster/)

### Mandatory
- No ignored errors — always handle or explicitly ignore with `_ =`
- Use `context.Context` for cancellation and timeouts
- Use `log/slog` for structured logging
- Use `errgroup` for fan-out/fan-in
- Table-driven tests for all unit tests

### Forbidden
- Ignoring errors (use `_ =` if intentionally ignoring)
- `panic()` in library code
- Global mutable state
- Mutex held across I/O operations
- Goroutine leaks (every goroutine must have shutdown path)

### Required Patterns
```go
// Error handling
if err := operation(); err != nil {
    return fmt.Errorf("operation failed: %w", err)
}

// Context propagation
func process(ctx context.Context, input Input) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    // ...
}

// Logging
slog.Info("metric collected",
    "service", "cluster",
    "tenant", tenantID,
)

// Testing
func TestProcess(t *testing.T) {
    tests := []struct {
        name     string
        input    Input
        expected Output
    }{
        // ...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

## TypeScript Rules (frontend/)

### Mandatory
- Strict TypeScript — no `any` type
- React functional components with hooks
- Zustand for global state management
- Proper error boundaries
- Data-testid attributes for E2E testing

### Forbidden
- `any` type (use `unknown` and narrow)
- Class components
- Direct DOM manipulation
- Prop drilling more than 3 levels
- Inline styles for complex styling

### Required Patterns
```typescript
// Type safety
interface Metric {
  id: string;
  value: number;
  timestamp: Date;
}

// React components
const Component: React.FC<Props> = ({ prop1, prop2 }) => {
  const [state, setState] = useState<Type>(initial);
  
  const handler = useCallback(() => {
    // ...
  }, [dependencies]);

  return <div data-testid="component">{/* ... */}</div>;
};

// Zustand store
const useStore = create<State>((set) => ({
  items: [],
  addItem: (item) => set((s) => ({ items: [...s.items, item] })),
}));
```

## Cross-Language Rules

### Naming
- Use descriptive names over abbreviations
- Prefix booleans with `is`, `has`, `should`
- Use past tense for events (`created`, `deleted`)
- Constants in SCREAMING_SNAKE_CASE

### Documentation
- Document "why" not "what"
- Keep comments up to date
- Use TODO with issue number: `// TODO(paryty#123): ...`
- Remove commented-out code before merge

### Git
- Conventional commits: `feat:`, `fix:`, `docs:`, `chore:`, `test:`
- Subject line under 72 characters
- Reference issue numbers
- One logical change per commit
