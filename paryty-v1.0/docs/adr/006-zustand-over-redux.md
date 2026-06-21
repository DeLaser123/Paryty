# ADR-006: Zustand over Redux for State Management

## Status: Accepted

## Date: 2025-01-15

## Context

Paryty's frontend needs global state management for topology data, selected nodes, time ranges, user preferences, and real-time WebSocket updates. Requirements:
- Minimal boilerplate
- Excellent TypeScript support
- Selector-based subscriptions (minimal re-renders)
- No provider wrapper required

## Decision

**Zustand** over Redux Toolkit and MobX.

## Rationale

| Criterion | Zustand | Redux Toolkit | MobX | Jotai |
|-----------|---------|---------------|------|-------|
| Bundle size | ~1 KB | ~11 KB | ~16 KB | ~3 KB |
| Boilerplate | Minimal | Moderate | Minimal | Minimal |
| TypeScript support | Excellent | Excellent | Good | Excellent |
| Provider required | No | Yes | No | No |
| DevTools | Yes (via middleware) | Yes (excellent) | Yes | Yes |
| Learning curve | Very low | Medium | Low | Low |
| Selector subscriptions | Yes | Yes | Automatic | Atomic |
| Immer integration | Yes (middleware) | Yes (built-in) | N/A | N/A |

**Key factors:**
1. **Minimal boilerplate** — `create((set) => ({ count: 0, inc: () => set(s => ({ count: s.count + 1 })) }))` — that's the entire store.
2. **No provider** — Unlike Redux, no `<Provider>` wrapper needed. Stores are standalone.
3. **1 KB bundle** — Critical for a visualization-heavy frontend where every KB matters.
4. **Selector subscriptions** — `useStore(s => s.nodes)` only re-renders when `nodes` changes.

## Consequences

- **Less ecosystem** than Redux — Mitigated by Zustand's simplicity (less need for middleware).
- **No time-travel debugging** — Available via `zustand/middleware/devtools` but less polished than Redux DevTools.
- **Separate stores** — Topology store, metrics store, UI store. Clean separation of concerns.

## Alternatives Considered

1. **Redux Toolkit** — Rejected: More boilerplate, provider wrapper, larger bundle. Overkill for Paryty's state needs.
2. **MobX** — Rejected: "Magic" reactivity can be confusing, larger bundle, harder to debug.
3. **Jotai** — Rejected: Atomic model is different from Paryty's centralized topology state pattern.
