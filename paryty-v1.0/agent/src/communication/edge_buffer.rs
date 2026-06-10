#![allow(dead_code)]

//! Edge Buffer — Write-Ahead Log for Zero Data Loss
//!
//! Provides a bounded ring buffer with SQLite disk spillover for buffering
//! metric data during network disconnections. Ensures zero data loss
//! by persisting to disk when memory buffer is full.
//!
//! Architecture:
//! - In-memory `VecDeque` ring buffer (fast path)
//! - SQLite WAL-mode database (spillover when memory is full)
//! - Bincode serialization for BLOB persistence
//! - Priority-based ordering for SQLite entries
//!
//! Research basis:
//! - "The Art of Multiprocessor Programming" (Herlihy/Shavit): Lock-free ring buffers
//! - MIT 6.172: Memory-efficient data structures

use anyhow::{Context, Result};
use std::collections::VecDeque;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use tokio::sync::RwLock;
use tracing::{debug, error, info, warn};

use std::sync::atomic::AtomicBool;

/// Maximum number of entries to drain from SQLite per call.
const SQLITE_DRAIN_BATCH_SIZE: usize = 1000;

/// Serializable payload for SQLite BLOB storage.
///
/// Contains the entry metadata and data that gets serialized with bincode
/// and stored as a BLOB in the SQLite `buffer` table. The `data_type` is
/// stored as a separate TEXT column for SQL-level filtering.
#[derive(serde::Serialize, serde::Deserialize)]
struct SqlitePayload {
    sequence_number: u64,
    data: Vec<u8>,
}

/// An entry in the edge buffer with metadata.
#[derive(Debug, Clone)]
pub struct BufferEntry {
    /// Unique sequence number for deduplication.
    pub sequence_number: u64,
    /// When this entry was created.
    pub created_at: Instant,
    /// The serialized data payload.
    pub data: Vec<u8>,
    /// Data type (metrics, traces, events).
    pub data_type: DataType,
    /// SQLite row ID. Present only for entries read from SQLite;
    /// used by `mark_sent()` and `mark_retry()` to acknowledge delivery.
    pub sqlite_row_id: Option<i64>,
}

/// Type of data being buffered.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DataType {
    Metrics,
    Traces,
    Events,
    NetworkEvents,
}

impl DataType {
    /// Convert to a string identifier for SQLite storage.
    fn as_str(&self) -> &'static str {
        match self {
            DataType::Metrics => "metrics",
            DataType::Traces => "traces",
            DataType::Events => "events",
            DataType::NetworkEvents => "network_events",
        }
    }

    /// Parse from a string identifier (used when reading from SQLite).
    fn from_str(s: &str) -> Option<Self> {
        match s {
            "metrics" => Some(DataType::Metrics),
            "traces" => Some(DataType::Traces),
            "events" => Some(DataType::Events),
            "network_events" => Some(DataType::NetworkEvents),
            _ => None,
        }
    }
}

/// Configuration for the edge buffer.
#[derive(Debug, Clone)]
pub struct EdgeBufferConfig {
    /// Maximum memory buffer size in bytes (default: 100MB).
    pub max_memory_bytes: usize,
    /// Maximum number of entries (default: 100,000).
    pub max_entries: usize,
    /// TTL for entries before eviction (default: 24h).
    pub entry_ttl: Duration,
    /// Directory for disk spillover (default: /tmp/paryty-edge-buffer).
    pub disk_spill_dir: PathBuf,
    /// Whether disk spillover is enabled.
    pub disk_spill_enabled: bool,
    /// Maximum disk usage in bytes (default: 1GB).
    pub max_disk_bytes: u64,
    /// Path to SQLite database file for persistent spillover.
    /// When `None`, the buffer operates in memory-only mode (backward compatible).
    pub sqlite_path: Option<String>,
}

impl Default for EdgeBufferConfig {
    fn default() -> Self {
        Self {
            max_memory_bytes: 100 * 1024 * 1024, // 100MB
            max_entries: 100_000,
            entry_ttl: Duration::from_secs(24 * 60 * 60), // 24h
            disk_spill_dir: PathBuf::from("/tmp/paryty-edge-buffer"),
            disk_spill_enabled: true,
            max_disk_bytes: 1024 * 1024 * 1024, // 1GB
            sqlite_path: None,
        }
    }
}

/// Metrics tracked by the edge buffer.
#[derive(Debug, Default)]
pub struct BufferMetrics {
    pub entries_written: AtomicU64,
    pub entries_read: AtomicU64,
    pub entries_evicted: AtomicU64,
    pub entries_disk_spilled: AtomicU64,
    pub current_memory_bytes: AtomicU64,
    pub current_entry_count: AtomicU64,
    pub disk_bytes_used: AtomicU64,
    /// Number of entries spilled to SQLite.
    pub entries_spilled_sqlite: AtomicU64,
    /// Number of entries drained from SQLite.
    pub entries_drained_sqlite: AtomicU64,
}

