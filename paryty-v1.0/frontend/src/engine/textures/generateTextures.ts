/**
 * Texture Generation for GPU Rendering Engine
 *
 * Creates pre-rendered PIXI textures for each (nodeType, status) combination.
 * Shapes: host=circle, container=rounded-square, service=hexagon, process=diamond
 * Status colors from design system tokens:
 *   healthy: 0x4ade80, degraded: 0xfb923c, unhealthy: 0xff4444, unknown: 0x656565
 */

import * as PIXI from 'pixi.js';

/** Texture size in pixels (base dimension for all node shapes). */
const TEXTURE_SIZE = 64;

/** Status colors mapped from design system tokens. */
export const STATUS_COLORS: Record<string, number> = {
  healthy: 0x4ade80,
  degraded: 0xfb923c,
  unhealthy: 0xff4444,
  unknown: 0x656565,
};

/** Border color (subtle white outline from design system). */
const BORDER_COLOR = 0xffffff;

/** Border alpha for shape outlines. */
const BORDER_ALPHA = 0.3;

/** Status indicator dot radius. */
const STATUS_DOT_RADIUS = 5;

/** Shape fill alpha. */
const FILL_ALPHA = 0.85;

/** Border width in pixels. */
const BORDER_WIDTH = 1.5;

/**
 * Draws a circle shape (host node) on a Graphics object.
 * @param g - PIXI.Graphics instance to draw on
 */
function drawCircle(g: PIXI.Graphics): void {
  const r = TEXTURE_SIZE / 2 - 4;
  g.drawCircle(TEXTURE_SIZE / 2, TEXTURE_SIZE / 2, r);
}

/**
 * Draws a rounded rectangle shape (container node) on a Graphics object.
 * @param g - PIXI.Graphics instance to draw on
 */
function drawRoundedRect(g: PIXI.Graphics): void {
  const pad = 4;
  const size = TEXTURE_SIZE - pad * 2;
  g.drawRoundedRect(pad, pad, size, size, 8);
}

/**
 * Draws a hexagon shape (service node) on a Graphics object.
 * @param g - PIXI.Graphics instance to draw on
 */
function drawHexagon(g: PIXI.Graphics): void {
  const cx = TEXTURE_SIZE / 2;
  const cy = TEXTURE_SIZE / 2;
  const r = TEXTURE_SIZE / 2 - 4;
  const points: number[] = [];
  for (let i = 0; i < 6; i++) {
    const angle = (Math.PI / 3) * i - Math.PI / 6;
    points.push(cx + r * Math.cos(angle), cy + r * Math.sin(angle));
  }
  g.drawPolygon(points);
}

/**
 * Draws a diamond shape (process node) on a Graphics object.
 * @param g - PIXI.Graphics instance to draw on
 */
function drawDiamond(g: PIXI.Graphics): void {
  const cx = TEXTURE_SIZE / 2;
  const cy = TEXTURE_SIZE / 2;
  const hw = TEXTURE_SIZE / 2 - 4;
  const hh = TEXTURE_SIZE / 2 - 4;
  g.drawPolygon([cx, cy - hh, cx + hw, cy, cx, cy + hh, cx - hw, cy]);
}

/**
 * Draws a status indicator dot in the top-right corner of the texture.
 * @param g - PIXI.Graphics instance to draw on
 * @param color - Hex color for the status dot
 */
function drawStatusDot(g: PIXI.Graphics, color: number): void {
  const dotX = TEXTURE_SIZE - 8;
  const dotY = 8;
  g.beginFill(color, 1.0);
  g.lineStyle(0);
  g.drawCircle(dotX, dotY, STATUS_DOT_RADIUS);
  g.endFill();
}

type ShapeDrawFn = (g: PIXI.Graphics) => void;

/** Map of node types to their shape drawing functions. */
const SHAPE_DRAWERS: Record<string, ShapeDrawFn> = {
  host: drawCircle,
  container: drawRoundedRect,
  service: drawHexagon,
  process: drawDiamond,
};

/** All known node types for texture generation. */
const NODE_TYPES = ['host', 'container', 'service', 'process'] as const;

/** All known statuses for texture generation. */
const STATUSES = ['healthy', 'degraded', 'unhealthy', 'unknown'] as const;

/**
 * Generates a texture atlas containing pre-rendered textures for all
 * (nodeType, status) combinations.
 *
 * @param renderer - PIXI renderer used to convert Graphics to textures
 * @returns Map keyed by "type:status" (e.g., "host:healthy") → PIXI.Texture
 */
export function generateTextureAtlas(
  renderer: PIXI.Renderer,
): Map<string, PIXI.Texture> {
  const atlas = new Map<string, PIXI.Texture>();

  for (const status of STATUSES) {
    const statusColor = STATUS_COLORS[status] ?? STATUS_COLORS.unknown;

    for (const nodeType of NODE_TYPES) {
      const drawShape = SHAPE_DRAWERS[nodeType];
      if (!drawShape) continue;

      const g = new PIXI.Graphics();

      // Draw filled shape
      g.beginFill(statusColor, FILL_ALPHA);
      g.lineStyle(BORDER_WIDTH, BORDER_COLOR, BORDER_ALPHA);
      drawShape(g);
      g.endFill();

      // Draw status indicator dot
      drawStatusDot(g, statusColor);

      // Convert Graphics to texture
      const texture = renderer.generateTexture({
        target: g,
        resolution: 2,
        frame: new PIXI.Rectangle(0, 0, TEXTURE_SIZE, TEXTURE_SIZE),
      });
      atlas.set(`${nodeType}:${status}`, texture);

      // Clean up temporary Graphics
      g.destroy();
    }
  }

  return atlas;
}

/**
 * Generates a plain white circle texture for particles.
 *
 * @param renderer - PIXI renderer used to convert Graphics to texture
 * @param size - Diameter of the circle in pixels
 * @returns PIXI.Texture of a white filled circle
 */
export function generateParticleTexture(
  renderer: PIXI.Renderer,
  size: number = 8,
): PIXI.Texture {
  const g = new PIXI.Graphics();
  g.beginFill(0xffffff, 1.0);
  g.lineStyle(0);
  g.drawCircle(size / 2, size / 2, size / 2);
  g.endFill();

  const texture = renderer.generateTexture({
    target: g,
    resolution: 2,
    frame: new PIXI.Rectangle(0, 0, size, size),
  });
  g.destroy();

  return texture;
}
