# ADR-005: PixiJS over Three.js for GPU Rendering

## Status: Accepted

## Date: 2025-01-15

## Context

Paryty's frontend renders topology maps with 10K-15K+ nodes at 60fps. Requirements:
- GPU-accelerated 2D rendering
- Particle system for data flow visualization
- Instanced rendering for large node counts
- Simple API for maintenance

## Decision

**PixiJS** for GPU rendering. Not Three.js, not raw WebGL.

## Rationale

| Criterion | PixiJS | Three.js | Raw WebGL/WebGPU |
|-----------|--------|----------|------------------|
| Dimension | 2D-optimized | 3D (2D mode available) | N/A |
| API complexity | Low | Medium | Very high |
| Instanced rendering | ParticleContainer | InstancedMesh | Manual |
| Particle system | Built-in (ParticleContainer) | Manual or plugin | Manual |
| Bundle size | ~150 KB | ~600 KB | 0 (native) |
| Performance (2D) | Excellent | Good (overhead for 3D) | Maximum |
| Browser support | Excellent | Excellent | WebGL: Excellent, WebGPU: Limited |
| Learning curve | Low | Medium | Very high |

**Key factors:**
1. **2D-optimized** — Paryty topology is a 2D graph. Three.js adds 3D overhead that's never used.
2. **ParticleContainer** — Built-in instanced rendering for 15K+ sprites at 60fps. No manual buffer management.
3. **Simple API** — Easier to maintain and extend. New developers contribute faster.
4. **Battle-tested** — Used by thousands of production applications.

## Consequences

- **No 3D visualization** — If 3D topology views are needed later, Three.js can be added alongside PixiJS.
- **WebGL only (not WebGPU)** — WebGPU support in PixiJS is in progress. WebGL is sufficient for V1.0.
- **D3-force for layout** — PixiJS handles rendering; D3-force handles node positioning. Clean separation.

## Alternatives Considered

1. **Three.js** — Rejected: 3D overhead for 2D use case, larger bundle, more complex API.
2. **Raw WebGL/WebGPU** — Rejected: Too much boilerplate, steep learning curve, WebGPU has limited browser support.
3. **Konva.js** — Rejected: Not GPU-accelerated, performance degrades at 2K+ nodes.