/// The edge buffer implementation.
pub struct EdgeBuffer {
    config: EdgeBufferConfig,
    /// In-memory ring buffer.
    buffer: RwLock<VecDeque<BufferEntry>>,
    /// Current memory usage in bytes.
    memory_usage: AtomicU64,
    /// Sequence number counter.
    sequence_counter: AtomicU64,
    /// Buffer metrics.
    metrics: BufferMetrics,
    /// Whether the buffer is running.
    running: AtomicBool,
    /// Optional SQLite connection for disk spillover.
    ///
    /// Wrapped in `std::sync::Mutex` because `rusqlite::Connection` is `Send`
    /// but not `Sync`. The mutex is never held across `.await` points.
    db: Option<Mutex<rusqlite::Connection>>,
}

// ---------------------------------------------------------------------------
// SQLite helpers (free functions)
// ---------------------------------------------------------------------------

/// Initialize a SQLite database for edge buffer persistence.
///
/// Creates the database file (and parent directories), the `buffer` table,
/// enables WAL journal mode, and creates a partial index for efficient
/// pending-entry queries.
fn init_sqlite(path: &Path) -> Result<rusqlite::Connection> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)
            .with_context(|| format!("failed to create directory: {}", parent.display()))?;
    }

    let conn = rusqlite::Connection::open(path)
        .with_context(|| format!("failed to open SQLite database: {}", path.display()))?;

    // WAL mode — better concurrent read performance, crash-safe.
    conn.execute_batch("PRAGMA journal_mode=WAL").context("failed to enable WAL mode")?;

    conn.execute_batch("PRAGMA foreign_keys=ON").context("failed to enable foreign keys")?;

    conn.execute_batch(
        "CREATE TABLE IF NOT EXISTS buffer (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            data_type TEXT NOT NULL,
            data BLOB NOT NULL,
            priority INTEGER NOT NULL DEFAULT 2,
            retry_count INTEGER NOT NULL DEFAULT 0,
            created_at INTEGER NOT NULL,
            sent_at INTEGER
        )",
    )
    .context("failed to create buffer table")?;

    // Partial index: only un-sent rows, ordered by (priority, created_at).
    conn.execute_batch(
        "CREATE INDEX IF NOT EXISTS idx_pending
         ON buffer(priority ASC, created_at ASC)
         WHERE sent_at IS NULL",
    )
    .context("failed to create pending index")?;

    info!("SQLite edge buffer initialized at {}", path.display());
    Ok(conn)
}

/// Returns the current wall-clock time as seconds since the Unix epoch.
fn now_epoch_secs() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system clock is before UNIX epoch; this cannot happen after 1970")
        .as_secs() as i64
}

// ---------------------------------------------------------------------------
// EdgeBuffer implementation
// ---------------------------------------------------------------------------

impl EdgeBuffer {
    /// Create a new edge buffer.
    ///
    /// If `config.sqlite_path` is `Some`, a SQLite database is opened for
    /// disk spillover. Falls back to memory-only mode on initialization failure.
    pub fn new(config: EdgeBufferConfig) -> Self {
        let db = config.sqlite_path.as_ref().and_then(|path| match init_sqlite(Path::new(path)) {
            Ok(conn) => Some(Mutex::new(conn)),
            Err(e) => {
                error!(
                    "Failed to initialize SQLite edge buffer at '{}': {}; \
                         falling back to memory-only mode",
                    path, e
                );
                None
            }
        });

        Self {
            config,
            buffer: RwLock::new(VecDeque::new()),
            memory_usage: AtomicU64::new(0),
            sequence_counter: AtomicU64::new(1),
            metrics: BufferMetrics::default(),
            running: AtomicBool::new(true),
            db,
        }
    }

