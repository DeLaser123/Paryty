//! eBPF Program Loader
//!
//! Loads and manages eBPF programs using libbpf-rs.
//!
//! On Linux, the loader:
//! - Verifies kernel capabilities (version >= 5.4, BTF, CAP_BPF)
//! - Loads embedded BPF ELF objects from compile-time `include_bytes!`
//! - Auto-attaches kprobes/kretprobes for each loaded program
//!
//! On non-Linux platforms, all operations return stubs with no-ops.

use anyhow::Result;
use tracing::warn;

// ---------------------------------------------------------------------------
// Shared types (both platforms)
// ---------------------------------------------------------------------------

/// Holds loaded BPF object handles after successful program loading.
///
/// On Linux, each field may hold a live [`libbpf_rs::Object`].
/// On non-Linux, all fields are `None` and [`is_loaded`](Self::is_loaded)
/// always returns `false`.
#[cfg(target_os = "linux")]
pub struct EbpfPrograms {
    pub tcp_object: Option<libbpf_rs::Object>,
    pub dns_object: Option<libbpf_rs::Object>,
    pub http_object: Option<libbpf_rs::Object>,
    /// Handles keeping kprobes/kretprobes attached.
    ///
    /// [`libbpf_rs::Link`] is the RAII guard for an attached BPF program.
    /// Dropping a `Link` detaches the corresponding probe, so all active
    /// links must be held for the entire lifetime of the eBPF programs.
    pub links: Vec<libbpf_rs::Link>,
}

#[cfg(target_os = "linux")]
impl EbpfPrograms {
    /// Returns `true` if at least one BPF program was successfully loaded.
    pub fn is_loaded(&self) -> bool {
        self.tcp_object.is_some() || self.dns_object.is_some() || self.http_object.is_some()
    }
}

// ---------------------------------------------------------------------------
// Non-Linux stub
// ---------------------------------------------------------------------------

#[cfg(not(target_os = "linux"))]
pub struct EbpfPrograms {
    pub tcp_object: Option<()>,
    pub dns_object: Option<()>,
    pub http_object: Option<()>,
}

#[cfg(not(target_os = "linux"))]
impl EbpfPrograms {
    /// Construct a stub with all programs `None`.
    pub fn stub() -> Self {
        Self { tcp_object: None, dns_object: None, http_object: None }
    }

    /// Always returns `false` on non-Linux platforms.
    pub fn is_loaded(&self) -> bool {
        false
    }
}

#[cfg(not(target_os = "linux"))]
pub struct EbpfLoader;

#[cfg(not(target_os = "linux"))]
impl EbpfLoader {
    pub fn new(_config: crate::config::EbpfConfig) -> Self {
        Self
    }

    /// On non-Linux platforms, returns a stub [`EbpfPrograms`] immediately.
    pub fn load(&self) -> Result<EbpfPrograms> {
        warn!("eBPF not supported on this platform. Using stub mode.");
        Ok(EbpfPrograms::stub())
    }
}

// ---------------------------------------------------------------------------
// Linux implementation
// ---------------------------------------------------------------------------

#[cfg(target_os = "linux")]
mod linux_impl {
    use super::*;
    use anyhow::Context;
    use std::io::Write;
    use std::path::Path;
    use tracing::{error, info, instrument, warn};

    /// Minimum kernel major.minor required for the eBPF features we use.
    const MIN_KERNEL_MAJOR: u32 = 5;
    const MIN_KERNEL_MINOR: u32 = 4;

    // ---- Embedded BPF bytecode (compiled by build.rs via clang) ----

    const TCP_TRACKER_ELF: &[u8] = include_bytes!(concat!(env!("OUT_DIR"), "/tcp_tracker.bpf.o"));
    const DNS_MAPPER_ELF: &[u8] = include_bytes!(concat!(env!("OUT_DIR"), "/dns_mapper.bpf.o"));
    const HTTP_INSPECTOR_ELF: &[u8] =
        include_bytes!(concat!(env!("OUT_DIR"), "/http_inspector.bpf.o"));

    /// Marker prefix that identifies kprobe programs in libbpf.
    const KPROBE_PREFIX: &str = "kprobe/";
    /// Marker prefix that identifies kretprobe programs in libbpf.
    const KRETPROBE_PREFIX: &str = "kretprobe/";

    /// Loader that reads [`EbpfConfig`] and loads the requested eBPF programs.
    pub struct EbpfLoader {
        config: crate::config::EbpfConfig,
    }

    impl EbpfLoader {
        /// Create a new loader from the given eBPF configuration.
        pub fn new(config: crate::config::EbpfConfig) -> Self {
            Self { config }
        }

