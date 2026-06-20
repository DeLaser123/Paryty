//! Paryty Agent - Main Entry Point
//!
//! This is the main entry point for the Paryty Agent. It initializes
//! the agent, loads configuration, wires the communication lifecycle,
//! starts collection layers, and handles cooperative shutdown via
//! a `CancellationToken` + OS signal handler.

use std::sync::Arc;

use anyhow::Result;
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};
use tracing_subscriber::{fmt, EnvFilter};

mod communication;
mod config;
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
mod ebpf;
mod metal;
mod proto;
mod supervisor;

/// Parsed CLI arguments.
struct CliArgs {
    config_path: Option<String>,
    set_key: Option<String>,
    cluster_agent_id: Option<String>,
}

/// Parse CLI arguments from argv. Hand-rolled to avoid pulling in clap.
fn parse_cli() -> CliArgs {
    let args: Vec<String> = std::env::args().collect();
    let mut result = CliArgs { config_path: None, set_key: None, cluster_agent_id: None };
    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "-c" | "--config" if i + 1 < args.len() => {
                result.config_path = Some(args[i + 1].clone());
                i += 2;
            }
            "--cluster-agent-id" if i + 1 < args.len() => {
                result.cluster_agent_id = Some(args[i + 1].clone());
                i += 2;
            }
            "--key" if i + 1 < args.len() => {
                result.set_key = Some(args[i + 1].clone());
                i += 2;
            }
            "-h" | "--help" => {
                eprintln!("Usage: paryty-agent [OPTIONS]");
                eprintln!();
                eprintln!("Options:");
                eprintln!("  -c, --config <PATH>  Path to YAML configuration file");
                eprintln!("  --cluster-agent-id <ID>  Cluster agent ID for auto-pairing");
                eprintln!(
                    "  --key <API_KEY>      Set or update the API key and persist to config file"
                );
                eprintln!("  -h, --help           Print help");
                eprintln!();
                eprintln!("Environment variables:");
                eprintln!("  PARYTY_API_KEY            API key (overrides config file)");
                eprintln!("  PARYTY_CLUSTER_ENDPOINT   Cluster endpoint (overrides config file)");
                eprintln!("  PARYTY_AGENT_CONFIG       Config file path (default: configs/agent/agent.yaml)");
                eprintln!("  PARYTY_CLUSTER_AGENT_ID   Cluster agent ID for auto-pairing");
                std::process::exit(0);
            }
            _ => {
                eprintln!("Unknown argument: {}. Run with --help for usage.", args[i]);
                std::process::exit(1);
            }
        }
    }
    result
}

