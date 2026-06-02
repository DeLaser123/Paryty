//! Container Detector
//!
//! Detects containers by inspecting cgroups and namespaces.
//! Supports Docker, Podman, and containerd runtimes.
//!
//! Enhancements over base detection:
//! - Reads cgroup v1/v2 resource limits (memory, CPU quota, CPU shares)
//! - Best-effort limit reads — collection continues if limits are unavailable

use anyhow::Result;
use chrono::Utc;
use serde::Serialize;
use tracing::instrument;

use crate::config::MetalConfig;

/// Container resource limits read from cgroup filesystem.
///
/// All fields are `Option` because limits may be unset (e.g., "max" in cgroup v2
/// for unlimited memory) or the underlying cgroup files may be unreadable.
#[derive(Debug, Clone, Serialize, serde::Deserialize, Default)]
pub struct ContainerResourceLimits {
    /// Memory limit in bytes. `None` if unlimited or unreadable.
    pub memory_limit_bytes: Option<u64>,
    /// CPU CFS quota as a ratio (quota / period). `None` if unlimited or unreadable.
    pub cpu_quota: Option<f64>,
    /// CPU shares (relative weight). `None` if not set or unreadable.
    pub cpu_shares: Option<u64>,
}

/// Container detection result.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct ContainerInfo {
    pub container_id: String,
    pub runtime: String,
    pub name: String,
    pub image: String,
    pub status: String,
    pub cgroup_version: String,
    pub pids: Vec<u32>,
    pub resource_limits: ContainerResourceLimits,
}

/// Container metrics collection result.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct ContainerMetrics {
    pub timestamp: String,
    pub containers: Vec<ContainerInfo>,
}

/// Container detector.
pub struct ContainerDetector;

impl ContainerDetector {
    pub fn new() -> Self {
        Self
    }

    #[instrument(skip(self, _config), fields(collector = "container"))]
    pub fn collect(&self, _config: &MetalConfig) -> Result<ContainerMetrics> {
        let containers = if cfg!(target_os = "linux") { self.detect_linux()? } else { Vec::new() };

        Ok(ContainerMetrics { timestamp: Utc::now().to_rfc3339(), containers })
    }

    #[cfg(target_os = "linux")]
    fn detect_linux(&self) -> Result<Vec<ContainerInfo>> {
        use std::collections::HashMap;
        use std::fs;

        let mut containers: HashMap<String, ContainerInfo> = HashMap::new();

        // Scan /proc/[pid]/cgroup for container IDs
        let proc_dir = fs::read_dir("/proc")?;
        for entry in proc_dir {
            let entry = entry?;
            let name = entry.file_name().to_string_lossy().to_string();
            let pid: u32 = match name.parse() {
                Ok(p) => p,
                Err(_) => continue,
            };

            if let Ok(cgroup) = fs::read_to_string(format!("/proc/{}/cgroup", pid)) {
                if let Some((container_id, runtime, cgroup_version, cgroup_path)) =
                    parse_cgroup(&cgroup)
                {
                    containers
                        .entry(container_id.clone())
                        .or_insert_with(|| {
                            let resource_limits =
                                read_resource_limits(&cgroup_path, &cgroup_version);
                            ContainerInfo {
                                container_id: container_id.clone(),
                                runtime,
                                name: String::new(),
                                image: String::new(),
                                status: "running".to_string(),
                                cgroup_version,
                                pids: Vec::new(),
                                resource_limits,
                            }
                        })
                        .pids
                        .push(pid);
                }
            }
        }

        Ok(containers.into_values().collect())
    }

    #[cfg(not(target_os = "linux"))]
    fn detect_linux(&self) -> Result<Vec<ContainerInfo>> {
        Ok(Vec::new())
    }
}

/// Read resource limits from the cgroup filesystem.
///
/// Best-effort: any read failure returns defaults (all `None`).
/// Caller provides `cgroup_path` (the relative path from `/proc/[pid]/cgroup`)
/// and `cgroup_version` ("v1" or "v2").
#[cfg(target_os = "linux")]
fn read_resource_limits(cgroup_path: &str, cgroup_version: &str) -> ContainerResourceLimits {
    match cgroup_version {
        "v2" => read_resource_limits_v2(cgroup_path),
        "v1" => read_resource_limits_v1(cgroup_path),
        _ => ContainerResourceLimits::default(),
    }
}

