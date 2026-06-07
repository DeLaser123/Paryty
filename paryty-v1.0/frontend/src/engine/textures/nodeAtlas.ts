/**
 * Texture Atlas Manager for GPU Rendering Engine
 *
 * Singleton wrapper around generateTextureAtlas that provides
 * lazy-initialized, cached texture lookups by (type, status) key.
 * Also provides edge line textures and particle textures.
 *
 * Performance: textures are generated once on first access and
 * reused for all subsequent lookups. Dispose to free GPU memory.
 */

import * as PIXI from 'pixi.js';
import { generateTextureAtlas, generateParticleTexture, STATUS_COLORS } from './generateTextures';

/** Default fallback texture key for unknown type/status combinations. */
const DEFAULT_TEXTURE_KEY = 'host:unknown';

/** Edge line types mapped to base colors. */
const EDGE_COLORS: Record<string, number> = {
  network: 0x3498db,
  dependency: 0xe67e22,
  contains: 0x9b59b6,
  calls: 0x1abc9c,
};

/** Default edge color for unknown types. */
const DEFAULT_EDGE_COLOR = 0x656565;

/**
 * Manages a pre-generated texture atlas for node rendering.
 *
 * Lazily initializes on first `getTexture()` call using the provided renderer.
 * Provides O(1) texture lookups by "type:status" key for instanced rendering.
 */
export class TextureAtlas {
  private atlas: Map<string, PIXI.Texture> | null = null;
  private edgeTextures: Map<string, PIXI.Texture> | null = null;
  private particleTexture: PIXI.Texture | null = null;
  private renderer: PIXI.Renderer;
  private disposed = false;

  constructor(renderer: PIXI.Renderer) {
    this.renderer = renderer;
  }

  /**
   * Ensures the atlas is initialized. Called automatically by getTexture().
   * Safe to call multiple times — only initializes once.
   */
  private ensureInitialized(): void {
    if (this.atlas !== null) return;

    this.atlas = generateTextureAtlas(this.renderer);
    this.edgeTextures = new Map();
    this.particleTexture = generateParticleTexture(this.renderer, 8);
  }

  /**
   * Gets the texture for a given node type and status.
   * Falls back to "host:unknown" if the exact key is not found.
   *
   * @param nodeType - Node type: 'host' | 'container' | 'service' | 'process'
   * @param status - Node status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown'
   * @returns PIXI.Texture for the requested (type, status) combination
   */
  getTexture(nodeType: string, status: string): PIXI.Texture {
    this.ensureInitialized();

    const key = `${nodeType}:${status}`;
    let texture = this.atlas!.get(key);

    if (!texture) {
      // Try just the status with default type
      texture = this.atlas!.get(`host:${status}`);
    }

    if (!texture) {
      // Ultimate fallback
      texture = this.atlas!.get(DEFAULT_TEXTURE_KEY);
    }

    // This should never be null after atlas generation, but guard anyway
    return texture!;
  }

  /**
   * Gets or creates a colored line texture for edge rendering.
   * Edge textures are 1px-wide colored lines cached by edge type.
   *
   * @param edgeType - Edge type: 'network' | 'dependency' | 'contains' | 'calls'
   * @returns PIXI.Texture for the edge line
   */
  getEdgeTexture(edgeType: string): PIXI.Texture {
    this.ensureInitialized();

    const cached = this.edgeTextures!.get(edgeType);
    if (cached) return cached;

    const color = EDGE_COLORS[edgeType] ?? DEFAULT_EDGE_COLOR;
    const g = new PIXI.Graphics();
    g.beginFill(color, 1.0);
    g.lineStyle(0);
    g.drawRect(0, 0, 4, 4);
    g.endFill();

    const texture = this.renderer.generateTexture(g, {
      resolution: 1,
      region: new PIXI.Rectangle(0, 0, 4, 4),
    });
    g.destroy();

    this.edgeTextures!.set(edgeType, texture);
    return texture;
  }

  /**
   * Gets the pre-generated white circle texture for particles.
   *
   * @returns PIXI.Texture for particle sprites
   */
  getParticleTexture(): PIXI.Texture {
    this.ensureInitialized();
    return this.particleTexture!;
  }

  /**
   * Gets the color for a given status from the design system tokens.
   *
   * @param status - Status string
   * @returns Hex color number
   */
  getStatusColor(status: string): number {
    return STATUS_COLORS[status] ?? STATUS_COLORS.unknown;
  }

  /**
   * Gets the color for a given edge type.
   *
   * @param edgeType - Edge type string
   * @returns Hex color number
   */
  getEdgeColor(edgeType: string): number {
    return EDGE_COLORS[edgeType] ?? DEFAULT_EDGE_COLOR;
  }

  /**
   * Checks if the atlas has been initialized.
   *
   * @returns true if textures have been generated
   */
  isInitialized(): boolean {
    return this.atlas !== null;
  }

  /**
   * Destroys all textures and frees GPU memory.
   * After calling dispose(), the atlas cannot be reused.
   */
  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;

    if (this.atlas) {
      for (const texture of this.atlas.values()) {
        texture.destroy(true);
      }
      this.atlas.clear();
      this.atlas = null;
    }

    if (this.edgeTextures) {
      for (const texture of this.edgeTextures.values()) {
        texture.destroy(true);
      }
      this.edgeTextures.clear();
      this.edgeTextures = null;
    }

    if (this.particleTexture) {
      this.particleTexture.destroy(true);
      this.particleTexture = null;
    }
  }
}
