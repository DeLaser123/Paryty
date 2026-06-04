//! Build script for Paryty Agent
//!
//! Two responsibilities:
//! 1. Generate Rust code from Protocol Buffer definitions using tonic-build (prost).
//! 2. Compile C eBPF programs to BPF bytecode using clang (Linux only).
//!
//! Both steps gracefully skip if the required tools are not available.

use std::env;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    compile_protos()?;

    // Compile eBPF programs (Linux only)
    #[cfg(target_os = "linux")]
    compile_ebpf_programs()?;

    Ok(())
}

/// Compile Protocol Buffer definitions to Rust code.
/// Skips gracefully if protoc is not installed.
fn compile_protos() -> Result<(), Box<dyn std::error::Error>> {
    // Allow PROTO_DIR env var override (set in Dockerfile for container builds)
    let proto_dir = env::var("PROTO_DIR").unwrap_or_else(|_| "../proto".to_string());

    // Tell Cargo to re-run if proto files change
    println!("cargo:rerun-if-changed={}", proto_dir);
    println!("cargo:rerun-if-env-changed=PROTO_DIR");

    // Check if protoc is available by trying to find it
    let protoc_available = which::which("protoc").is_ok();

    if !protoc_available {
        println!("cargo:warning=protoc not found, skipping proto compilation. Proto types will use placeholder definitions.");
        return Ok(());
    }

    // Collect all .proto files
    let proto_files = vec![
        format!("{}/paryty/v1/common.proto", proto_dir),
        format!("{}/paryty/v1/agent.proto", proto_dir),
        format!("{}/paryty/v1/ebpf.proto", proto_dir),
        format!("{}/paryty/v1/ingestion.proto", proto_dir),
        format!("{}/paryty/v1/query.proto", proto_dir),
        format!("{}/paryty/v1/sdk.proto", proto_dir),
    ];

    // Include paths for proto imports
    let include_dirs = vec![proto_dir.to_string()];

    // Configure tonic-build
    tonic_build::configure()
        .build_client(true)
        .build_server(true)
        .out_dir("src/proto")
        .compile(&proto_files, &include_dirs)?;

    Ok(())
}

/// Compile C eBPF programs to BPF bytecode using clang.
///
/// This function:
/// 1. Checks if clang is available (skips with warning if not)
/// 2. Generates vmlinux.h from the running kernel's BTF info
/// 3. Compiles each .bpf.c file to .bpf.o (BPF ELF object)
///
/// The compiled objects are placed in OUT_DIR and embedded into the
/// final binary via `include_bytes!` in the loader module.
#[cfg(target_os = "linux")]
fn compile_ebpf_programs() -> Result<(), Box<dyn std::error::Error>> {
    use std::path::PathBuf;
    use std::process::Command;

    // Check if clang is available
    if which::which("clang").is_err() {
        println!("cargo:warning=clang not found, skipping eBPF compilation. eBPF programs will not be available.");
        return Ok(());
    }

    let out_dir = PathBuf::from(env::var("OUT_DIR")?);
    let ebpf_dir = PathBuf::from("src/ebpf/bpf");

    // Step 1: Generate vmlinux.h if not exists
    let vmlinux_h = ebpf_dir.join("vmlinux.h");
    if !vmlinux_h.exists() {
        if which::which("bpftool").is_ok() {
            println!("cargo:warning=Generating vmlinux.h from running kernel BTF...");
            let status = Command::new("bpftool")
                .args(["btf", "dump", "file", "/sys/kernel/btf/vmlinux", "format", "c"])
                .stdout(std::fs::File::create(&vmlinux_h)?)
                .status()?;
            if !status.success() {
                println!("cargo:warning=bpftool failed to generate vmlinux.h. eBPF compilation may fail.");
            }
        } else {
            println!("cargo:warning=bpftool not found, cannot generate vmlinux.h. eBPF compilation may fail.");
        }
    }

    // Step 2: Detect target architecture for BPF compilation
    let arch = env::var("CARGO_CFG_TARGET_ARCH").unwrap_or_else(|_| "x86".to_string());
    let target_arch_flag = format!("-D__TARGET_ARCH_{}", arch);

    // Step 3: Compile each .bpf.c file to .bpf.o (BPF bytecode)
    let programs = ["tcp_tracker.bpf.c", "dns_mapper.bpf.c", "http_inspector.bpf.c"];

    for prog in &programs {
        let src = ebpf_dir.join(prog);
        let obj_name = prog.replace(".c", ".o");
        let obj = out_dir.join(&obj_name);

        // Tell Cargo to re-run if source changes
        println!("cargo:rerun-if-changed={}", src.display());

        if !src.exists() {
            println!("cargo:warning=eBPF source {} not found, skipping.", src.display());
            continue;
        }

        let status = Command::new("clang")
            .args([
                "-g",  // Debug info for BTF
                "-O2", // Optimization (required for verifier)
                "-target",
                "bpf",             // Target: BPF bytecode
                &target_arch_flag, // Architecture-specific defines
                "-I",
                ebpf_dir.to_str().unwrap_or("."),
                "-c",
                src.to_str().unwrap_or("."),
                "-o",
                obj.to_str().unwrap_or("."),
            ])
            .status()?;

        if !status.success() {
            return Err(format!("Failed to compile eBPF program: {}", prog).into());
        }

        println!("cargo:warning=Compiled eBPF: {} -> {}", prog, obj_name);
    }

    // Re-run if any header changes
    println!("cargo:rerun-if-changed={}", ebpf_dir.join("common.h").display());
    println!("cargo:rerun-if-changed={}", vmlinux_h.display());

    Ok(())
}