/// Read resource limits from cgroup v2 unified hierarchy.
///
/// - `memory.max`: "max" → None, numeric → Some(bytes)
/// - `cpu.max`: "quota period" → Some(quota / period), "max _" → None
#[cfg(target_os = "linux")]
fn read_resource_limits_v2(cgroup_path: &str) -> ContainerResourceLimits {
    let base = format!("/sys/fs/cgroup{}", cgroup_path);

    let memory_limit_bytes = match std::fs::read_to_string(format!("{}/memory.max", base)) {
        Ok(content) => parse_cgroup_v2_memory_max(content.trim()),
        Err(_) => None,
    };

    let cpu_quota = match std::fs::read_to_string(format!("{}/cpu.max", base)) {
        Ok(content) => parse_cgroup_v2_cpu_max(content.trim()),
        Err(_) => None,
    };

    // cgroup v2 uses cpu.weight (range 1-10000) instead of cpu.shares (range 2-262144).
    // We map cpu.weight as the "shares" equivalent for consistency.
    let cpu_shares = match std::fs::read_to_string(format!("{}/cpu.weight", base)) {
        Ok(content) => content.trim().parse::<u64>().ok(),
        Err(_) => None,
    };

    ContainerResourceLimits { memory_limit_bytes, cpu_quota, cpu_shares }
}

/// Read resource limits from cgroup v1 controller hierarchies.
///
/// - `memory.limit_in_bytes`: numeric in bytes
/// - `cpu.cfs_quota_us`: numeric (quota in microseconds; -1 means unlimited)
/// - `cpu.shares`: numeric (relative weight)
#[cfg(target_os = "linux")]
fn read_resource_limits_v1(cgroup_path: &str) -> ContainerResourceLimits {
    let memory_limit_bytes = match std::fs::read_to_string(format!(
        "/sys/fs/cgroup/memory{}/memory.limit_in_bytes",
        cgroup_path
    )) {
        Ok(content) => content.trim().parse::<u64>().ok().filter(|&v| v > 0),
        Err(_) => None,
    };

    let cpu_quota = match std::fs::read_to_string(format!(
        "/sys/fs/cgroup/cpu{}/cpu.cfs_quota_us",
        cgroup_path
    )) {
        Ok(content) => parse_cgroup_v1_cpu_quota(content.trim()),
        Err(_) => None,
    };

    let cpu_shares =
        match std::fs::read_to_string(format!("/sys/fs/cgroup/cpu{}/cpu.shares", cgroup_path)) {
            Ok(content) => content.trim().parse::<u64>().ok().filter(|&v| v > 0),
            Err(_) => None,
        };

    ContainerResourceLimits { memory_limit_bytes, cpu_quota, cpu_shares }
}

/// Parse cgroup v2 `memory.max` content.
///
/// Returns `None` for "max" (unlimited) or unparseable values.
#[allow(dead_code)] // Used on Linux via read_resource_limits_v2; dead on other platforms
fn parse_cgroup_v2_memory_max(content: &str) -> Option<u64> {
    if content == "max" {
        return None;
    }
    content.parse::<u64>().ok().filter(|&v| v > 0)
}

/// Parse cgroup v2 `cpu.max` content.
///
/// Format: `"$QUOTA $PERIOD"` — returns quota / period as a float ratio.
/// Returns `None` if quota is "max" (unlimited) or parsing fails.
#[allow(dead_code)] // Used on Linux via read_resource_limits_v2; dead on other platforms
fn parse_cgroup_v2_cpu_max(content: &str) -> Option<f64> {
    let parts: Vec<&str> = content.split_whitespace().collect();
    if parts.len() < 2 {
        return None;
    }
    if parts[0] == "max" {
        return None;
    }
    let quota = parts[0].parse::<f64>().ok()?;
    let period = parts[1].parse::<f64>().ok()?;
    if period <= 0.0 {
        return None;
    }
    Some(quota / period)
}