    /// Write data to the buffer.
    ///
    /// Returns the assigned sequence number. When the memory buffer is full
    /// and SQLite is configured, the entry is spilled to disk instead of
    /// evicting an in-memory entry.
    pub async fn write(&self, data: Vec<u8>, data_type: DataType) -> Result<u64> {
        let seq = self.sequence_counter.fetch_add(1, Ordering::SeqCst);
        let entry = BufferEntry {
            sequence_number: seq,
            created_at: Instant::now(),
            data,
            data_type,
            sqlite_row_id: None,
        };

        let entry_size = entry.data.len();

        // Evict expired entries from memory (and SQLite if configured).
        self.evict_expired().await;

        // Determine whether the memory buffer has room.
        let memory_full = self.memory_usage.load(Ordering::Relaxed) + entry_size as u64
            > self.config.max_memory_bytes as u64;

        if memory_full {
            // Try SQLite spillover first.
            if let Some(ref db_mutex) = self.db {
                match self.spill_to_sqlite(db_mutex, &entry) {
                    Ok(()) => {
                        self.metrics.entries_spilled_sqlite.fetch_add(1, Ordering::Relaxed);
                        self.metrics.entries_written.fetch_add(1, Ordering::Relaxed);
                        debug!("Spilled entry {} to SQLite ({} bytes)", seq, entry_size);
                        return Ok(seq);
                    }
                    Err(e) => {
                        warn!(
                            "SQLite spill failed for entry {}, \
                             falling back to memory eviction: {}",
                            seq, e
                        );
                    }
                }
            }

            // Evict oldest memory entries to make room.
            while self.memory_usage.load(Ordering::Relaxed) + entry_size as u64
                > self.config.max_memory_bytes as u64
            {
                if !self.evict_oldest().await {
                    break;
                }
            }
        }

        // Write to memory buffer.
        let mut buffer = self.buffer.write().await;
        buffer.push_back(entry);
        drop(buffer);

        self.memory_usage.fetch_add(entry_size as u64, Ordering::Relaxed);
        self.metrics.entries_written.fetch_add(1, Ordering::Relaxed);
        self.metrics
            .current_memory_bytes
            .store(self.memory_usage.load(Ordering::Relaxed), Ordering::Relaxed);
        self.metrics.current_entry_count.fetch_add(1, Ordering::Relaxed);

        debug!(
            "Buffered entry {} ({} bytes, total: {} entries, {} bytes)",
            seq,
            entry_size,
            self.metrics.current_entry_count.load(Ordering::Relaxed),
            self.memory_usage.load(Ordering::Relaxed)
        );

        Ok(seq)
    }

    /// Read and remove entries from the memory buffer (FIFO order).
    ///
    /// Returns up to `count` entries. Entries are removed from the buffer.
    pub async fn read(&self, count: usize) -> Vec<BufferEntry> {
        let mut buffer = self.buffer.write().await;
        let drain_count = count.min(buffer.len());
        let entries: Vec<BufferEntry> = buffer.drain(..drain_count).collect();

        let total_size: usize = entries.iter().map(|e| e.data.len()).sum();
        self.memory_usage.fetch_sub(total_size as u64, Ordering::Relaxed);
        self.metrics.entries_read.fetch_add(entries.len() as u64, Ordering::Relaxed);
        self.metrics
            .current_memory_bytes
            .store(self.memory_usage.load(Ordering::Relaxed), Ordering::Relaxed);
        self.metrics.current_entry_count.fetch_sub(entries.len() as u64, Ordering::Relaxed);

        entries
    }

    /// Peek at entries without removing them (for inspection).
    pub async fn peek(&self, count: usize) -> Vec<BufferEntry> {
        let buffer = self.buffer.read().await;
        buffer.iter().take(count).cloned().collect()
    }

    /// Get the current number of entries in the memory buffer.
    pub async fn len(&self) -> usize {
        self.buffer.read().await.len()
    }

    /// Check if the memory buffer is empty.
    pub async fn is_empty(&self) -> bool {
        self.buffer.read().await.is_empty()
    }

    /// Get current memory usage in bytes.
    pub fn memory_usage(&self) -> u64 {
        self.memory_usage.load(Ordering::Relaxed)
    }

    /// Get the estimated size of persistent (SQLite) backlog in bytes.
    ///
    /// Returns the total size of data BLOBs for all pending (un-sent) entries
    /// in the SQLite buffer. Returns 0 when SQLite is not configured.
    pub fn persistent_size(&self) -> u64 {
        let db = match self.db {
            Some(ref db) => db,
            None => return 0,
        };
        let conn = match db.lock() {
            Ok(c) => c,
            Err(_) => {
                warn!("SQLite mutex is poisoned; this is a bug — returning 0 for persistent_size");
                return 0;
            }
        };
        match conn.query_row(
            "SELECT COALESCE(SUM(LENGTH(data)), 0) FROM buffer WHERE sent_at IS NULL",
            [],
            |row| row.get::<_, i64>(0),
        ) {
            Ok(total) => total as u64,
            Err(e) => {
                warn!("Failed to query persistent_size: {}", e);
                0
            }
        }
    }