        /// Load all enabled eBPF programs and return handles.
        ///
        /// Performs capability checks first, then conditionally loads each
        /// BPF program based on the configuration flags.
        #[instrument(skip(self), fields(
            tcp = self.config.tcp_connections,
            dns = self.config.dns_resolution,
            http = self.config.http_inspection,
        ))]
        pub fn load(&self) -> Result<EbpfPrograms> {
            self.check_capabilities().context("eBPF capability check failed")?;

            let mut all_links: Vec<libbpf_rs::Link> = Vec::new();

            let tcp_object = if self.config.tcp_connections {
                info!("Loading TCP tracker eBPF program");
                let (obj, links) = load_program("tcp_tracker", TCP_TRACKER_ELF)?;
                all_links.extend(links);
                Some(obj)
            } else {
                info!("TCP tracker eBPF program disabled in config, skipping");
                None
            };

            let dns_object = if self.config.dns_resolution {
                info!("Loading DNS mapper eBPF program");
                let (obj, links) = load_program("dns_mapper", DNS_MAPPER_ELF)?;
                all_links.extend(links);
                Some(obj)
            } else {
                info!("DNS mapper eBPF program disabled in config, skipping");
                None
            };

            let http_object = if self.config.http_inspection {
                info!("Loading HTTP inspector eBPF program");
                let (obj, links) = load_program("http_inspector", HTTP_INSPECTOR_ELF)?;
                all_links.extend(links);
                Some(obj)
            } else {
                info!("HTTP inspector eBPF program disabled in config, skipping");
                None
            };

            let loaded_count =
                [&tcp_object, &dns_object, &http_object].iter().filter(|o| o.is_some()).count();
            info!(loaded_count, link_count = all_links.len(), "eBPF program loading complete");

            Ok(EbpfPrograms { tcp_object, dns_object, http_object, links: all_links })
        }

        /// Pre-flight checks for eBPF capability on the host.
        ///
        /// Validates:
        /// 1. Kernel version >= 5.4
        /// 2. BTF availability at `/sys/kernel/btf/vmlinux`
        /// 3. CAP_BPF effective capability (warn-only, non-fatal)
        fn check_capabilities(&self) -> Result<()> {
            // Kernel version gate
            let (major, minor) = kernel_version()?;
            if major < MIN_KERNEL_MAJOR || (major == MIN_KERNEL_MAJOR && minor < MIN_KERNEL_MINOR) {
                anyhow::bail!(
                    "Kernel {}.{} does not meet minimum requirement {}.{} for eBPF",
                    major,
                    minor,
                    MIN_KERNEL_MAJOR,
                    MIN_KERNEL_MINOR
                );
            }
            info!(major, minor, "Kernel version check passed");

            // BTF availability
            let btf_path = Path::new("/sys/kernel/btf/vmlinux");
            if !btf_path.exists() {
                anyhow::bail!(
                    "BTF not available at /sys/kernel/btf/vmlinux — \
                     CONFIG_DEBUG_INFO_BTF may not be enabled"
                );
            }
            info!("BTF availability check passed");

            // CAP_BPF check (bit 39 in CapEff) — warn only
            match check_cap_bpf() {
                Ok(true) => info!("CAP_BPF capability detected"),
                Ok(false) => {
                    warn!(
                        "CAP_BPF capability (bit 39) not detected in effective capabilities; \
                         eBPF program loading may fail without root or CAP_SYS_ADMIN"
                    );
                }
                Err(e) => {
                    warn!(error = %e, "Could not determine CAP_BPF status");
                }
            }

            Ok(())
        }
    }

    /// Parse the kernel release string from `/proc/sys/kernel/osrelease` and
    /// return `(major, minor)`.
    pub(super) fn kernel_version() -> Result<(u32, u32)> {
        let raw = std::fs::read_to_string("/proc/sys/kernel/osrelease")
            .context("Failed to read /proc/sys/kernel/osrelease")?;
        let trimmed = raw.trim();
        parse_kernel_version(trimmed)
            .with_context(|| format!("Failed to parse kernel version from '{}'", trimmed))
    }

    /// Parse a kernel version string like `"5.15.0-generic"` into `(5, 15)`.
    pub(super) fn parse_kernel_version(s: &str) -> Result<(u32, u32)> {
        let mut parts = s.split('.');
        let major_str =
            parts.next().ok_or_else(|| anyhow::anyhow!("kernel version string is empty"))?;
        let minor_str = parts
            .next()
            .ok_or_else(|| anyhow::anyhow!("kernel version string missing minor component"))?;

        let major: u32 =
            major_str.parse().with_context(|| format!("invalid kernel major '{}'", major_str))?;
        let minor: u32 =
            minor_str.parse().with_context(|| format!("invalid kernel minor '{}'", minor_str))?;

        Ok((major, minor))
    }

    /// Check if CAP_BPF (bit 39) is set in the process's effective capabilities.
    ///
    /// Reads `/proc/self/status`, finds the `CapEff` line, and tests bit 39.
    pub(super) fn check_cap_bpf() -> Result<bool> {
        const CAP_BPF_BIT: u32 = 39;

        let status = std::fs::read_to_string("/proc/self/status")
            .context("Failed to read /proc/self/status")?;

        let cap_eff_line = status
            .lines()
            .find(|line| line.starts_with("CapEff:"))
            .ok_or_else(|| anyhow::anyhow!("CapEff line not found in /proc/self/status"))?;

        let hex_str = cap_eff_line
            .split_whitespace()
            .nth(1)
            .ok_or_else(|| anyhow::anyhow!("CapEff line has no value"))?;

        let cap_eff: u64 = u64::from_str_radix(hex_str, 16)
            .with_context(|| format!("failed to parse CapEff hex value '{}'", hex_str))?;

        Ok((cap_eff >> CAP_BPF_BIT) & 1 == 1)
    }

    /// Load a single eBPF program from ELF bytes.
    ///
    /// Writes the bytecode to a temporary file, opens it via libbpf, calls
    /// [`libbpf_rs::OpenObject::load`] to obtain a loaded
    /// [`libbpf_rs::Object`], and auto-attaches any kprobe/kretprobe
    /// programs by name. The temporary file is cleaned up on exit.
    ///
    /// Returns `(Object, Vec<Link>)` — the caller must keep the `Link`
    /// handles alive for the duration that the eBPF programs should remain
    /// attached. Dropping a `Link` detaches the corresponding probe.
    fn load_program(
        name: &str,
        elf_bytes: &[u8],
    ) -> Result<(libbpf_rs::Object, Vec<libbpf_rs::Link>)> {
        use std::fs;

        let tmp_path = std::env::temp_dir().join(format!("paryty_{}.bpf.o", name));

        // Write ELF bytes to a temporary file — libbpf_rs::open_file needs a path.
        {
            let mut tmp_file = fs::File::create(&tmp_path)
                .with_context(|| format!("Failed to create temp file: {}", tmp_path.display()))?;
            tmp_file
                .write_all(elf_bytes)
                .with_context(|| format!("Failed to write eBPF ELF to {}", tmp_path.display()))?;
            tmp_file.flush().context("Failed to flush temp BPF file")?;
        }

        // Open the BPF object (skeleton), then load it into the kernel.
        let open_obj = libbpf_rs::ObjectBuilder::default()
            .open_file(&tmp_path)
            .with_context(|| format!("Failed to open eBPF object '{}'", name))?;

        let mut obj =
            open_obj.load().with_context(|| format!("Failed to load eBPF program '{}'", name))?;

        info!(program = name, "eBPF object loaded successfully");

        // Auto-attach kprobes / kretprobes and collect Link handles.
        // Use prog.section() (not prog.name()) to get the ELF section name
        // like "kprobe/handle_tcp_connect". prog.name() returns only the
        // function name like "handle_tcp_connect".
        let mut links: Vec<libbpf_rs::Link> = Vec::new();
        let mut attached: u32 = 0;
        for prog in obj.progs_iter_mut() {
            let prog_name = prog.name().to_string();
            let section = prog.section().to_string();

            if section.starts_with(KPROBE_PREFIX) || section.starts_with(KRETPROBE_PREFIX) {
                match prog.attach() {
                    Ok(link) => {
                        links.push(link);
                        attached += 1;
                        info!(
                            program = name,
                            probe = %prog_name,
                            section = %section,
                            "Attached kprobe"
                        );
                    }
                    Err(e) => {
                        error!(
                            program = name,
                            probe = %prog_name,
                            section = %section,
                            error = %e,
                            "Failed to attach kprobe"
                        );
                    }
                }
            }
        }

        if attached > 0 {
            info!(program = name, attached, "All kprobes attached");
        }

        // Clean up the temporary file. Ignore errors (best-effort).
        if let Err(e) = fs::remove_file(&tmp_path) {
            warn!(
                path = %tmp_path.display(),
                error = %e,
                "Failed to remove temporary eBPF file (non-fatal)"
            );
        }

        Ok((obj, links))
    }
}