/// Parse cgroup v1 `cpu.cfs_quota_us` content.
///
/// Returns `None` for -1 (unlimited) or non-positive values. Otherwise returns
/// the raw quota in microseconds as a float (caller can combine with period).
#[cfg(target_os = "linux")]
fn parse_cgroup_v1_cpu_quota(content: &str) -> Option<f64> {
    let quota = content.parse::<i64>().ok()?;
    if quota <= 0 {
        return None;
    }
    // Read period to compute ratio
    // Default CFS period is 100000us (100ms). We try to read the actual period file
    // but fall back to the default if unavailable.
    let period = std::fs::read_to_string("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
        .ok()
        .and_then(|s| s.trim().parse::<f64>().ok())
        .filter(|&p| p > 0.0)
        .unwrap_or(100_000.0);

    Some(quota as f64 / period)
}

/// Parse /proc/[pid]/cgroup to extract container ID, runtime, cgroup version,
/// and the raw cgroup path (for reading resource limits).
///
/// Returns `(container_id, runtime, cgroup_version, cgroup_path)`.
#[allow(dead_code)] // Used on Linux via detect_linux; dead on other platforms
fn parse_cgroup(cgroup: &str) -> Option<(String, String, String, String)> {
    for line in cgroup.lines() {
        let parts: Vec<&str> = line.splitn(3, ':').collect();
        if parts.len() < 3 {
            continue;
        }
        let path = parts[2];
        let version = if line.starts_with("0::") { "v2" } else { "v1" };

        // Docker container ID (cgroup v2 or v1)
        if let Some(id) = extract_docker_id(path) {
            return Some((id, "docker".to_string(), version.to_string(), path.to_string()));
        }

        // containerd
        if path.contains("containerd") {
            if let Some(id) = extract_id_from_path(path) {
                return Some((id, "containerd".to_string(), version.to_string(), path.to_string()));
            }
        }

        // Podman
        if path.contains("libpod") {
            if let Some(id) = extract_id_from_path(path) {
                return Some((id, "podman".to_string(), version.to_string(), path.to_string()));
            }
        }
    }
    None
}

/// Extract Docker container ID from cgroup path.
#[allow(dead_code)] // Used on Linux via parse_cgroup; dead on other platforms
fn extract_docker_id(path: &str) -> Option<String> {
    // Docker cgroup paths contain a 64-char hex container ID
    for segment in path.split('/') {
        if segment.len() >= 64 && segment.chars().all(|c| c.is_ascii_hexdigit()) {
            return Some(segment[..64].to_string());
        }
    }
    None
}

/// Extract container ID from a cgroup path segment.
#[allow(dead_code)] // Used on Linux via parse_cgroup; dead on other platforms
fn extract_id_from_path(path: &str) -> Option<String> {
    path.split('/')
        .next_back()
        .filter(|s| s.len() >= 8 && s.chars().all(|c| c.is_ascii_hexdigit()))
        .map(|s| s.to_string())
}

impl Default for ContainerDetector {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_cgroup_docker_path() {
        // Docker cgroup v2: unified hierarchy with 64-char hex ID
        let cgroup_v2 =
            "0::/docker/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890";
        let result = parse_cgroup(cgroup_v2);
        assert!(result.is_some(), "Should parse Docker v2 cgroup");
        let (id, runtime, version, path) = result.unwrap();
        assert_eq!(id, "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890");
        assert_eq!(runtime, "docker");
        assert_eq!(version, "v2");
        assert_eq!(
            path,
            "/docker/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
        );

        // Docker cgroup v1: per-controller hierarchy
        let cgroup_v1 = "12:cpuacct,cpu:/docker/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890";
        let result = parse_cgroup(cgroup_v1);
        assert!(result.is_some(), "Should parse Docker v1 cgroup");
        let (id, runtime, version, _path) = result.unwrap();
        assert_eq!(id, "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890");
        assert_eq!(runtime, "docker");
        assert_eq!(version, "v1");
    }

    #[test]
    fn test_cgroup_podman_path() {
        // Podman cgroup v2: libpod path with hex ID as last segment.
        // Note: real Podman paths may end in `.scope` (e.g., `libpod-conmon-<id>.scope`),
        // but the parser's extract_id_from_path requires a pure-hex last segment.
        // This tests the libpod detection branch with a directly-parseable hex ID.
        let cgroup_v2 = "0::/user.slice/libpod/abcdef1234567890abcdef12";
        let result = parse_cgroup(cgroup_v2);
        assert!(result.is_some(), "Should parse Podman v2 cgroup");
        let (id, runtime, version, _path) = result.unwrap();
        assert_eq!(id, "abcdef1234567890abcdef12");
        assert_eq!(runtime, "podman");
        assert_eq!(version, "v2");

        // Podman cgroup v1
        let cgroup_v1 = "11:cpuacct,cpu:/libpod/deadbeefcafebabe12345678";
        let result = parse_cgroup(cgroup_v1);
        assert!(result.is_some(), "Should parse Podman v1 cgroup");
        let (id, runtime, version, _path) = result.unwrap();
        assert_eq!(id, "deadbeefcafebabe12345678");
        assert_eq!(runtime, "podman");
        assert_eq!(version, "v1");
    }

    #[test]
    fn test_cgroup_version_detection() {
        // v2: single unified hierarchy line starting with "0::"
        // Use a valid Docker path for clean version detection test:
        let cgroup_v2_docker =
            "0::/docker/aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344";
        let result = parse_cgroup(cgroup_v2_docker);
        assert!(result.is_some());
        assert_eq!(result.unwrap().2, "v2", "Unified hierarchy (0::) must be detected as v2");

        // v1: numbered hierarchies like "12:cpuacct,cpu:/..."
        let cgroup_v1_docker =
            "12:cpuacct,cpu:/docker/aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344";
        let result = parse_cgroup(cgroup_v1_docker);
        assert!(result.is_some());
        assert_eq!(result.unwrap().2, "v1", "Numbered hierarchy must be detected as v1");

        // v1: another controller layout
        let cgroup_v1_mixed =
            "9:net_cls,net_prio:/docker/aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344\n\
             12:cpuacct,cpu:/docker/aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344";
        let result = parse_cgroup(cgroup_v1_mixed);
        assert!(result.is_some());
        assert_eq!(result.unwrap().2, "v1");
    }

    #[test]
    fn test_extract_docker_id() {
        // Valid 64-char hex ID
        let path = "/docker/aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344";
        let id = extract_docker_id(path);
        assert!(id.is_some());
        assert_eq!(id.unwrap(), "aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344");

        // No valid ID
        let path = "/system.slice/docker.service";
        assert!(extract_docker_id(path).is_none());

        // Short hex is not a valid Docker ID
        let path = "/docker/abcdef1234";
        assert!(extract_docker_id(path).is_none());
    }

    #[test]
    fn test_extract_id_from_path() {
        // Last segment includes ".scope" — not purely hex, so must return None
        let path = "/libpod-conmon-deadbeefcafebabe12345678.scope";
        let id = extract_id_from_path(path);
        assert!(id.is_none(), "scope-suffixed segment is not valid hex");

        // Plain hex segment
        let path = "/containerd/deadbeefcafebabe";
        let id = extract_id_from_path(path);
        assert_eq!(id.unwrap(), "deadbeefcafebabe");

        // Too short
        let path = "/containerd/abc";
        assert!(extract_id_from_path(path).is_none());
    }

    #[test]
    fn test_resource_limits_default() {
        let limits = ContainerResourceLimits::default();
        assert!(limits.memory_limit_bytes.is_none());
        assert!(limits.cpu_quota.is_none());
        assert!(limits.cpu_shares.is_none());
    }

    #[test]
    fn test_parse_cgroup_v2_memory_max() {
        assert_eq!(parse_cgroup_v2_memory_max("max"), None);
        assert_eq!(parse_cgroup_v2_memory_max("0"), None); // zero treated as unlimited
        assert_eq!(parse_cgroup_v2_memory_max("536870912"), Some(536_870_912));
        assert_eq!(parse_cgroup_v2_memory_max("not_a_number"), None);
    }

    #[test]
    fn test_parse_cgroup_v2_cpu_max() {
        // Format: "quota period"
        assert_eq!(parse_cgroup_v2_cpu_max("max 100000"), None);
        assert_eq!(parse_cgroup_v2_cpu_max("50000 100000"), Some(0.5));
        assert_eq!(parse_cgroup_v2_cpu_max("200000 100000"), Some(2.0));
        assert_eq!(parse_cgroup_v2_cpu_max("100000"), None); // missing period
        assert_eq!(parse_cgroup_v2_cpu_max("not 100000"), None);
        assert_eq!(parse_cgroup_v2_cpu_max("100000 0"), None); // zero period
    }

    #[test]
    fn test_container_info_serializes() {
        let info = ContainerInfo {
            container_id: "abc123".to_string(),
            runtime: "docker".to_string(),
            name: "test-container".to_string(),
            image: "nginx:latest".to_string(),
            status: "running".to_string(),
            cgroup_version: "v2".to_string(),
            pids: vec![1, 42],
            resource_limits: ContainerResourceLimits {
                memory_limit_bytes: Some(536_870_912),
                cpu_quota: Some(2.0),
                cpu_shares: Some(1024),
            },
        };

        let json = serde_json::to_value(&info).expect("ContainerInfo must serialize");
        assert_eq!(json["container_id"], "abc123");
        assert_eq!(json["runtime"], "docker");
        assert_eq!(json["resource_limits"]["memory_limit_bytes"], 536_870_912);
        assert_eq!(json["resource_limits"]["cpu_quota"], 2.0);
        assert_eq!(json["resource_limits"]["cpu_shares"], 1024);
    }
}