    /// Get the timestamp (epoch seconds) of the oldest un-sent backlog entry.
    ///
    /// Returns the `created_at` value of the oldest pending entry in SQLite.
    /// Returns 0 when SQLite is not configured or there are no pending entries.
    pub fn oldest_entry_timestamp(&self) -> i64 {
        let db = match self.db {
            Some(ref db) => db,
            None => return 0,
        };
        let conn = match db.lock() {
            Ok(c) => c,
            Err(_) => {
                warn!(
                    "SQLite mutex is poisoned; this is a bug — returning 0 for oldest_entry_timestamp"
                );
                return 0;
            }
        };
        match conn.query_row(
            "SELECT COALESCE(MIN(created_at), 0) FROM buffer WHERE sent_at IS NULL",
            [],
            |row| row.get::<_, i64>(0),
        ) {
            Ok(ts) => ts,
            Err(e) => {
                warn!("Failed to query oldest_entry_timestamp: {}", e);
                0
            }
        }
    }

    /// Delete all backlog entries from the persistent SQLite buffer.
    ///
    /// Used when the cluster issues a `DeleteBacklog` command.
    /// This is a destructive operation that removes all entries
    /// (both sent and pending) from the SQLite buffer table.
    /// In-memory entries are also drained.
    pub fn delete_all_backlog(&self) {
        // Drain in-memory buffer.
        let drained = {
            let buffer = self.buffer.try_write();
            match buffer {
                Ok(mut buf) => {
                    let count = buf.len();
                    buf.clear();
                    count
                }
                Err(_) => 0,
            }
        };
        if drained > 0 {
            self.memory_usage.store(0, Ordering::Relaxed);
            self.metrics.current_entry_count.store(0, Ordering::Relaxed);
            info!(drained_entries = drained, "In-memory backlog drained");
        }

        // Delete all entries from SQLite.
        if let Some(ref db_mutex) = self.db {
            match db_mutex.lock() {
                Ok(conn) => match conn.execute("DELETE FROM buffer", []) {
                    Ok(deleted) => {
                        info!(deleted_sqlite_rows = deleted, "SQLite backlog deleted permanently");
                    }
                    Err(e) => {
                        warn!("Failed to delete SQLite backlog: {}", e);
                    }
                },
                Err(_) => {
                    warn!("SQLite mutex is poisoned; cannot delete backlog");
                }
            }
        }
    }

    /// Get buffer metrics.
    pub fn metrics(&self) -> &BufferMetrics {
        &self.metrics
    }

    /// Drain all entries from the buffer for replay.
    ///
    /// Drains the in-memory buffer first (FIFO), then pulls pending entries
    /// from SQLite (ordered by priority ASC, created_at ASC) if configured.
    /// SQLite entries retain their `sqlite_row_id` for later acknowledgement
    /// via `mark_sent()`.
    pub async fn drain(&self) -> Vec<BufferEntry> {
        // Drain memory buffer.
        let mut entries = {
            let mut buffer = self.buffer.write().await;
            let entries: Vec<BufferEntry> = buffer.drain(..).collect();

            let total_size: usize = entries.iter().map(|e| e.data.len()).sum();
            self.memory_usage.fetch_sub(total_size as u64, Ordering::Relaxed);
            self.metrics.entries_read.fetch_add(entries.len() as u64, Ordering::Relaxed);
            self.metrics.current_memory_bytes.store(0, Ordering::Relaxed);
            self.metrics.current_entry_count.store(0, Ordering::Relaxed);
            entries
        };
        // RwLock released here.

        // Drain pending SQLite entries.
        if let Some(ref db_mutex) = self.db {
            match self.drain_sqlite_pending(db_mutex) {
                Ok(sqlite_entries) => {
                    let sqlite_count = sqlite_entries.len() as u64;
                    entries.extend(sqlite_entries);
                    self.metrics.entries_drained_sqlite.fetch_add(sqlite_count, Ordering::Relaxed);
                }
                Err(e) => {
                    warn!("Failed to drain SQLite entries: {}", e);
                }
            }
        }

        entries
    }

    /// Drain entries of a specific type from the memory buffer.
    ///
    /// Returns entries matching the given data type and removes them from
    /// the buffer. Non-matching entries are preserved.
    pub async fn drain_by_type(&self, data_type: DataType) -> Vec<BufferEntry> {
        let mut buffer = self.buffer.write().await;
        let mut entries = Vec::new();
        let mut retained = VecDeque::new();

        while let Some(entry) = buffer.pop_front() {
            if entry.data_type == data_type {
                entries.push(entry);
            } else {
                retained.push_back(entry);
            }
        }

        *buffer = retained;

        let drained_size: usize = entries.iter().map(|e| e.data.len()).sum();
        self.memory_usage.fetch_sub(drained_size as u64, Ordering::Relaxed);
        self.metrics.entries_read.fetch_add(entries.len() as u64, Ordering::Relaxed);
        self.metrics.current_entry_count.fetch_sub(entries.len() as u64, Ordering::Relaxed);

        entries
    }

