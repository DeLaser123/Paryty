# Code Standards — Paryty Coding Standards

## Purpose
Enforce consistent coding standards across all Paryty components. Covers Rust, Go, and TypeScript conventions.

## Rust Standards

### Formatting
- Use `cargo fmt` with default settings
- Max line width: 100 characters
- Use 4 spaces for indentation

### Naming
- `snake_case` for variables, functions, modules
- `PascalCase` for types, traits, enums
- `SCREAMING_SNAKE_CASE` for constants
- Prefix `_` for unused variables

### Error Handling
```rust
// Good: Propagate errors with context
fn process_metric(raw: &[u8]) -> Result<Metric, ProcessingError> {
    let parsed = parse(raw).map_err(|e| ProcessingError::Parse(e))?;
    validate(parsed).map_err(|e| ProcessingError::Validation(e))?;
    Ok(parsed)
}

// Bad: Unwrap in library code
fn process_metric(raw: &[u8]) -> Metric {
    let parsed = parse(raw).unwrap();  // NEVER in library code
    parsed
}
```

### Unsafe Code
```rust
// Every unsafe block MUST have a SAFETY comment
unsafe {
    // SAFETY: ptr is guaranteed to be valid for reads of len bytes
    // because it was allocated by alloc() and not yet freed
    std::ptr::copy_nonoverlapping(ptr, dst, len);
}
```

### Concurrency
```rust
// Good: Use channels for communication
let (tx, rx) = tokio::sync::mpsc::channel(100);

// Good: Use Arc<Mutex<T>> for shared state
let state = Arc::new(Mutex::new(HashMap::new()));

// Bad: Raw shared mutable state
static mut COUNTER: u64 = 0;  // NEVER
```

### Documentation
```rust
/// Processes a raw metric byte slice into a structured Metric.
///
/// # Arguments
/// * `raw` - Raw bytes from the agent
///
/// # Returns
/// The parsed and validated Metric
///
/// # Errors
/// Returns ProcessingError if parsing or validation fails
fn process_metric(raw: &[u8]) -> Result<Metric, ProcessingError> {
    // ...
}
```

## Go Standards

### Formatting
- Use `gofmt` with default settings
- Use `golangci-lint` with project config
- Max line width: 120 characters

### Naming
- `camelCase` for unexported identifiers
- `PascalCase` for exported identifiers
- Short variable names in small scopes
- Descriptive names in larger scopes

### Error Handling
```go
// Good: Wrap errors with context
if err := processMetric(raw); err != nil {
    return fmt.Errorf("processing metric: %w", err)
}

// Bad: Ignore errors
processMetric(raw)  // NEVER ignore errors
```

### Concurrency
```go
// Good: Use context for cancellation
func process(ctx context.Context, ch <-chan Metric) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case m, ok := <-ch:
            if !ok {
                return nil
            }
            handle(m)
        }
    }
}

// Good: Use errgroup for fan-out
g, ctx := errgroup.WithContext(ctx)
for _, shard := range shards {
    shard := shard
    g.Go(func() error {
        return processShard(ctx, shard)
    })
}
return g.Wait()
```

### Testing
```go
// Good: Table-driven tests
func TestAggregation(t *testing.T) {
    tests := []struct {
        name     string
        input    []Metric
        expected AggregatedMetric
    }{
        {"empty", nil, AggregatedMetric{}},
        {"single", []Metric{{V: 1}}, AggregatedMetric{Avg: 1}},
        {"multiple", []Metric{{V: 1}, {V: 3}}, AggregatedMetric{Avg: 2}},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := Aggregate(tt.input)
            if got != tt.expected {
                t.Errorf("Aggregate() = %v, want %v", got, tt.expected)
            }
        })
    }
}
```

## TypeScript Standards

### Formatting
- Use Prettier with project config
- Use ESLint with project config
- Max line width: 100 characters

### Naming
- `camelCase` for variables, functions
- `PascalCase` for components, types, interfaces
- `SCREAMING_SNAKE_CASE` for constants
- Prefix `I` for interfaces (optional)

### React Patterns
```typescript
// Good: Functional components with hooks
const TopologyView: React.FC<TopologyViewProps> = ({ nodes, edges }) => {
  const [selected, setSelected] = useState<string | null>(null);
  
  const handleSelect = useCallback((id: string) => {
    setSelected(id);
  }, []);

  return (
    <div data-testid="topology-canvas">
      {nodes.map(node => (
        <Node key={node.id} node={node} onSelect={handleSelect} />
      ))}
    </div>
  );
};
```

### State Management
```typescript
// Good: Zustand store
const useTopologyStore = create<TopologyState>((set) => ({
  nodes: [],
  edges: [],
  addNode: (node) => set((state) => ({ nodes: [...state.nodes, node] })),
  removeNode: (id) => set((state) => ({
    nodes: state.nodes.filter(n => n.id !== id),
  })),
}));

// Bad: Prop drilling through 5+ levels
```

### Type Safety
```typescript
// Good: Strict types
interface Metric {
  id: string;
  value: number;
  timestamp: Date;
  labels: Record<string, string>;
}

// Bad: Using any
const metric: any = getValue();  // NEVER use any
```

## Cross-Language Standards

### Naming Conventions
- Use descriptive names over abbreviations
- Use domain terminology consistently
- Prefix booleans with is/has/should
- Use past tense for events (created, deleted)

### Documentation
- Document "why" not "what"
- Keep comments up to date
- Use TODO with ticket number
- Remove commented-out code

### Git Commits
- Use conventional commits: `feat:`, `fix:`, `docs:`, `chore:`
- Keep subject line under 72 characters
- Reference issue numbers
- One logical change per commit

### Code Organization
- Group related functions together
- Keep files under 500 lines
- Keep functions under 50 lines
- Extract complex logic into named functions

## Notes
- These standards are enforced by linters and CI
- Exceptions require explicit justification in PR
- Standards evolve through team discussion
- When in doubt, follow the existing codebase patterns
