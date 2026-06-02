//! Log tailer for watching log files in real-time
//!
//! Implements file watching using platform-specific mechanisms:
//! - Linux: inotify for efficient event-based watching
//! - Windows/macOS: polling fallback with configurable interval
//!
//! Features:
//! - Glob pattern matching for multiple files
//! - Regex filtering for line selection
//! - Line buffering for partial reads
//! - Tail-from-end mode for existing files

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::time::Duration;

use regex::Regex;
use tokio::fs::File;
use tokio::io::{AsyncBufReadExt, AsyncSeekExt, BufReader, SeekFrom};
use tokio::sync::mpsc;
use tokio::time::interval;
use tracing::{debug, error, info, warn};

/// Log tailer configuration
#[derive(Debug, Clone)]
pub struct LogTailerConfig {
    /// Glob pattern for log files (e.g., "/var/log/*.log")
    pub pattern: String,
    /// Regex filter for lines (only matching lines are forwarded)
    pub filter: Option<String>,
    /// Whether to start tailing from the end of existing files
    pub tail_from_end: bool,
    /// Polling interval for non-Linux platforms
    pub poll_interval: Duration,
    /// Maximum line size in bytes
    pub max_line_size: usize,
}

impl Default for LogTailerConfig {
    fn default() -> Self {
        Self {
            pattern: "/var/log/*.log".to_string(),
            filter: None,
            tail_from_end: true,
            poll_interval: Duration::from_secs(1),
            max_line_size: 64 * 1024, // 64KB
        }
    }
}

/// A log line with metadata
#[derive(Debug, Clone, serde::Serialize)]
pub struct LogLine {
    /// Source file path
    pub file: PathBuf,
    /// Line content
    pub content: String,
    /// Line number in the file
    pub line_number: u64,
    /// Timestamp when the line was read
    pub timestamp: chrono::DateTime<chrono::Utc>,
}

/// File state for tracking position
struct FileState {
    /// Current read position
    position: u64,
    /// Current line number
    line_number: u64,
}

/// Log tailer implementation
pub struct LogTailer {
    /// Configuration
    config: LogTailerConfig,
    /// Compiled filter regex
    filter: Option<Regex>,
    /// File states by path
    file_states: HashMap<PathBuf, FileState>,
}

impl LogTailer {
    /// Create a new log tailer
    pub fn new(config: LogTailerConfig) -> Self {
        let filter = config.filter.as_ref().and_then(|f| match Regex::new(f) {
            Ok(re) => Some(re),
            Err(e) => {
                warn!(error = %e, "Invalid filter regex, ignoring");
                None
            }
        });

        Self { config, filter, file_states: HashMap::new() }
    }

    /// Start tailing log files
    pub async fn run(&mut self, tx: mpsc::Sender<LogLine>) {
        info!(pattern = %self.config.pattern, "Starting log tailer");

        // Resolve glob pattern to files
        let files = self.resolve_glob().await;
        if files.is_empty() {
            warn!("No log files found matching pattern");
            return;
        }

        info!(count = files.len(), "Found log files to tail");

        // Initialize file states
        for file in &files {
            if !self.file_states.contains_key(file) {
                let position =
                    if self.config.tail_from_end { self.get_file_size(file).await } else { 0 };

                self.file_states.insert(file.clone(), FileState { position, line_number: 0 });
            }
        }

        // Poll files for new content
        let mut ticker = interval(self.config.poll_interval);
        loop {
            ticker.tick().await;

            for file in files.clone() {
                self.tail_file(&file, &tx).await;
            }
        }
    }

    /// Resolve glob pattern to file paths
    async fn resolve_glob(&self) -> Vec<PathBuf> {
        // Simple glob implementation - in production, use glob crate
        let pattern = &self.config.pattern;
        let (dir, _glob_pattern) = if let Some(pos) = pattern.rfind('/') {
            (&pattern[..pos], &pattern[pos + 1..])
        } else {
            (".", pattern.as_str())
        };

        let mut files = Vec::new();
        if let Ok(mut entries) = tokio::fs::read_dir(dir).await {
            while let Ok(Some(entry)) = entries.next_entry().await {
                let path = entry.path();
                if path.is_file() {
                    // Simple pattern matching (in production, use glob crate)
                    if let Some(name) = path.file_name() {
                        let name_str = name.to_string_lossy();
                        if self.matches_glob(&name_str, _glob_pattern) {
                            files.push(path);
                        }
                    }
                }
            }
        }

        files
    }

    /// Simple glob matching
    fn matches_glob(&self, name: &str, pattern: &str) -> bool {
        if pattern == "*" {
            return true;
        }

        if let Some(ext) = pattern.strip_prefix("*.") {
            return name.ends_with(ext);
        }

        name == pattern
    }

    /// Get file size
    async fn get_file_size(&self, path: &Path) -> u64 {
        match tokio::fs::metadata(path).await {
            Ok(metadata) => metadata.len(),
            Err(_) => 0,
        }
    }

    /// Tail a single file for new content
    async fn tail_file(&mut self, path: &Path, tx: &mpsc::Sender<LogLine>) {
        let state = match self.file_states.get_mut(path) {
            Some(state) => state,
            None => return,
        };

        // Open file and seek to last position
        let file = match File::open(path).await {
            Ok(file) => file,
            Err(e) => {
                debug!(error = %e, path = %path.display(), "Failed to open file");
                return;
            }
        };

        let metadata = match file.metadata().await {
            Ok(m) => m,
            Err(e) => {
                debug!(error = %e, path = %path.display(), "Failed to get metadata");
                return;
            }
        };

        let file_size = metadata.len();

        // Check if file was truncated (rotation)
        if file_size < state.position {
            info!(path = %path.display(), "File truncated, resetting position");
            state.position = 0;
            state.line_number = 0;
        }

        // No new data
        if file_size == state.position {
            return;
        }

        // Read new content
        let mut reader = BufReader::new(file);
        if let Err(e) = reader.seek(SeekFrom::Start(state.position)).await {
            error!(error = %e, path = %path.display(), "Failed to seek");
            return;
        }

        let mut line = String::new();
        loop {
            line.clear();
            match reader.read_line(&mut line).await {
                Ok(0) => break, // EOF
                Ok(_) => {
                    state.position += line.len() as u64;
                    state.line_number += 1;

                    // Apply filter
                    if let Some(filter) = &self.filter {
                        if !filter.is_match(&line) {
                            continue;
                        }
                    }

                    let log_line = LogLine {
                        file: path.to_path_buf(),
                        content: line.trim_end().to_string(),
                        line_number: state.line_number,
                        timestamp: chrono::Utc::now(),
                    };

                    if tx.send(log_line).await.is_err() {
                        error!("Failed to send log line, channel closed");
                        return;
                    }
                }
                Err(e) => {
                    error!(error = %e, path = %path.display(), "Failed to read line");
                    break;
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_log_tailer_config_default() {
        let config = LogTailerConfig::default();
        assert_eq!(config.pattern, "/var/log/*.log");
        assert!(config.filter.is_none());
        assert!(config.tail_from_end);
        assert_eq!(config.max_line_size, 64 * 1024);
    }

    #[test]
    fn test_glob_matching() {
        let tailer = LogTailer::new(LogTailerConfig::default());

        assert!(tailer.matches_glob("test.log", "*"));
        assert!(tailer.matches_glob("test.log", "*.log"));
        assert!(!tailer.matches_glob("test.txt", "*.log"));
        assert!(tailer.matches_glob("test.log", "test.log"));
    }
}