    /// Mark a SQLite entry as successfully sent.
    ///
    /// Sets the `sent_at` timestamp so the entry is excluded from future drains.
    /// Call this after the entry has been transmitted to the cluster.
    pub fn mark_sent(&self, row_id: i64) -> Result<()> {
        let db = self.db.as_ref().context("SQLite not initialized; cannot mark sent")?;
        let conn = db.lock().expect("SQLite mutex is poisoned; this is a bug");
        let now = now_epoch_secs();
        let updated = conn.execute(
            "UPDATE buffer SET sent_at = ?1 WHERE id = ?2",
            rusqlite::params![now, row_id],
        )?;
        if updated == 0 {
            warn!("mark_sent: no row found with id {}", row_id);
        }
        Ok(())
    }

    /// Increment the retry count for a SQLite entry.
    ///
    /// Call this when a send attempt fails. Entries with excessive retries
    /// can be identified for manual inspection or cleanup.
    pub fn mark_retry(&self, row_id: i64) -> Result<()> {
        let db = self.db.as_ref().context("SQLite not initialized; cannot mark retry")?;
        let conn = db.lock().expect("SQLite mutex is poisoned; this is a bug");
        let updated = conn.execute(
            "UPDATE buffer SET retry_count = retry_count + 1 WHERE id = ?1",
            rusqlite::params![row_id],
        )?;
        if updated == 0 {
            warn!("mark_retry: no row found with id {}", row_id);
        }
        Ok(())
    }

    /// Get the number of pending (un-sent) entries in SQLite.
    ///
    /// Returns `0` when SQLite is not configured.
    pub fn sqlite_pending_count(&self) -> Result<usize> {
        let db = match self.db {
            Some(ref db) => db,
            None => return Ok(0),
        };
        let conn = db.lock().expect("SQLite mutex is poisoned; this is a bug");
        let count: i64 =
            conn.query_row("SELECT COUNT(*) FROM buffer WHERE sent_at IS NULL", [], |row| {
                row.get(0)
            })?;
        Ok(count as usize)
    }

    /// Shutdown the buffer.
    pub fn shutdown(&self) {
        self.running.store(false, Ordering::Release);
    }

    // -----------------------------------------------------------------------
    // Private helpers
    // -----------------------------------------------------------------------

    /// Evict the oldest entry from the memory buffer.
    async fn evict_oldest(&self) -> bool {
        let mut buffer = self.buffer.write().await;
        if let Some(entry) = buffer.pop_front() {
            let size = entry.data.len() as u64;
            self.memory_usage.fetch_sub(size, Ordering::Relaxed);
            self.metrics.entries_evicted.fetch_add(1, Ordering::Relaxed);
            self.metrics.current_entry_count.fetch_sub(1, Ordering::Relaxed);
            true
        } else {
            false
        }
    }

    /// Evict entries that have exceeded their TTL from memory and SQLite.
    async fn evict_expired(&self) {
        // Memory eviction (holds RwLock only within this block).
        {
            let now = Instant::now();
            let mut buffer = self.buffer.write().await;

            while let Some(entry) = buffer.front() {
                if now.duration_since(entry.created_at) > self.config.entry_ttl {
                    if let Some(entry) = buffer.pop_front() {
                        let size = entry.data.len() as u64;
                        self.memory_usage.fetch_sub(size, Ordering::Relaxed);
                        self.metrics.entries_evicted.fetch_add(1, Ordering::Relaxed);
                        self.metrics.current_entry_count.fetch_sub(1, Ordering::Relaxed);
                    }
                } else {
                    break; // Entries are in FIFO order, so we can stop.
                }
            }
        }
        // RwLock released.

        // SQLite eviction.
        self.evict_expired_sqlite();
    }

    /// Delete expired entries from the SQLite buffer table.
    fn evict_expired_sqlite(&self) {
        if let Some(ref db_mutex) = self.db {
            // Lock is not held across any .await (synchronous operation).
            if let Ok(conn) = db_mutex.lock() {
                let cutoff =
                    now_epoch_secs().saturating_sub(self.config.entry_ttl.as_secs() as i64);
                match conn
                    .execute("DELETE FROM buffer WHERE created_at < ?1", rusqlite::params![cutoff])
                {
                    Ok(deleted) if deleted > 0 => {
                        debug!("Evicted {} expired entries from SQLite", deleted);
                        self.metrics.entries_evicted.fetch_add(deleted as u64, Ordering::Relaxed);
                    }
                    Ok(_) => {}
                    Err(e) => {
                        warn!("Failed to evict expired SQLite entries: {}", e);
                    }
                }
            }
        }
    }

