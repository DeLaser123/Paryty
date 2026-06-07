---
description: "TypeScript coding standards. Use when writing TypeScript/React code."
applyTo: "**/*.{ts,tsx}"
---

# TypeScript Coding Bible — Memory Safety & Extreme Performance

Sources: Google TypeScript Style Guide, "TypeScript Deep Dive" (Basarat), React Performance Patterns, PixiJS Performance Guide, Netflix UI Engineering, "React+TypeScript Cheatsheet".

## I. Type Safety (Non-Negotiable)

### Strict Mode Enforcement
1. **`strict: true` in tsconfig.json.** No exceptions. This enables all strict checks.
2. **No `any` type.** Use `unknown` and narrow with type guards. `any` disables the type system.
3. **No `as` type assertions without validation.** Use runtime checks or discriminated unions instead.
4. **No non-null assertion (`!`) on external data.** Validate data from APIs, user input, and storage.
5. **Use discriminated unions for state:**
   ```typescript
   type State =
     | { status: 'loading' }
     | { status: 'success'; data: Metric[] }
     | { status: 'error'; message: string };
   ```
6. **Exhaustive switch checks:** `const _exhaustive: never = state;` in default case catches unhandled variants.
7. **Use `readonly` for immutable properties** — prevents accidental mutation.
8. **Use `as const` for literal types** — `const COLORS = ['red', 'green', 'blue'] as const`.

### Interface & Type Design
9. **`interface` for object shapes, `type` for unions and intersections.**
10. **Mark optional properties explicitly.** `{ name: string; label?: string }` — document optionality.
11. **Use `Record<K, V>` over index signatures** — more type-safe: `Record<string, Metric>`.
12. **Generic constraints:** `<T extends HasId>` — constrain generics to required shape.

## II. React Performance (Netflix UI Engineering)

### Rendering Optimization
13. **`React.memo` for pure components** that receive the same props frequently.
14. **`useMemo` for expensive computations** — but only when profiling shows benefit. Don't memoize everything.
15. **`useCallback` for event handlers passed to child components** — prevents unnecessary re-renders.
16. **Stable keys for lists.** Use `id` from data, not array index. Index keys break on reorder.
17. **Virtualize long lists** (react-window/react-virtuoso) — never render 10K DOM nodes.
18. **Avoid inline object/array creation in JSX.** `<Child style={{ color: 'red' }} />` creates new object every render.

### State Management (Zustand)
19. **Zustand for global state.** No Redux, no MobX, no Context for frequently-changing data.
20. **Selector-based subscriptions:** `const nodes = useStore((s) => s.nodes)` — re-renders only when `nodes` changes.
21. **Immer middleware for immutable updates:** `set((s) => { s.nodes.push(node) })` — readable mutations, immutable under the hood.
22. **Separate stores for separate concerns.** Topology store, metrics store, UI store — not one monolithic store.
23. **Never store derived state.** Compute it from base state in selectors or `useMemo`.

### Component Design
24. **Functional components with hooks only.** No class components.
25. **Custom hooks for reusable logic.** `useTopology()`, `useMetrics()`, `useWebSocket()`.
26. **`data-testid` on all interactive elements** for E2E testing.
27. **Error boundaries around visualization components** — WebGL failure shouldn't crash the app.
28. **No prop drilling >3 levels.** Use Zustand or composition patterns.

## III. PixiJS GPU Performance (Graphics Programming)

### Rendering Pipeline
29. **Instanced rendering for 10K+ nodes.** `PIXI.ParticleContainer` for batches of similar sprites.
30. **Object pooling for particles.** Pre-allocate 50K particles. Never `new` in render loop.
31. **Texture atlas for sprites** — one texture bind for many sprites. Reduces draw calls.
32. **`autoResize` on renderer.** Handle DPI changes and window resize.
33. **`preserveDrawingBuffer: false`** (default) — better performance. Only set `true` for screenshots.

