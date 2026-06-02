#![allow(dead_code)]

//! Compression Layer for Agent Communication
//!
//! Provides Zstd compression for all outgoing data.
//! Compression is mandatory for all outgoing data to minimize bandwidth.
//!
//! Research basis:
//! - Zstd documentation (Facebook): Tunable compression levels
//! - "Data Compression: The Complete Reference" (Salomon): Compression algorithms

use anyhow::{Context, Result};
use std::io::{Read, Write};
use tracing::debug;

/// Compression algorithm selection.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CompressionAlgorithm {
    /// Zstd — best compression ratio for structured data.
    /// Level 3 provides ~4x compression with fast decompression.
    Zstd,
    /// No compression (for pre-compressed or small data).
    None,
}

/// Compressor that handles Zstd compression/decompression.
pub struct Compressor {
    /// Zstd compression level (1-22, default 3 for speed).
    zstd_level: i32,
}

impl Compressor {
    /// Create a new compressor with default settings.
    pub fn new() -> Self {
        Self {
            zstd_level: 3, // Fast compression, good ratio
        }
    }

    /// Create a compressor with custom settings.
    pub fn with_settings(zstd_level: i32) -> Self {
        Self { zstd_level: zstd_level.clamp(1, 22) }
    }

    /// Compress data using the specified algorithm.
    pub fn compress(&self, data: &[u8], algorithm: CompressionAlgorithm) -> Result<Vec<u8>> {
        match algorithm {
            CompressionAlgorithm::Zstd => self.compress_zstd(data),
            CompressionAlgorithm::None => Ok(data.to_vec()),
        }
    }

    /// Decompress data using the specified algorithm.
    pub fn decompress(&self, data: &[u8], algorithm: CompressionAlgorithm) -> Result<Vec<u8>> {
        match algorithm {
            CompressionAlgorithm::Zstd => self.decompress_zstd(data),
            CompressionAlgorithm::None => Ok(data.to_vec()),
        }
    }

    /// Compress metrics data using Zstd.
    pub fn compress_metrics(&self, data: &[u8]) -> Result<Vec<u8>> {
        self.compress_zstd(data)
    }

    /// Compress trace data using Zstd.
    pub fn compress_traces(&self, data: &[u8]) -> Result<Vec<u8>> {
        self.compress_zstd(data)
    }

    /// Compress using Zstd.
    fn compress_zstd(&self, data: &[u8]) -> Result<Vec<u8>> {
        let mut encoder = zstd::Encoder::new(Vec::new(), self.zstd_level)
            .context("Failed to create Zstd encoder")?;
        encoder.write_all(data).context("Failed to write data to Zstd encoder")?;
        let compressed = encoder.finish().context("Failed to finish Zstd compression")?;

        let ratio =
            if !compressed.is_empty() { data.len() as f64 / compressed.len() as f64 } else { 1.0 };

        debug!(
            "Zstd compressed {} bytes -> {} bytes ({:.1}x ratio)",
            data.len(),
            compressed.len(),
            ratio
        );

        Ok(compressed)
    }

    /// Decompress using Zstd.
    fn decompress_zstd(&self, data: &[u8]) -> Result<Vec<u8>> {
        let mut decoder = zstd::Decoder::new(data).context("Failed to create Zstd decoder")?;
        let mut decompressed = Vec::new();
        decoder.read_to_end(&mut decompressed).context("Failed to decompress Zstd data")?;
        Ok(decompressed)
    }
}

impl Default for Compressor {
    fn default() -> Self {
        Self::new()
    }
}
