import { describe, it, expect } from 'vitest';
import {
  encodePositions,
  decodePositions,
  packPositionsCommand,
  transferListFor,
  isRenderEvent,
  isRenderCommand,
  type NodePosition,
  type RenderCommand,
} from '../../engine/workers/renderProtocol';

describe('position codec', () => {
  it('round-trips positions through encode/decode', () => {
    const positions: NodePosition[] = [
      { x: 1.5, y: -2.25 },
      { x: 0, y: 0 },
      { x: 1024, y: 768 },
    ];
    const buffer = encodePositions(positions);
    expect(buffer).toBeInstanceOf(Float32Array);
    expect(buffer.length).toBe(positions.length * 2);

    const decoded = decodePositions(buffer);
    expect(decoded).toEqual(positions);
  });

  it('produces an empty buffer for an empty position list', () => {
    const buffer = encodePositions([]);
    expect(buffer.length).toBe(0);
    expect(decodePositions(buffer)).toEqual([]);
  });

  it('throws on a malformed (odd-length) buffer', () => {
    expect(() => decodePositions(new Float32Array(3))).toThrow(RangeError);
  });
});

describe('packPositionsCommand', () => {
  it('packs a positions command and transfers the backing ArrayBuffer', () => {
    const positions: NodePosition[] = [
      { x: 10, y: 20 },
      { x: 30, y: 40 },
    ];
    const { message, transfer } = packPositionsCommand(positions);

    expect(message.type).toBe('positions');
    expect(decodePositions(message.buffer)).toEqual(positions);

    // The transfer list must contain exactly the buffer's memory so the
    // postMessage moves (not copies) it to the worker.
    expect(transfer).toHaveLength(1);
    expect(transfer[0]).toBe(message.buffer.buffer);
  });
});

describe('transferListFor', () => {
  it('transfers the buffer for positions commands', () => {
    const cmd = packPositionsCommand([{ x: 1, y: 2 }]).message;
    expect(transferListFor(cmd)).toEqual([cmd.buffer.buffer]);
  });

  it('returns an empty transfer list for plain commands', () => {
    const resize: RenderCommand = { type: 'resize', width: 800, height: 600 };
    expect(transferListFor(resize)).toEqual([]);
  });
});

describe('type guards', () => {
  it('isRenderEvent accepts known events and rejects others', () => {
    expect(isRenderEvent({ type: 'ready' })).toBe(true);
    expect(isRenderEvent({ type: 'fps', value: 60 })).toBe(true);
    expect(isRenderEvent({ type: 'nodeClick', id: 'n1' })).toBe(true);
    expect(isRenderEvent({ type: 'viewportChange', x: 0, y: 0, zoom: 1 })).toBe(true);
    expect(isRenderEvent({ type: 'positions' })).toBe(false);
    expect(isRenderEvent(null)).toBe(false);
    expect(isRenderEvent('ready')).toBe(false);
  });

  it('isRenderCommand accepts known commands and rejects others', () => {
    expect(isRenderCommand({ type: 'resize', width: 1, height: 1 })).toBe(true);
    expect(isRenderCommand({ type: 'destroy' })).toBe(true);
    expect(isRenderCommand({ type: 'updateNodes', nodes: [] })).toBe(true);
    expect(isRenderCommand({ type: 'updateEdges', edges: [] })).toBe(true);
    expect(isRenderCommand({ type: 'selectNode', id: null })).toBe(true);
    expect(isRenderCommand({ type: 'zoomToNode', id: 'n1' })).toBe(true);
    expect(isRenderCommand({ type: 'pointer', kind: 'wheel', x: 0, y: 0 })).toBe(true);
    expect(isRenderCommand({ type: 'fps', value: 60 })).toBe(false);
    expect(isRenderCommand(undefined)).toBe(false);
  });

  it('keeps updateNodes/updateEdges/selectNode as plain (non-transfer) commands', () => {
    expect(transferListFor({ type: 'updateNodes', nodes: [] })).toEqual([]);
    expect(transferListFor({ type: 'updateEdges', edges: [] })).toEqual([]);
    expect(transferListFor({ type: 'selectNode', id: 'n1' })).toEqual([]);
    expect(transferListFor({ type: 'zoomToNode', id: 'n1' })).toEqual([]);
  });
});
