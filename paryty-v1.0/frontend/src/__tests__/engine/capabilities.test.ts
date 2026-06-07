import { describe, it, expect } from 'vitest';
import {
  detectRenderCapabilities,
  canUseRenderWorker,
  selectRenderMode,
  type RenderCapabilities,
} from '../../engine/capabilities';

/** Builds a capability set with every flag off, overriding selected fields. */
function caps(overrides: Partial<RenderCapabilities>): RenderCapabilities {
  return {
    worker: false,
    offscreenCanvas: false,
    transferControl: false,
    crossOriginIsolated: false,
    sharedArrayBuffer: false,
    ...overrides,
  };
}

describe('detectRenderCapabilities', () => {
  it('returns a fully-populated boolean capability set without throwing', () => {
    const result = detectRenderCapabilities();
    expect(typeof result.worker).toBe('boolean');
    expect(typeof result.offscreenCanvas).toBe('boolean');
    expect(typeof result.transferControl).toBe('boolean');
    expect(typeof result.crossOriginIsolated).toBe('boolean');
    expect(typeof result.sharedArrayBuffer).toBe('boolean');
  });
});

describe('canUseRenderWorker', () => {
  it('requires worker, offscreenCanvas, and transferControl together', () => {
    expect(
      canUseRenderWorker(
        caps({ worker: true, offscreenCanvas: true, transferControl: true }),
      ),
    ).toBe(true);
  });

  it('is false when any of the three required features is missing', () => {
    expect(canUseRenderWorker(caps({ offscreenCanvas: true, transferControl: true }))).toBe(false);
    expect(canUseRenderWorker(caps({ worker: true, transferControl: true }))).toBe(false);
    expect(canUseRenderWorker(caps({ worker: true, offscreenCanvas: true }))).toBe(false);
  });

  it('does not require SharedArrayBuffer (zero-copy is preferred, not required)', () => {
    expect(
      canUseRenderWorker(
        caps({
          worker: true,
          offscreenCanvas: true,
          transferControl: true,
          sharedArrayBuffer: false,
          crossOriginIsolated: false,
        }),
      ),
    ).toBe(true);
  });
});

describe('selectRenderMode', () => {
  it("selects 'worker' when capable", () => {
    expect(
      selectRenderMode(
        caps({ worker: true, offscreenCanvas: true, transferControl: true }),
      ),
    ).toBe('worker');
  });

  it("falls back to 'main' when incapable", () => {
    expect(selectRenderMode(caps({}))).toBe('main');
  });
});