    /// Spill a single entry to the SQLite database.
    ///
    /// Serializes the entry payload with bincode and inserts it into the
    /// `buffer` table. This is a synchronous operation; the mutex is never
    /// held across an `.await` point.
    fn spill_to_sqlite(
        &self,
        db_mutex: &Mutex<rusqlite::Connection>,
        entry: &BufferEntry,
    ) -> Result<()> {
        let payload =
            SqlitePayload { sequence_number: entry.sequence_number, data: entry.data.clone() };
        let blob = bincode::serialize(&payload).context("failed to serialize entry for SQLite")?;

        // SAFETY: Mutex is acquired and released within this synchronous block;
        // no .await occurs while the guard is alive.
        let conn = db_mutex.lock().expect("SQLite mutex is poisoned; this is a bug");
        conn.execute(
            "INSERT INTO buffer (data_type, data, priority, created_at) \
             VALUES (?1, ?2, 2, ?3)",
            rusqlite::params![entry.data_type.as_str(), blob, now_epoch_secs()],
        )
        .context("failed to insert into SQLite buffer")?;

        Ok(())
    }

    /// Drain pending (un-sent) entries from SQLite.
    ///
    /// Returns up to [`SQLITE_DRAIN_BATCH_SIZE`] entries ordered by
    /// priority ASC, created_at ASC. Entries are **not** removed or marked;
    /// the caller must use `mark_sent()` / `mark_retry()` for acknowledgement.
    fn drain_sqlite_pending(
        &self,
        db_mutex: &Mutex<rusqlite::Connection>,
    ) -> Result<Vec<BufferEntry>> {
        // SAFETY: Mutex is acquired and released within this synchronous block;
        // no .await occurs while the guard is alive.
        let conn = db_mutex.lock().expect("SQLite mutex is poisoned; this is a bug");

        let raw_entries = {
            let mut stmt = conn
                .prepare(
                    "SELECT id, data_type, data, created_at \
                     FROM buffer \
                     WHERE sent_at IS NULL \
                     ORDER BY priority ASC, created_at ASC \
                     LIMIT ?1",
                )
                .context("failed to prepare SQLite drain statement")?;

            // Break into named bindings to satisfy the borrow checker:
            // `rows` borrows `stmt`; `.collect()` consumes `rows` before the
            // block ends so `stmt` can be dropped safely.
            let rows = stmt
                .query_map(rusqlite::params![SQLITE_DRAIN_BATCH_SIZE as i64], |row| {
                    let row_id: i64 = row.get(0)?;
                    let data_type_str: String = row.get(1)?;
                    let blob: Vec<u8> = row.get(2)?;
                    let _created_at: i64 = row.get(3)?;
                    Ok((row_id, data_type_str, blob))
                })
                .context("failed to query SQLite pending entries")?;

            rows.collect::<std::result::Result<Vec<_>, _>>()
                .context("failed to read SQLite rows")?
        };

        let mut result = Vec::with_capacity(raw_entries.len());
        for (row_id, data_type_str, blob) in raw_entries {
            let payload: SqlitePayload =
                bincode::deserialize(&blob).context("failed to deserialize SQLite entry")?;
            let data_type = match DataType::from_str(&data_type_str) {
                Some(dt) => dt,
                None => {
                    warn!(
                        "Unknown data type '{}' in SQLite row {}, defaulting to Metrics",
                        data_type_str, row_id
                    );
                    DataType::Metrics
                }
            };

            result.push(BufferEntry {
                sequence_number: payload.sequence_number,
                created_at: Instant::now(),
                data: payload.data,
                data_type,
                sqlite_row_id: Some(row_id),
            });
        }

        Ok(result)
    }
}