// Re-export the Linux implementation so callers use `loader::EbpfLoader` uniformly.
#[cfg(target_os = "linux")]
pub use linux_impl::EbpfLoader;

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(target_os = "linux")]
    use super::linux_impl::parse_kernel_version;

    #[test]
    fn ebpf_programs_stub_is_not_loaded() {
        #[cfg(not(target_os = "linux"))]
        {
            let programs = EbpfPrograms::stub();
            assert!(!programs.is_loaded());
            assert!(programs.tcp_object.is_none());
            assert!(programs.dns_object.is_none());
            assert!(programs.http_object.is_none());
        }
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn test_parse_kernel_version_valid() {
        assert_eq!(parse_kernel_version("5.15.0-generic").unwrap(), (5, 15));
        assert_eq!(parse_kernel_version("6.1.0").unwrap(), (6, 1));
        assert_eq!(parse_kernel_version("5.4.0-100-lowlatency").unwrap(), (5, 4));
        assert_eq!(parse_kernel_version("6.8.1").unwrap(), (6, 8));
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn test_parse_kernel_version_invalid() {
        assert!(parse_kernel_version("").is_err());
        assert!(parse_kernel_version("nope").is_err());
        assert!(parse_kernel_version("5").is_err());
        assert!(parse_kernel_version("a.b.c").is_err());
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn test_check_cap_bpf_reads_proc() {
        // This test just verifies the function doesn't panic — the actual
        // result depends on the test environment's capabilities.
        let _result = super::linux_impl::check_cap_bpf();
    }
}
