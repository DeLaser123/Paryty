import { describe, it, expect } from 'vitest';
import { ViewportController } from '../../engine/viewport';

/**
 * Minimal stand-in for a PIXI.Container stage. ViewportController only mutates
 * `pivot`, `scale`, and `position` via `.set(x, y)`, so a fake suffices to
 * exercise the pan/zoom math without a WebGL context.
 */
function makeFakeStage() {
  return {
    pivot: { x: 0, y: 0, set(x: number, y: number) { this.x = x; this.y = y; } },
    scale: { x: 1, y: 1, set(x: number, y: number) { this.x = x; this.y = y; } },
    position: { x: 0, y: 0, set(x: number, y: number) { this.x = x; this.y = y; } },
  };
}

function makeController(width = 800, height = 600) {
  const stage = makeFakeStage();
  // The fake structurally matches the subset of PIXI.Container we use.
  const vp = new ViewportController(stage as unknown as import('pixi.js').Container, width, height);
  return { vp, stage };
}

describe('ViewportController coordinate input', () => {
  it('zooms in on negative wheel delta and out on positive, clamped to limits', () => {
    const { vp } = makeController();
    const start = vp.currentZoom;

    vp.inputWheel(400, 300, -100); // zoom in
    expect(vp.currentZoom).toBeGreaterThan(start);

    const zoomedIn = vp.currentZoom;
    vp.inputWheel(400, 300, 100); // zoom out
    expect(vp.currentZoom).toBeLessThan(zoomedIn);
  });

  it('keeps the world point under the cursor stable across a wheel zoom', () => {
    const { vp } = makeController();
    const screenX = 250;
    const screenY = 180;
    const worldBefore = vp.screenToWorld(screenX, screenY);

    vp.inputWheel(screenX, screenY, -100);

    const worldAfter = vp.screenToWorld(screenX, screenY);
    expect(worldAfter.x).toBeCloseTo(worldBefore.x, 4);
    expect(worldAfter.y).toBeCloseTo(worldBefore.y, 4);
  });

  it('pans with a middle-button drag and reports drag start', () => {
    const { vp } = makeController();
    const before = vp.getState();

    const started = vp.inputPointerDown(100, 100, 1, false);
    expect(started).toBe(true);

    vp.inputPointerMove(150, 130); // drag right/down by (50, 30)
    const after = vp.getState();

    // Dragging the pointer right/down moves the pivot left/up (content follows pointer).
    expect(after.x).toBeLessThan(before.x);
    expect(after.y).toBeLessThan(before.y);

    vp.inputPointerUp();
    const settled = vp.getState();
    vp.inputPointerMove(400, 400); // no-op after pointer up
    expect(vp.getState()).toEqual(settled);
  });

  it('does not start a drag for a plain left click (no shift)', () => {
    const { vp } = makeController();
    expect(vp.inputPointerDown(100, 100, 0, false)).toBe(false);

    const before = vp.getState();
    vp.inputPointerMove(200, 200); // must be ignored (no active drag)
    expect(vp.getState()).toEqual(before);
  });

  it('starts a drag for shift + left button', () => {
    const { vp } = makeController();
    expect(vp.inputPointerDown(100, 100, 0, true)).toBe(true);
  });
});
