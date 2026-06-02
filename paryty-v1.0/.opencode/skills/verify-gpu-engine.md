# Verify GPU Engine

Verify the GPU Rendering Engine (`frontend/src/engine/`) for performance, correctness, and memory safety.

## Verification Steps

### 1. Type Check & Build
```bash
cd frontend && npx tsc --noEmit 2>&1
cd frontend && npm run build 2>&1
```

### 2. Unit Tests
```bash
cd frontend && npm run test -- --reporter=verbose 2>&1
```
Verify:
- Node rendering correctness
- Edge rendering with curves
- Force-directed layout convergence
- Object pool recycling

### 3. Performance Tests
- Frame rate: 60fps with 10K nodes, 50K edges
- GPU memory: <512MB with 15K nodes
- Layout convergence: <2s for 10K nodes
- Context recovery: <500ms after context loss

### 4. Memory Leak Tests
- 1-hour continuous rendering: no memory growth
- Particle pool: all particles recycled
- Textures: disposed on zoom out

### 5. WebGL Context Loss Test
- Simulate context loss: all GPU resources released
- Simulate context restore: all GPU resources rebuilt
- No visual artifacts after recovery

### 6. Security Audit
- [ ] No arbitrary shader injection
- [ ] Sandboxed WebGL context
- [ ] No XSS via node labels
- [ ] CSP compatible

## Pass Criteria
- Type check passes
- Build succeeds
- All tests pass
- Performance targets met
- No memory leaks
- WebGL context loss handled
- Security checklist clean
