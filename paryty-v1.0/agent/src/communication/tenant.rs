//! Tenant Resolution and Local Cache
//!
//! Handles local caching of the tenant ID on disk. After registration,
//! the tenant_id is persisted so the agent doesn't need to re-authenticate
//! on restart. The cache is a single-line file at `<data_dir>/tenant_id`.
//!
//! The `TenantCache` is intentionally simple — it uses `tokio::fs` for
//! async file I/O and `anyhow::Result` for error propagation, consistent
//! with the rest of the agent's I/O patterns.

use anyhow::Result;
use std::path::{Path, PathBuf};
use tokio::fs;
use tracing::{debug, warn};

/// TenantCache handles local caching of tenant ID on disk.
///
/// After registration, the tenant_id is cached so the agent doesn't
/// need to re-authenticate on restart. The cache file is a plain-text
/// file containing just the tenant ID string.
pub struct TenantCache {
    /// Path to the tenant ID cache file.
    cache_path: PathBuf,
}

impl TenantCache {
    /// Create a new `TenantCache` rooted at the given data directory.
    ///
    /// The cache file will be created at `<data_dir>/tenant_id`.
    pub fn new(data_dir: &Path) -> Self {
        Self { cache_path: data_dir.join("tenant_id") }
    }

    /// Read cached tenant ID from disk.
    ///
    /// Returns `Some(tenant_id)` if the cache file exists and contains
    /// a non-empty string. Returns `None` if the file doesn't exist,
    /// is empty, or cannot be read.
    pub async fn read(&self) -> Option<String> {
        match fs::read_to_string(&self.cache_path).await {
            Ok(content) => {
                let trimmed = content.trim().to_string();
                if trimmed.is_empty() {
                    debug!(path = %self.cache_path.display(), "Tenant cache file is empty");
                    None
                } else {
                    debug!(
                        path = %self.cache_path.display(),
                        tenant_id = %trimmed,
                        "Read tenant ID from cache"
                    );
                    Some(trimmed)
                }
            }
            Err(e) => {
                debug!(
                    path = %self.cache_path.display(),
                    error = %e,
                    "Could not read tenant cache (may not exist yet)"
                );
                None
            }
        }
    }

    /// Write tenant ID to disk cache.
    ///
    /// Creates the parent directory if it doesn't exist. Overwrites any
    /// existing cache file.
    pub async fn write(&self, tenant_id: &str) -> Result<()> {
        if let Some(parent) = self.cache_path.parent() {
            fs::create_dir_all(parent).await?;
        }
        fs::write(&self.cache_path, tenant_id).await?;
        debug!(
            path = %self.cache_path.display(),
            tenant_id = %tenant_id,
            "Wrote tenant ID to cache"
        );
        Ok(())
    }

    /// Clear the cached tenant ID.
    ///
    /// Removes the cache file if it exists. Does nothing if the file
    /// doesn't exist.
    pub async fn clear(&self) -> Result<()> {
        if self.cache_path.exists() {
            fs::remove_file(&self.cache_path).await?;
            debug!(path = %self.cache_path.display(), "Cleared tenant cache");
        } else {
            warn!(path = %self.cache_path.display(), "Tenant cache file does not exist, nothing to clear");
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_read_write_roundtrip() {
        let dir = tempfile::tempdir().expect("failed to create temp dir");
        let cache = TenantCache::new(dir.path());

        // Initially empty — should return None.
        assert!(cache.read().await.is_none());

        // Write a tenant ID.
        let tenant_id = "tenant-abc-123";
        cache.write(tenant_id).await.expect("write should succeed");

        // Read it back.
        let read_back = cache.read().await.expect("read should return Some");
        assert_eq!(read_back, tenant_id);
    }

    #[tokio::test]
    async fn test_read_nonexistent() {
        let dir = tempfile::tempdir().expect("failed to create temp dir");
        let cache = TenantCache::new(dir.path());

        // Cache file doesn't exist yet — should return None, not an error.
        assert!(cache.read().await.is_none());
    }

    #[tokio::test]
    async fn test_clear() {
        let dir = tempfile::tempdir().expect("failed to create temp dir");
        let cache = TenantCache::new(dir.path());

        // Write then clear.
        cache.write("tenant-to-clear").await.expect("write should succeed");
        assert!(cache.read().await.is_some());

        cache.clear().await.expect("clear should succeed");
        assert!(cache.read().await.is_none());

        // Clear again on already-empty cache should not error.
        cache.clear().await.expect("clear on empty cache should succeed");
    }

    #[tokio::test]
    async fn test_write_creates_parent_directory() {
        let dir = tempfile::tempdir().expect("failed to create temp dir");
        let nested = dir.path().join("deeply").join("nested");
        let cache = TenantCache::new(&nested);

        // Write to a cache whose parent doesn't exist yet.
        cache.write("nested-tenant").await.expect("write should create parent dirs");

        let read_back = cache.read().await.expect("read should return Some");
        assert_eq!(read_back, "nested-tenant");
    }
}