/// Clean shutdown: checkpoint the WAL and close the SQLite connection.
impl Drop for EdgeBuffer {
    fn drop(&mut self) {
        if let Some(ref db) = self.db {
            if let Ok(conn) = db.lock() {
                // Checkpoint WAL for clean shutdown — errors are non-fatal.
                let _ = conn.execute_batch("PRAGMA wal_checkpoint(TRUNCATE)");
                info!("SQLite edge buffer connection closed");
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    /// RAII helper that removes a temporary directory on drop.
    struct TestDir(PathBuf);

    impl TestDir {
        fn new(name: &str) -> Self {
            let path = std::env::temp_dir().join(format!(
                "paryty-edge-buffer-test-{}-{}",
                name,
                fastrand::u64(..)
            ));
            Self(path)
        }
    }

    impl Drop for TestDir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    // ---- Test 1: SQLite initialization ------------------------------------

    #[test]
    fn test_sqlite_init() {
        let dir = TestDir::new("init");
        let db_path = dir.0.join("test.db");

        let conn = init_sqlite(&db_path).expect("SQLite init should succeed");

        // Verify buffer table exists.
        let table_name: String = conn
            .query_row(
                "SELECT name FROM sqlite_master WHERE type='table' AND name='buffer'",
                [],
                |row| row.get(0),
            )
            .expect("buffer table should exist");
        assert_eq!(table_name, "buffer");

        // Verify WAL journal mode.
        let journal_mode: String = conn
            .query_row("PRAGMA journal_mode", [], |row| row.get(0))
            .expect("should read journal_mode");
        assert_eq!(journal_mode, "wal");

        // Verify partial index exists.
        let index_name: String = conn
            .query_row(
                "SELECT name FROM sqlite_master WHERE type='index' AND name='idx_pending'",
                [],
                |row| row.get(0),
            )
            .expect("idx_pending index should exist");
        assert_eq!(index_name, "idx_pending");

        // Verify a basic insert/select round-trip.
        conn.execute(
            "INSERT INTO buffer (data_type, data, priority, created_at) \
             VALUES ('metrics', X'DEAD', 2, 1000)",
            [],
        )
        .expect("should insert test row");

        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM buffer", [], |row| row.get(0))
            .expect("should count rows");
        assert_eq!(count, 1);
    }

    // ---- Test 2: Memory spillover to SQLite -------------------------------

    #[tokio::test]
    async fn test_memory_spillover() {
        let dir = TestDir::new("spillover");
        let db_path = dir.0.join("spillover.db");

        // Very small memory cap (40 bytes) so most writes spill to SQLite.
        let config = EdgeBufferConfig {
            max_memory_bytes: 40,
            max_entries: 100_000,
            entry_ttl: Duration::from_secs(3600),
            disk_spill_dir: dir.0.clone(),
            disk_spill_enabled: true,
            max_disk_bytes: 1024 * 1024,
            sqlite_path: Some(db_path.to_string_lossy().to_string()),
        };

        let buffer = EdgeBuffer::new(config);

        // 25 bytes each; with max_memory=40, entry 1 fits, entries 2-6 spill.
        let data = vec![0u8; 25];
        for i in 0u8..6 {
            let dt = if i % 2 == 0 { DataType::Metrics } else { DataType::Events };
            buffer.write(data.clone(), dt).await.expect("write should succeed");
        }

        let entries = buffer.drain().await;
        assert!(!entries.is_empty(), "drain should return entries");

        // Sequence numbers should be in ascending order (FIFO).
        let seqs: Vec<u64> = entries.iter().map(|e| e.sequence_number).collect();
        let mut sorted_seqs = seqs.clone();
        sorted_seqs.sort();
        assert_eq!(seqs, sorted_seqs, "entries should be in sequence order");

        // SQLite entries should carry valid row IDs.
        for entry in entries.iter().filter(|e| e.sqlite_row_id.is_some()) {
            assert!(entry.sqlite_row_id.unwrap() > 0);
        }
    }

    // ---- Test 3: TTL eviction for SQLite rows -----------------------------

    #[tokio::test]
    async fn test_evict_expired_sqlite() {
        let dir = TestDir::new("evict");
        let db_path = dir.0.join("evict.db");

        let config = EdgeBufferConfig {
            max_memory_bytes: 1024,
            max_entries: 100_000,
            entry_ttl: Duration::from_secs(1), // 1-second TTL
            disk_spill_dir: dir.0.clone(),
            disk_spill_enabled: true,
            max_disk_bytes: 1024 * 1024,
            sqlite_path: Some(db_path.to_string_lossy().to_string()),
        };

        let buffer = EdgeBuffer::new(config);

        // Insert a stale entry directly into SQLite (1 hour old).
        {
            let db = buffer.db.as_ref().expect("SQLite should be initialized");
            let conn = db.lock().expect("lock");
            let old_ts = now_epoch_secs() - 3600;
            conn.execute(
                "INSERT INTO buffer (data_type, data, priority, created_at) \
                 VALUES ('metrics', X'AABB', 2, ?1)",
                rusqlite::params![old_ts],
            )
            .expect("insert old entry");
        }

        // Confirm the stale entry exists.
        let count = buffer.sqlite_pending_count().expect("count");
        assert_eq!(count, 1, "should have 1 pending entry before eviction");

        // A normal write triggers evict_expired().
        buffer.write(vec![1, 2, 3], DataType::Metrics).await.expect("write should succeed");

        // The stale entry should now be gone.
        let count = buffer.sqlite_pending_count().expect("count");
        assert_eq!(count, 0, "expired SQLite entry should have been evicted");

        assert!(
            buffer.metrics().entries_evicted.load(Ordering::Relaxed) > 0,
            "eviction metric should be incremented"
        );
    }

    // ---- Test 4: mark_sent / mark_retry -----------------------------------

    #[tokio::test]
    async fn test_mark_sent_and_retry() {
        let dir = TestDir::new("mark");
        let db_path = dir.0.join("mark.db");

        let config = EdgeBufferConfig {
            max_memory_bytes: 10, // Force everything to SQLite.
            max_entries: 100_000,
            entry_ttl: Duration::from_secs(3600),
            disk_spill_dir: dir.0.clone(),
            disk_spill_enabled: true,
            max_disk_bytes: 1024 * 1024,
            sqlite_path: Some(db_path.to_string_lossy().to_string()),
        };

        let buffer = EdgeBuffer::new(config);

        // 11 bytes → exceeds 10-byte memory cap → spills to SQLite.
        buffer
            .write(vec![1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11], DataType::Metrics)
            .await
            .expect("write should succeed");

        // Drain to obtain the SQLite row ID.
        let entries = buffer.drain().await;
        assert!(!entries.is_empty(), "should have entries");
        let sqlite_entry = entries
            .iter()
            .find(|e| e.sqlite_row_id.is_some())
            .expect("should have at least one SQLite entry");
        let row_id = sqlite_entry.sqlite_row_id.unwrap();

        // Before mark_sent the entry is still pending.
        let pending = buffer.sqlite_pending_count().expect("pending count");
        assert_eq!(pending, 1, "entry should be pending before mark_sent");

        // Mark retry (increments counter).
        buffer.mark_retry(row_id).expect("mark_retry should succeed");

        // Mark sent (sets sent_at).
        buffer.mark_sent(row_id).expect("mark_sent should succeed");

        // After mark_sent the entry is no longer pending.
        let pending = buffer.sqlite_pending_count().expect("pending count");
        assert_eq!(pending, 0, "sent entry should not be pending");
    }

    // ---- Test 5: Backward-compatible memory-only mode ---------------------

    #[tokio::test]
    async fn test_memory_only_mode() {
        // No sqlite_path — pure in-memory mode.
        let config = EdgeBufferConfig::default();
        let buffer = EdgeBuffer::new(config);

        buffer.write(vec![1, 2, 3], DataType::Metrics).await.expect("write should succeed");

        let entries = buffer.drain().await;
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].data, vec![1, 2, 3]);
        assert!(entries[0].sqlite_row_id.is_none());

        // sqlite_pending_count returns 0 when SQLite is not configured.
        assert_eq!(buffer.sqlite_pending_count().expect("ok"), 0);
    }

    // ---- Test 6: persistent_size and oldest_entry_timestamp ---------------

    #[tokio::test]
    async fn test_persistent_size() {
        let dir = TestDir::new("persistent");
        let db_path = dir.0.join("persistent.db");

        let config = EdgeBufferConfig {
            max_memory_bytes: 10,
            max_entries: 100_000,
            entry_ttl: Duration::from_secs(3600),
            disk_spill_dir: dir.0.clone(),
            disk_spill_enabled: true,
            max_disk_bytes: 1024 * 1024,
            sqlite_path: Some(db_path.to_string_lossy().to_string()),
        };

        let buffer = EdgeBuffer::new(config);

        // persistent_size returns 0 when no entries.
        assert_eq!(buffer.persistent_size(), 0);
        assert_eq!(buffer.oldest_entry_timestamp(), 0);

        // Write an entry that spills to SQLite (11 bytes > 10 byte cap).
        buffer
            .write(vec![1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11], DataType::Metrics)
            .await
            .expect("write should succeed");

        let size = buffer.persistent_size();
        assert!(size > 0, "persistent_size should be >0 after SQLite spill");

        let ts = buffer.oldest_entry_timestamp();
        assert!(ts > 0, "oldest_entry_timestamp should be >0");
    }

    // ---- Test 7: delete_all_backlog ---------------------------------------

    #[tokio::test]
    async fn test_delete_all_backlog() {
        let dir = TestDir::new("delete_backlog");
        let db_path = dir.0.join("delete.db");

        let config = EdgeBufferConfig {
            max_memory_bytes: 10,
            max_entries: 100_000,
            entry_ttl: Duration::from_secs(3600),
            disk_spill_dir: dir.0.clone(),
            disk_spill_enabled: true,
            max_disk_bytes: 1024 * 1024,
            sqlite_path: Some(db_path.to_string_lossy().to_string()),
        };

        let buffer = EdgeBuffer::new(config);

        // Write entries that spill to SQLite.
        for _ in 0..5 {
            buffer
                .write(vec![1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11], DataType::Metrics)
                .await
                .expect("write should succeed");
        }

        // Entries should exist in SQLite.
        let pending = buffer.sqlite_pending_count().expect("pending count");
        assert!(pending > 0, "should have pending entries before delete");

        // Delete all backlog.
        buffer.delete_all_backlog();

        // All entries should be gone.
        let pending = buffer.sqlite_pending_count().expect("pending count");
        assert_eq!(pending, 0, "all entries should be deleted");

        assert_eq!(buffer.persistent_size(), 0);
        assert_eq!(buffer.oldest_entry_timestamp(), 0);
    }
}