### Memory Management
34. **GPU memory budget: 512MB.** Monitor with `renderer.textureGC` and manual tracking.
35. **Dispose textures when no longer needed.** `texture.destroy(true)` — frees GPU memory.
36. **Dispose graphics objects.** `graphics.destroy()` — prevents memory leaks on component unmount.
37. **Handle `webglcontextlost` / `webglcontextrestored`.** Rebuild all GPU resources on context loss.
38. **No allocations in render loop.** Pre-compute all data outside the `requestAnimationFrame` callback.

### Frame Budget (60fps)
39. **16.67ms per frame budget.** Profile with Chrome DevTools Performance tab.
40. **Degrade gracefully at <30fps.** Reduce particle count, simplify glow effects, skip non-essential animations.
41. **Barnes-Hut layout: O(n log n)** for force-directed graph. Never O(n^2).
42. **Frustum culling.** Don't render objects outside the visible viewport.
43. **Level of Detail (LOD) by zoom level.** Full detail at close zoom, simplified at far zoom.
44. **`requestAnimationFrame` only.** Never `setInterval` or `setTimeout` for rendering.

## IV. TypeScript Performance

45. **Avoid `any` for performance too** — TypeScript can't optimize `any`-typed code.
46. **Use `WeakMap` for caches keyed by objects** — automatic garbage collection of unreachable keys.
47. **Use `Map` over plain objects for dynamic key-value stores** — better lookup performance, no prototype chain.
48. **Use `Set` for membership checks** — O(1) vs O(n) for `Array.includes`.
49. **Batch DOM updates.** Multiple state changes → single re-render (React 18 auto-batching).
50. **Web Workers for heavy computation** — layout calculation, data processing off main thread.

## V. Error Handling

51. **Error boundaries at every major component.** Catch render errors, show fallback UI.
52. **Try/catch around all async operations.** `async/await` with proper error handling.
53. **Typed error responses from API.** Use discriminated unions:
    ```typescript
    type ApiResult<T> =
      | { ok: true; data: T }
      | { ok: false; error: { code: string; message: string } };
    ```
54. **Never `catch(e) {}` silently.** Log or re-throw.

## VI. Security

55. **No `dangerouslySetInnerHTML`** unless content is sanitized with DOMPurify.
56. **Sanitize all user-generated content** before rendering (service names, hostnames in topology).
57. **CSP-compatible:** No `eval()`, no inline scripts, no inline styles for complex cases.
58. **No credentials in client-side state.** Tokens in httpOnly cookies only.
59. **Validate WebSocket messages** against expected schema before processing.

## VII. Testing

60. **Vitest for unit and component tests.** Jest-compatible API, native ESM, fast.
61. **Testing Library for component tests.** Test behavior, not implementation.
62. **Visual regression tests** for topology rendering (screenshot comparison).
63. **`data-testid` selectors** for all E2E tests — no CSS class or text selectors.

## VIII. Code Organization

64. **Named exports only.** No default exports (Google style guide).
65. **Barrel files (`index.ts`) sparingly** — they can cause circular dependencies and tree-shaking issues.
66. **Co-locate tests with source:** `Component.tsx` + `Component.test.tsx`.
67. **Type files in `types/` directory.** Shared interfaces and types, not component-specific.
68. **Files under 500 lines.** Functions under 50 lines.

## IX. Forbidden Patterns

69. **No `any` type.** Use `unknown` and narrow.
70. **No `as` assertion on external data.** Validate first.
71. **No class components.** Functional only.
72. **No direct DOM manipulation.** Use React refs and state.
73. **No `setInterval` for rendering.** Use `requestAnimationFrame`.
74. **No allocations in render loop.** Pre-allocate and reuse.
75. **No inline styles for complex styling.** Use CSS modules or styled-components.
76. **No prop drilling >3 levels.** Use Zustand or composition.
77. **No `dangerouslySetInnerHTML` without sanitization.**
78. **No default exports.**

