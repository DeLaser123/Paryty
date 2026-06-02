//! Build script for Paryty Agent
//!
//! Generates Rust code from Protocol Buffer definitions using tonic-build (prost).
//! Skips proto compilation if protoc is not available.
//!
/// Set PROTO_DIR env var to override the default proto directory path.
/// Default: ../proto (local dev), /app/proto (Docker build with project root context).
use std::env;

fn main() -> Result<(), Box<dyn std::error::Error>> {
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