#[tokio::main(flavor = "multi_thread")]
async fn main() -> Result<()> {
    // Initialize logging — respects RUST_LOG env var, defaults to info.
    fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .json()
        .init();

    info!("Starting Paryty Agent");

    // ── Root cancellation token for cooperative shutdown ──────────────
    let cancel_token = CancellationToken::new();

    // ── Load configuration ───────────────────────────────────────────
    let cli = parse_cli();

    // Handle --key flag: persist API key to config file and exit.
    if let Some(ref new_key) = cli.set_key {
        let config_path = cli.config_path.clone().unwrap_or_else(|| {
            std::env::var("PARYTY_AGENT_CONFIG")
                .unwrap_or_else(|_| "configs/agent/agent.yaml".to_string())
        });
        config::persist_api_key(&config_path, new_key)?;
        info!(path = %config_path, "API key persisted to config file");
        eprintln!("✓ API key updated in {}", config_path);
        eprintln!("  Restart the agent for the new key to take effect.");
        eprintln!(
            "  Or set PARYTY_API_KEY environment variable for immediate use without restart."
        );
        return Ok(());
    }

    let config = if let Some(path) = &cli.config_path {
        info!(path = %path, "Loading config from CLI argument");
        config::load_from_path(path)?
    } else {
        config::load()?
    };
    info!(
        agent_id = %config.agent.id,
        endpoint = %config.agent.cluster_endpoint,
        "Configuration loaded successfully"
    );

    // ── Initialize communication layer ───────────────────────────────
    let comm = Arc::new(communication::Client::new(&config).await?);
    // Store config path for runtime key refresh.
    comm.set_config_path(cli.config_path.clone().unwrap_or_else(|| {
        std::env::var("PARYTY_AGENT_CONFIG")
            .unwrap_or_else(|_| "configs/agent/agent.yaml".to_string())
    }))
    .await;
    info!("Communication layer initialized");

    // ── Wire communication lifecycle ─────────────────────────────────
    //
    // 1. Connect (best-effort — reconnection loop handles retries)
    // 2. Start background loops (heartbeat, stream listener, reconnect, registration)
    //
    // Registration is handled by a dedicated retry loop that keeps trying
    // with exponential backoff until the cluster accepts it. This ensures
    // the agent works on ANY environment (bare metal, VM, WSL2, container)
    // even if the cluster is temporarily unavailable or slow to start.

    match comm.connect().await {
        Ok(()) => {
            info!("Connected to cluster at {}", config.agent.cluster_endpoint);
        }
        Err(e) => {
            warn!(
                error = %e,
                endpoint = %config.agent.cluster_endpoint,
                "Initial connection failed — reconnection loop will retry"
            );
        }
    }

    // Start background communication loops.
    // Each loop uses the client's internal CancellationToken and will
    // stop when `client.shutdown()` is called.
    comm.start_reconnection_loop();
    comm.start_registration_loop();
    comm.start_heartbeat_loop();
    comm.start_stream_listener();
    info!("Communication background loops started (reconnect, registration, heartbeat, stream)");

    // ── Spawn collection layers ──────────────────────────────────────
    let mut handles: Vec<tokio::task::JoinHandle<()>> = Vec::new();

    // Layer 1: Metal Scraper
    if config.layers.metal.enabled {
        let token = cancel_token.child_token();
        let metal_config = config.layers.metal.clone();
        let comm = comm.clone();
        let handle = tokio::spawn(async move {
            tokio::select! {
                result = metal::run(metal_config, (*comm).clone()) => {
                    if let Err(e) = result {
                        error!("Metal scraper error: {}", e);
                    }
                }
                _ = token.cancelled() => {
                    info!("Metal scraper shutting down (cancel signal)");
                }
            }
        });
        handles.push(handle);
        info!("Metal scraper started");
    }

    // Layer 2: eBPF Network Observer (Linux only)
    //
    // libbpf-rs types (Object, Link, Program, Map) contain raw pointers
    // (NonNull<bpf_object>, etc.) that are NOT Send. We cannot use tokio::spawn
    // because it requires Send + 'static. Instead, we run the entire eBPF path
    // on a dedicated OS thread via spawn_blocking, with its own current_thread
    // tokio runtime for the async event loop.
    #[cfg(target_os = "linux")]
    if config.layers.ebpf.enabled {
        let token = cancel_token.child_token();
        let ebpf_config = config.layers.ebpf.clone();
        let comm = comm.clone();
        let handle = tokio::task::spawn_blocking(move || {
            // Create a single-threaded tokio runtime for the eBPF event loop.
            // Failure here must not bring down the whole agent — the metal
            // scraper keeps working without eBPF (graceful degradation
            // principle: eBPF → /proc fallback → stub).
            let rt = match tokio::runtime::Builder::new_current_thread().enable_all().build() {
                Ok(rt) => rt,
                Err(e) => {
                    error!("Failed to create eBPF thread runtime; eBPF layer disabled: {}", e);
                    return;
                }
            };

            rt.block_on(async move {
                tokio::select! {
                    result = ebpf::run(ebpf_config, (*comm).clone()) => {
                        if let Err(e) = result {
                            error!("eBPF observer error: {}", e);
                        }
                    }
                    _ = token.cancelled() => {
                        info!("eBPF observer shutting down (cancel signal)");
                    }
                }
            });
        });
        handles.push(handle);
        info!("eBPF network observer started (dedicated thread)");
    }

    // Layer 3: Supervisor (optional)
    if config.layers.supervisor.enabled {
        let token = cancel_token.child_token();
        let comm = comm.clone();
        let handle = tokio::spawn(async move {
            tokio::select! {
                _ = async {
                    let mut supervisor =
                        supervisor::Supervisor::new(supervisor::SupervisorConfig::default());
                    supervisor.run(&comm).await;
                } => {}
                _ = token.cancelled() => {
                    info!("Supervisor shutting down (cancel signal)");
                }
            }
        });
        handles.push(handle);
        info!("Supervisor started");
    }

    // Self-metrics endpoint
    if config.agent.self_metrics.enabled {
        let token = cancel_token.child_token();
        let port = config.agent.self_metrics.port;
        let handle = tokio::spawn(async move {
            tokio::select! {
                result = start_metrics_endpoint(port) => {
                    if let Err(e) = result {
                        error!("Metrics endpoint error: {}", e);
                    }
                }
                _ = token.cancelled() => {
                    info!("Metrics endpoint shutting down (cancel signal)");
                }
            }
        });
        handles.push(handle);
        info!(port = port, "Self-metrics endpoint started");
    }

    info!(
        agent_id = %config.agent.id,
        endpoint = %config.agent.cluster_endpoint,
        metal_enabled = config.layers.metal.enabled,
        ebpf_enabled = config.layers.ebpf.enabled,
        supervisor_enabled = config.layers.supervisor.enabled,
        "Paryty Agent started successfully"
    );

    // ── Signal handler ───────────────────────────────────────────────
    //
    // Spawns a background task that waits for Ctrl+C (SIGINT) and
    // triggers the root cancellation token.
    let signal_token = cancel_token.clone();
    tokio::spawn(async move {
        if let Err(e) = tokio::signal::ctrl_c().await {
            error!("Failed to listen for shutdown signal: {}", e);
            return;
        }
        info!("Shutdown signal received (Ctrl+C)");
        signal_token.cancel();
    });

    // ── Wait for cancellation ────────────────────────────────────────
    //
    // The agent blocks here until the cancellation token is triggered
    // by the signal handler (or any other code that calls cancel).
    cancel_token.cancelled().await;

    // ── Graceful shutdown ────────────────────────────────────────────
    //
    // 1. Shut down the communication layer (cancel internal tasks,
    //    flush edge buffer, disconnect gRPC).
    // 2. Wait for all collection layer tasks to finish (they observe
    //    the cancellation token and exit cleanly).
    info!("Initiating graceful shutdown");

    comm.shutdown().await;

    for handle in handles {
        match handle.await {
            Ok(()) => {}
            Err(e) if e.is_cancelled() => {
                // Task was cancelled — this is expected during shutdown.
            }
            Err(e) => {
                error!("Task panicked during shutdown: {:?}", e);
            }
        }
    }

    info!("Paryty Agent shutdown complete");
    Ok(())
}

async fn start_metrics_endpoint(port: u16) -> Result<()> {
    use tokio::io::AsyncWriteExt;
    use tokio::net::TcpListener;

    let listener = TcpListener::bind(format!("0.0.0.0:{}", port))
        .await
        .map_err(|e| anyhow::anyhow!("Failed to bind metrics endpoint: {}", e))?;

    info!("Metrics endpoint listening on port {}", port);

    loop {
        let (mut stream, _) = listener.accept().await?;

        tokio::spawn(async move {
            let body = serde_json::json!({
                "status": "healthy",
                "service": "paryty-agent",
                "version": env!("CARGO_PKG_VERSION"),
            });
            let body_str = body.to_string();

            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body_str.len(),
                body_str
            );

            let _ = stream.write_all(response.as_bytes()).await;
            let _ = stream.shutdown().await;
        });
    }
}
