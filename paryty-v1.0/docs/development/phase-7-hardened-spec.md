# Phase 7 Hardened Specification — SDK & Developer Experience

**Version:** 1.0.0
**Status:** LOCKED — All architectural decisions finalized
**Target LOC:** ~5,000 (Go + Python + Java + TypeScript)
**Estimated Effort:** 3-4 weeks for a senior SDK engineer

---

## Table of Contents

1. [Phase 7 Overview & Decisions](#1-phase-7-overview--decisions)
2. [Pre-Phase Setup](#2-pre-phase-setup)
3. [Layer 25: Go SDK Enhancement](#3-layer-25-go-sdk-enhancement)
4. [Layer 26: Multi-Language SDKs](#4-layer-26-multi-language-sdks)
5. [Verification Gates](#5-verification-gates)
6. [Performance Targets](#6-performance-targets)
7. [Contingency & Rollback](#7-contingency--rollback)
8. [Appendices](#8-appendices)

---

## 1. Phase 7 Overview & Decisions

### 1.1 What Phase 7 Delivers

Phase 7 makes Paryty easy for application developers to instrument their services. The Go SDK gets OpenTelemetry integration, environment-based configuration, and auto-instrumentation middleware. New SDKs for Python, Java, and Node.js are created with idiomatic wrappers over proto-generated stubs.

**Before Phase 7:** Go SDK exists but requires manual setup. No multi-language support. No OpenTelemetry integration. No auto-instrumentation.

**After Phase 7:** Production-grade SDKs for Go, Python, Java, and Node.js. One-line middleware for HTTP/gRPC/SQL instrumentation. Environment variable configuration. Published to package managers.

### 1.2 Architectural Decisions (LOCKED)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | OpenTelemetry Integration | **A — Wrap OTel SDK** | Paryty API wrapping OTel internally. Gets auto-instrumentation ecosystem. |
| 2 | Multi-Language SDKs | **B — Proto + Idiomatic Wrapper** | Proto stubs + language-native wrapper for great DX. |
| 3 | SDK Distribution | **A — Package Managers** | Go modules, PyPI, Maven Central, npm. Standard distribution. |
| 4 | Auto-Instrumentation | **A — Middleware-Based** | HTTP middleware, gRPC interceptors, SQL driver wrapper. Non-invasive. |

### 1.3 What Gets Built

| Layer | Component | Language | LOC | Description |
|-------|-----------|----------|-----|-------------|
| 25 | Go SDK Enhancement | Go | ~2,500 | OTel wrapper, env config, auto-instrumentation, examples |
| 26 | Python SDK | Python | ~800 | Proto stubs + Pythonic wrapper |
| 26 | Java SDK | Java | ~800 | Proto stubs + builder pattern wrapper |
| 26 | Node.js SDK | TypeScript | ~600 | Proto stubs + TypeScript types |

### 1.4 Existing Go SDK Assessment

**What exists (solid foundation):**
- `client.go` (388 lines): gRPC client with auto-reconnect, background flush, metric/trace/health callbacks
- `metrics.go` (414 lines): Counter (atomic), Gauge (mutex), Histogram (bucket-based), Registry, MetricSnapshot
- `health.go` (273 lines): HealthChecker with TCP/HTTP convenience checks, periodic reporting, liveness/readiness
- `traces.go` (265 lines): Tracer with Span, context propagation (spanContextKey), trace batching
- `sdk.proto` (226 lines): SDKService (ReportMetric, ReportTrace, ReportHealth, StreamMetrics, GetConfig)

**What's missing (Phase 7 adds):**
- OpenTelemetry bridge (export Paryty metrics/traces to OTel collector)
- Environment variable configuration (PARYTY_AGENT_ADDR, PARYTY_SERVICE_NAME, etc.)
- Auto-instrumentation middleware (HTTP, gRPC, SQL)
- Sampling configuration
- Multi-language SDKs

---

## 2. Pre-Phase Setup

### 2.1 New File Structure

```
agent/go_sdk/
├── paryty/
│   ├── client.go              # ENHANCE — OTel bridge, env config
│   ├── metrics.go             # ENHANCE — OTel metric exporter
│   ├── traces.go              # ENHANCE — OTel span exporter
│   ├── health.go              # KEEP
│   ├── otel_bridge.go         # NEW — OpenTelemetry bridge (~300 LOC)
│   ├── config.go              # NEW — Env-based configuration (~200 LOC)
│   ├── middleware.go           # NEW — HTTP/gRPC middleware (~400 LOC)
│   ├── sampling.go            # NEW — Sampling strategies (~150 LOC)
│   └── examples_test.go       # NEW — Example-based tests (~200 LOC)
├── examples/
│   ├── http_server/           # NEW — HTTP server example
│   ├── grpc_server/           # NEW — gRPC server example
│   ├── worker/                # NEW — Background worker example
│   └── quickstart/            # NEW — 5-minute quickstart
├── go.mod                     # UPDATE — Add OTel dependencies
└── go.sum

sdks/
├── python/
│   ├── pyproject.toml         # Python package config
│   ├── paryty/
│   │   ├── __init__.py
│   │   ├── client.py          # Pythonic client wrapper
│   │   ├── metrics.py         # Counter, Gauge, Histogram
│   │   ├── tracing.py         # Tracer wrapper
│   │   ├── health.py          # Health checker
│   │   ├── middleware/
│   │   │   ├── __init__.py
│   │   │   ├── django.py      # Django middleware
│   │   │   ├── flask.py       # Flask middleware
│   │   │   └── fastapi.py     # FastAPI middleware
│   │   └── _proto/            # Generated proto stubs
│   │       ├── sdk_pb2.py
│   │       └── sdk_pb2_grpc.py
│   └── tests/
├── java/
│   ├── pom.xml                # Maven package config
│   ├── src/main/java/io/paryty/
│   │   ├── ParytyClient.java  # Builder-pattern client
│   │   ├── Metrics.java       # Counter, Gauge, Histogram
│   │   ├── Tracing.java       # Tracer wrapper
│   │   ├── Health.java        # Health checker
│   │   ├── middleware/
│   │   │   ├── ServletFilter.java    # Servlet filter
│   │   │   └── GrpcInterceptor.java  # gRPC interceptor
│   │   └── proto/             # Generated proto stubs
│   └── src/test/java/
└── nodejs/
    ├── package.json           # npm package config
    ├── tsconfig.json
    ├── src/
    │   ├── index.ts           # Main export
    │   ├── client.ts          # TypeScript client
    │   ├── metrics.ts         # Counter, Gauge, Histogram
    │   ├── tracing.ts         # Tracer wrapper
    │   ├── health.ts          # Health checker
    │   ├── middleware/
    │   │   ├── express.ts     # Express middleware
    │   │   ├── koa.ts         # Koa middleware
    │   │   └── grpc.ts        # gRPC interceptor
    │   └── proto/             # Generated proto stubs
    └── __tests__/
```

---

## 3. Layer 25: Go SDK Enhancement

### 3.1 OpenTelemetry Bridge

**File:** `agent/go_sdk/paryty/otel_bridge.go` (~300 LOC)

```go
// OpenTelemetry bridge for the Paryty SDK.
//
// This module bridges Paryty's internal metrics/traces to the OpenTelemetry
// ecosystem. It allows users who already use OTel to seamlessly integrate
// Paryty without changing their existing instrumentation.
//
// ARCHITECTURE:
//   Paryty SDK → OTel Bridge → OTel Collector → Any backend
//                                       ↓
//                                  Paryty Agent (via gRPC)
//
// The bridge acts as an OTel exporter that sends data to both
// the Paryty Agent AND the OTel collector simultaneously.

package paryty

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/sdk/metric/sdkmetric"
	"go.opentelemetry.io/otel/sdk/trace/sdktrace"
)

// OTelBridge connects Paryty SDK to OpenTelemetry.
type OTelBridge struct {
	client       *Client
	meterProvider *sdkmetric.MeterProvider
	tracerProvider *sdktrace.TracerProvider
	meter        metric.Meter
	tracer       trace.Tracer
}

// OTelConfig configures the OpenTelemetry bridge.
type OTelConfig struct {
	// Enabled enables the OTel bridge.
	Enabled bool
	// ServiceName overrides the service name for OTel.
	ServiceName string
	// OTelEndpoint is the OTel collector endpoint (e.g., "localhost:4317").
	OTelEndpoint string
	// ExportInterval is the metric export interval (default: 10s).
	ExportInterval time.Duration
	// SampleRate is the trace sample rate (0.0 to 1.0, default: 1.0).
	SampleRate float64
}

// NewOTelBridge creates a new OpenTelemetry bridge.
//
// USAGE:
//
//	bridge, err := paryty.NewOTelBridge(client, paryty.OTelConfig{
//	    Enabled:      true,
//	    OTelEndpoint: "localhost:4317",
//	    SampleRate:   0.1, // Sample 10% of traces
//	})
//	if err != nil { log.Fatal(err) }
//	defer bridge.Shutdown(ctx)
//
//	// Now use OTel API as normal — data goes to both Paryty and OTel
//	meter := bridge.Meter("my-service")
//	counter, _ := meter.Int64Counter("requests.total")
//	counter.Add(ctx, 1)
func NewOTelBridge(client *Client, cfg OTelConfig) (*OTelBridge, error) {
	if cfg.ExportInterval == 0 {
		cfg.ExportInterval = 10 * time.Second
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0
	}
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = client.config.ServiceName
	}

	// Create Paryty metric exporter (sends to agent via gRPC)
	parytyExporter := &parytyMetricExporter{client: client}

	// Create OTel metric provider with both exporters
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(parytyExporter,
				sdkmetric.WithInterval(cfg.ExportInterval),
			),
		),
	)

	// Create Paryty span exporter
	spanExporter := &parytySpanExporter{client: client}

	// Create OTel tracer provider
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(spanExporter),
		sdktrace.WithSampler(
			sdktrace.ParentBased(
				sdktrace.TraceIDRatioBased(cfg.SampleRate),
			),
		),
	)

	// Set as global providers
	otel.SetMeterProvider(meterProvider)
	otel.SetTracerProvider(tracerProvider)

	return &OTelBridge{
		client:         client,
		meterProvider:  meterProvider,
		tracerProvider: tracerProvider,
		meter:          meterProvider.Meter(serviceName),
		tracer:         tracerProvider.Tracer(serviceName),
	}, nil
}

// Meter returns an OTel meter that exports to Paryty.
func (b *OTelBridge) Meter(name string) metric.Meter {
	return b.meterProvider.Meter(name)
}

// Tracer returns an OTel tracer that exports to Paryty.
func (b *OTelBridge) Tracer(name string) trace.Tracer {
	return b.tracerProvider.Tracer(name)
}

// Shutdown gracefully shuts down the bridge.
func (b *OTelBridge) Shutdown(ctx context.Context) error {
	if err := b.meterProvider.Shutdown(ctx); err != nil {
		return err
	}
	return b.tracerProvider.Shutdown(ctx)
}

// parytyMetricExporter implements sdkmetric.Exporter.
// It converts OTel metrics to Paryty MetricSnapshots and sends via SDK.
type parytyMetricExporter struct {
	client *Client
}

func (e *parytyMetricExporter) Export(ctx context.Context, rm *sdkmetric.ResourceMetrics) error {
	// Convert OTel metrics to Paryty format and send via client
	// This is the bridge: OTel → Paryty
	return nil
}

func (e *parytyMetricExporter) Temporality(kind sdkmetric.InstrumentKind) sdkmetric.Temporality {
	return sdkmetric.CumulativeTemporality
}

func (e *parytyMetricExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}

func (e *parytyMetricExporter) Shutdown(ctx context.Context) error { return nil }
func (e *parytyMetricExporter) ForceFlush(ctx context.Context) error { return nil }

// parytySpanExporter implements sdktrace.SpanExporter.
type parytySpanExporter struct {
	client *Client
}

func (e *parytySpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	// Convert OTel spans to Paryty Span format and send via client
	return nil
}

func (e *parytySpanExporter) Shutdown(ctx context.Context) error { return nil }
```

### 3.2 Environment-Based Configuration

**File:** `agent/go_sdk/paryty/config.go` (~200 LOC)

```go
// Environment-based configuration for the Paryty SDK.
//
// Supports configuration via environment variables, YAML file,
// and programmatic options. Priority: Programmatic > Env > YAML > Defaults.
//
// ENVIRONMENT VARIABLES:
//   PARYTY_AGENT_ADDR          — Agent address (default: localhost:9090)
//   PARYTY_SERVICE_NAME        — Service name (default: unknown)
//   PARYTY_METRIC_FLUSH_INTERVAL — Metric flush interval (default: 10s)
//   PARYTY_TRACE_FLUSH_INTERVAL  — Trace flush interval (default: 15s)
//   PARYTY_HEALTH_INTERVAL       — Health check interval (default: 30s)
//   PARYTY_TRACE_SAMPLE_RATE     — Trace sample rate (default: 1.0)
//   PARYTY_ENABLED               — Enable/disable SDK (default: true)
//   PARYTY_ENVIRONMENT           — Environment (dev/staging/prod)

package paryty

import (
	"os"
	"strconv"
	"time"
)

// EnvConfig reads configuration from environment variables.
func EnvConfig() Config {
	cfg := DefaultConfig()

	if addr := os.Getenv("PARYTY_AGENT_ADDR"); addr != "" {
		cfg.AgentAddr = addr
	}
	if name := os.Getenv("PARYTY_SERVICE_NAME"); name != "" {
		cfg.ServiceName = name
	}
	if interval := os.Getenv("PARYTY_METRIC_FLUSH_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			cfg.MetricFlushInterval = d
		}
	}
	if interval := os.Getenv("PARYTY_TRACE_FLUSH_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			cfg.TraceFlushInterval = d
		}
	}
	if interval := os.Getenv("PARYTY_HEALTH_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			cfg.HealthCheckInterval = d
		}
	}
	if rate := os.Getenv("PARYTY_TRACE_SAMPLE_RATE"); rate != "" {
		if r, err := strconv.ParseFloat(rate, 64); err == nil {
			cfg.TraceSampleRate = r
		}
	}
	if enabled := os.Getenv("PARYTY_ENABLED"); enabled == "false" {
		cfg.Enabled = false
	}
	if env := os.Getenv("PARYTY_ENVIRONMENT"); env != "" {
		cfg.Environment = env
	}

	return cfg
}

// NewClientFromEnv creates a client using environment variables.
//
// USAGE:
//
//	client, err := paryty.NewClientFromEnv()
//	// Reads PARYTY_AGENT_ADDR, PARYTY_SERVICE_NAME, etc.
func NewClientFromEnv() (*Client, error) {
	cfg := EnvConfig()
	if !cfg.Enabled {
		return NewNoopClient(), nil
	}
	return NewClientWithConfig(cfg)
}

// NewNoopClient creates a client that does nothing (for testing/disabled mode).
func NewNoopClient() *Client {
	return &Client{
		config:    Config{Enabled: false},
		registry:  NewRegistry(),
		tracer:    NewTracer("noop"),
		health:    NewHealthChecker("noop"),
	}
}

// Add to Config struct:
// Enabled       bool
// TraceSampleRate float64
// Environment   string  // "dev", "staging", "prod"
```

### 3.3 Auto-Instrumentation Middleware

**File:** `agent/go_sdk/paryty/middleware.go` (~400 LOC)

```go
// Auto-instrumentation middleware for HTTP, gRPC, and SQL.
//
// MIDDLEWARE PATTERN:
// Users wrap their handlers/interceptors with Paryty middleware.
// The middleware automatically creates spans, records metrics, and
// propagates trace context.
//
// USAGE:
//
//	// HTTP
//	http.Handle("/api", paryty.Middleware(client, handler))
//
//	// gRPC
//	grpc.UnaryInterceptor(paryty.UnaryServerInterceptor(client))
//
//	// SQL
//	db, _ := sql.Open("paryty", "postgres://...")

package paryty

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ============================================================================
// HTTP Middleware
// ============================================================================

// Middleware returns an HTTP middleware that instruments requests.
//
// Automatically:
//   - Creates a span for each request
//   - Records request duration histogram
//   - Counts requests by method and status
//   - Propagates trace context via W3C headers
//   - Records errors
//
// USAGE:
//
//	client, _ := paryty.NewClientFromEnv()
//	defer client.Close()
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/api/orders", handleOrders)
//
//	instrumented := paryty.Middleware(client, mux)
//	http.ListenAndServe(":8080", instrumented)
func Middleware(client *Client, next http.Handler) http.Handler {
	requestCounter := client.Registry().RegisterCounter(
		"http_requests_total",
		WithDescription("Total HTTP requests"),
	)
	requestDuration := client.Registry().RegisterHistogram(
		"http_request_duration_seconds",
		WithDescription("HTTP request duration"),
		WithUnit("seconds"),
		WithBuckets([]float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0}),
	)
	errorCounter := client.Registry().RegisterCounter(
		"http_errors_total",
		WithDescription("Total HTTP errors"),
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Extract trace context from headers
		ctx := extractHTTPTraceContext(r.Context(), r)

		// Start span
		ctx, span := client.StartSpan(ctx, fmt.Sprintf("%s %s", r.Method, r.URL.Path),
			WithSpanKind(SpanKindServer),
			WithSpanAttributes(map[string]string{
				"http.method":      r.Method,
				"http.url":         r.URL.String(),
				"http.remote_addr": r.RemoteAddr,
			}),
		)
		defer span.End()

		// Wrap response writer to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: 200}

		// Call next handler
		next.ServeHTTP(rw, r.WithContext(ctx))

		// Record metrics
		duration := time.Since(start)
		requestCounter.Inc()
		requestDuration.Observe(duration.Seconds())

		// Record status
		span.SetAttribute("http.status_code", fmt.Sprintf("%d", rw.statusCode))
		if rw.statusCode >= 400 {
			errorCounter.Inc()
			span.SetAttribute("error", "true")
		}
		if rw.statusCode >= 500 {
			span.SetStatus(SpanStatusError, fmt.Sprintf("HTTP %d", rw.statusCode))
		}
	})
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// ============================================================================
// gRPC Interceptor
// ============================================================================

// UnaryServerInterceptor returns a gRPC unary server interceptor.
//
// Automatically:
//   - Creates a span for each RPC
//   - Records RPC duration histogram
//   - Counts RPCs by method and status
//   - Propagates trace context via gRPC metadata
//
// USAGE:
//
//	server := grpc.NewServer(
//	    grpc.UnaryInterceptor(paryty.UnaryServerInterceptor(client)),
//	)
func UnaryServerInterceptor(client *Client) grpc.UnaryServerInterceptor {
	rpcCounter := client.Registry().RegisterCounter(
		"grpc_server_requests_total",
		WithDescription("Total gRPC server requests"),
	)
	rpcDuration := client.Registry().RegisterHistogram(
		"grpc_server_request_duration_seconds",
		WithDescription("gRPC server request duration"),
		WithBuckets([]float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0}),
	)

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		// Extract trace context from metadata
		ctx = extractGRPCMetadata(ctx)

		// Start span
		ctx, span := client.StartSpan(ctx, info.FullMethod,
			WithSpanKind(SpanKindServer),
			WithSpanAttributes(map[string]string{
				"rpc.system":  "grpc",
				"rpc.method":  info.FullMethod,
			}),
		)
		defer span.End()

		// Call handler
		resp, err := handler(ctx, req)

		// Record metrics
		duration := time.Since(start)
		rpcCounter.Inc()
		rpcDuration.Observe(duration.Seconds())

		if err != nil {
			span.SetError(err)
			span.SetAttribute("rpc.grpc.status_code", "error")
		}

		return resp, err
	}
}

// ============================================================================
// SQL Driver Wrapper
// ============================================================================

// OpenSQL wraps a SQL driver connection with instrumentation.
//
// USAGE:
//
//	db, _ := paryty.OpenSQL(client, "postgres", "postgres://localhost/mydb")
//	// Use db as normal — all queries are instrumented
func OpenSQL(client *Client, driverName, dsn string) (*sql.DB, error) {
	wrappedDriver := &instrumentedDriver{
		client:     client,
		driverName: driverName,
	}
	sql.Register("paryty_"+driverName, wrappedDriver)
	return sql.Open("paryty_"+driverName, dsn)
}

type instrumentedDriver struct {
	client     *Client
	driverName string
}

// extractHTTPTraceContext extracts W3C trace context from HTTP headers.
func extractHTTPTraceContext(ctx context.Context, r *http.Request) context.Context {
	// W3C Trace Context: traceparent header
	traceparent := r.Header.Get("traceparent")
	if traceparent != "" {
		// Parse: version-traceId-spanId-flags
		// Store in context for span creation
	}
	return ctx
}

// extractGRPCMetadata extracts trace context from gRPC metadata.
func extractGRPCMetadata(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	traceparents := md.Get("traceparent")
	if len(traceparents) > 0 {
		// Parse and store in context
	}
	return ctx
}
```

### 3.4 SDK Go Dependencies

**Add to `agent/go_sdk/go.mod`:**

```
require (
    go.opentelemetry.io/otel v1.24.0
    go.opentelemetry.io/otel/metric v1.24.0
    go.opentelemetry.io/otel/trace v1.24.0
    go.opentelemetry.io/otel/sdk v1.24.0
    go.opentelemetry.io/otel/sdk/metric v1.24.0
    go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.49.0
)
```

---

## 4. Layer 26: Multi-Language SDKs

### 4.1 Python SDK

**File:** `sdks/python/paryty/client.py` (~300 LOC)

```python
"""
Paryty Python SDK — Idiomatic Python wrapper.

USAGE:
    from paryty import ParytyClient, Counter, Gauge, Histogram

    client = ParytyClient(agent_addr="localhost:9090", service_name="my-service")
    client.connect()

    # Metrics
    counter = client.counter("requests_total", description="Total requests")
    counter.inc()

    gauge = client.gauge("active_connections", description="Active connections")
    gauge.set(42)

    histogram = client.histogram("request_duration_seconds", buckets=[0.01, 0.05, 0.1, 0.5, 1.0])
    histogram.observe(0.045)

    # Tracing
    with client.start_span("process_order") as span:
        span.set_attribute("order_id", "12345")
        # ... do work ...
        span.add_event("payment_processed")

    # Health
    @client.health_check("database")
    def check_db():
        db.ping()
        return HealthStatus.HEALTHY

    # Auto-flush on exit
    client.close()
"""

import os
import time
import threading
from contextlib import contextmanager
from dataclasses import dataclass, field
from typing import Optional, Callable
import grpc

# Import generated proto stubs
from ._proto import sdk_pb2, sdk_pb2_grpc


@dataclass
class ParytyConfig:
    """SDK configuration with env var support."""
    agent_addr: str = "localhost:9090"
    service_name: str = "unknown"
    metric_flush_interval: float = 10.0  # seconds
    trace_flush_interval: float = 15.0
    health_check_interval: float = 30.0
    trace_sample_rate: float = 1.0
    enabled: bool = True
    environment: str = "dev"

    @classmethod
    def from_env(cls) -> "ParytyConfig":
        """Load configuration from environment variables."""
        return cls(
            agent_addr=os.getenv("PARYTY_AGENT_ADDR", "localhost:9090"),
            service_name=os.getenv("PARYTY_SERVICE_NAME", "unknown"),
            metric_flush_interval=float(os.getenv("PARYTY_METRIC_FLUSH_INTERVAL", "10")),
            trace_flush_interval=float(os.getenv("PARYTY_TRACE_FLUSH_INTERVAL", "15")),
            health_check_interval=float(os.getenv("PARYTY_HEALTH_INTERVAL", "30")),
            trace_sample_rate=float(os.getenv("PARYTY_TRACE_SAMPLE_RATE", "1.0")),
            enabled=os.getenv("PARYTY_ENABLED", "true").lower() == "true",
            environment=os.getenv("PARYTY_ENVIRONMENT", "dev"),
        )


class ParytyClient:
    """
    Paryty SDK client for Python.

    Connects to the local Paryty Agent via gRPC and provides
    idiomatic Python APIs for metrics, tracing, and health checks.
    """

    def __init__(
        self,
        agent_addr: str = None,
        service_name: str = None,
        config: ParytyConfig = None,
    ):
        self._config = config or ParytyConfig.from_env()
        if agent_addr:
            self._config.agent_addr = agent_addr
        if service_name:
            self._config.service_name = service_name

        self._channel: Optional[grpc.Channel] = None
        self._stub: Optional[sdk_pb2_grpc.SDKServiceStub] = None
        self._metrics: list = []
        self._traces: list = []
        self._health_checks: dict[str, Callable] = {}
        self._lock = threading.Lock()
        self._connected = False
        self._flush_thread: Optional[threading.Thread] = None
        self._stop_event = threading.Event()

    def connect(self):
        """Connect to the Paryty Agent."""
        if not self._config.enabled:
            return

        self._channel = grpc.insecure_channel(self._config.agent_addr)
        self._stub = sdk_pb2_grpc.SDKServiceStub(self._channel)
        self._connected = True

        # Start background flush thread
        self._flush_thread = threading.Thread(target=self._flush_loop, daemon=True)
        self._flush_thread.start()

    def close(self):
        """Close the connection and flush remaining data."""
        self._stop_event.set()
        self._flush()
        if self._channel:
            self._channel.close()
        self._connected = False

    # === Metrics ===

    def counter(self, name: str, description: str = "", unit: str = "") -> "Counter":
        """Create a counter metric."""
        return Counter(self, name, description, unit)

    def gauge(self, name: str, description: str = "", unit: str = "") -> "Gauge":
        """Create a gauge metric."""
        return Gauge(self, name, description, unit)

    def histogram(self, name: str, description: str = "", unit: str = "",
                  buckets: list[float] = None) -> "Histogram":
        """Create a histogram metric."""
        return Histogram(self, name, description, unit, buckets)

    # === Tracing ===

    @contextmanager
    def start_span(self, name: str, kind: str = "internal", **attributes):
        """Start a trace span (context manager)."""
        span = Span(self, name, kind, attributes)
        try:
            yield span
        except Exception as e:
            span.set_error(str(e))
            raise
        finally:
            span.end()

    # === Health ===

    def health_check(self, name: str):
        """Decorator to register a health check."""
        def decorator(fn):
            self._health_checks[name] = fn
            return fn
        return decorator

    # === Internal ===

    def _record_metric(self, metric):
        with self._lock:
            self._metrics.append(metric)

    def _record_trace(self, trace):
        with self._lock:
            self._traces.append(trace)

    def _flush(self):
        """Flush metrics and traces to agent."""
        if not self._connected or not self._stub:
            return

        with self._lock:
            metrics = self._metrics.copy()
            self._metrics.clear()

        if metrics:
            try:
                request = sdk_pb2.ReportMetricsRequest(
                    application_id=self._config.service_name,
                    metrics=[m.to_proto() for m in metrics],
                )
                self._stub.ReportMetrics(request)
            except grpc.RpcError:
                pass  # Will retry on next flush

    def _flush_loop(self):
        """Background flush loop."""
        while not self._stop_event.is_set():
            self._stop_event.wait(self._config.metric_flush_interval)
            self._flush()


# ============================================================================
# Metric Types
# ============================================================================

class Counter:
    """Monotonically increasing counter."""

    def __init__(self, client: ParytyClient, name: str, description: str, unit: str):
        self._client = client
        self._name = name
        self._description = description
        self._unit = unit
        self._value = 0

    def inc(self, delta: int = 1):
        """Increment the counter."""
        self._value += delta

    def get(self) -> int:
        """Get current value."""
        return self._value


class Gauge:
    """Value that can go up and down."""

    def __init__(self, client: ParytyClient, name: str, description: str, unit: str):
        self._client = client
        self._name = name
        self._value = 0.0

    def set(self, value: float):
        """Set the gauge value."""
        self._value = value

    def inc(self, delta: float = 1.0):
        """Increment the gauge."""
        self._value += delta

    def dec(self, delta: float = 1.0):
        """Decrement the gauge."""
        self._value -= delta


class Histogram:
    """Distribution of observed values."""

    def __init__(self, client: ParytyClient, name: str, description: str,
                 unit: str, buckets: list[float] = None):
        self._client = client
        self._name = name
        self._buckets = buckets or [0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0]
        self._observations: list[float] = []

    def observe(self, value: float):
        """Record an observation."""
        self._observations.append(value)


# ============================================================================
# Tracing
# ============================================================================

class Span:
    """A single trace span."""

    def __init__(self, client: ParytyClient, name: str, kind: str, attributes: dict):
        self._client = client
        self._name = name
        self._kind = kind
        self._attributes = dict(attributes)
        self._start_time = time.time()
        self._end_time: Optional[float] = None
        self._events: list = []
        self._error: Optional[str] = None

    def set_attribute(self, key: str, value: str):
        """Set a span attribute."""
        self._attributes[key] = value

    def add_event(self, name: str, **attributes):
        """Add a span event."""
        self._events.append({"name": name, "attributes": attributes, "time": time.time()})

    def set_error(self, message: str):
        """Mark span as errored."""
        self._error = message

    def end(self):
        """End the span."""
        self._end_time = time.time()
        # Record to client for batched sending
        self._client._record_trace(self)


# ============================================================================
# Middleware
# ============================================================================

def django_middleware(get_response):
    """Django middleware for auto-instrumentation."""
    def middleware(request):
        client = getattr(request, '_paryty_client', None)
        if not client:
            return get_response(request)

        with client.start_span(
            f"{request.method} {request.path}",
            http_method=request.method,
            http_url=request.build_absolute_uri(),
        ) as span:
            response = get_response(request)
            span.set_attribute("http.status_code", str(response.status_code))
            return response

    return middleware
```

### 4.2 Java SDK

**File:** `sdks/java/src/main/java/io/paryty/ParytyClient.java` (~300 LOC)

```java
/**
 * Paryty Java SDK — Builder-pattern client.
 *
 * USAGE:
 *   ParytyClient client = ParytyClient.builder()
 *       .agentAddr("localhost:9090")
 *       .serviceName("my-service")
 *       .build();
 *
 *   client.connect();
 *
 *   Counter counter = client.counter("requests_total")
 *       .description("Total requests")
 *       .build();
 *   counter.inc();
 *
 *   try (Span span = client.startSpan("process_order")) {
 *       span.setAttribute("order_id", "12345");
 *       // ... do work ...
 *   }
 *
 *   client.close();
 */
package io.paryty;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import java.time.Duration;
import java.util.concurrent.*;
import java.util.Map;
import java.util.HashMap;

public class ParytyClient implements AutoCloseable {

    private final Config config;
    private ManagedChannel channel;
    private SDKServiceGrpc.SDKServiceBlockingStub stub;
    private final ScheduledExecutorService scheduler;
    private final ConcurrentHashMap<String, Object> metrics;
    private boolean connected = false;

    private ParytyClient(Config config) {
        this.config = config;
        this.scheduler = Executors.newScheduledThreadPool(2);
        this.metrics = new ConcurrentHashMap<>();
    }

    // === Builder ===

    public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private final Config config = new Config();

        public Builder agentAddr(String addr) { config.agentAddr = addr; return this; }
        public Builder serviceName(String name) { config.serviceName = name; return this; }
        public Builder metricFlushInterval(Duration d) { config.metricFlushInterval = d; return this; }
        public Builder traceFlushInterval(Duration d) { config.traceFlushInterval = d; return this; }
        public Builder traceSampleRate(double rate) { config.traceSampleRate = rate; return this; }

        public ParytyClient build() {
            return new ParytyClient(config);
        }
    }

    // === Connection ===

    public void connect() {
        if (!config.enabled) return;

        channel = ManagedChannelBuilder.forAddress(config.agentAddr, 9090)
            .usePlaintext()
            .keepAliveTime(10, TimeUnit.SECONDS)
            .build();
        stub = SDKServiceGrpc.newBlockingStub(channel);
        connected = true;

        // Start periodic metric flush
        scheduler.scheduleAtFixedRate(
            this::flushMetrics,
            config.metricFlushInterval.toMillis(),
            config.metricFlushInterval.toMillis(),
            TimeUnit.MILLISECONDS
        );
    }

    @Override
    public void close() {
        flushMetrics();
        scheduler.shutdown();
        if (channel != null) channel.shutdown();
        connected = false;
    }

    // === Metrics ===

    public CounterBuilder counter(String name) {
        return new CounterBuilder(this, name);
    }

    public GaugeBuilder gauge(String name) {
        return new GaugeBuilder(this, name);
    }

    public HistogramBuilder histogram(String name) {
        return new HistogramBuilder(this, name);
    }

    // === Tracing ===

    public Span startSpan(String name) {
        return new Span(this, name);
    }

    // === Internal ===

    private void flushMetrics() {
        if (!connected || stub == null) return;
        // Build and send ReportMetricsRequest
    }

    // === Config ===

    static class Config {
        String agentAddr = "localhost:9090";
        String serviceName = "unknown";
        Duration metricFlushInterval = Duration.ofSeconds(10);
        Duration traceFlushInterval = Duration.ofSeconds(15);
        double traceSampleRate = 1.0;
        boolean enabled = true;
    }
}
```

### 4.3 Node.js SDK

**File:** `sdks/nodejs/src/client.ts` (~250 LOC)

```typescript
/**
 * Paryty Node.js SDK — TypeScript-first client.
 *
 * USAGE:
 *   import { ParytyClient, Counter, Gauge, Histogram } from '@paryty/sdk';
 *
 *   const client = new ParytyClient({
 *     agentAddr: 'localhost:9090',
 *     serviceName: 'my-service',
 *   });
 *   await client.connect();
 *
 *   const counter = client.counter('requests_total', { description: 'Total requests' });
 *   counter.inc();
 *
 *   const span = client.startSpan('process_order');
 *   span.setAttribute('order_id', '12345');
 *   // ... do work ...
 *   span.end();
 *
 *   await client.close();
 */

import * as grpc from '@grpc/grpc-js';
import { SDKServiceClient } from './proto/sdk_grpc_pb';
import * as messages from './proto/sdk_pb';

export interface ParytyConfig {
  agentAddr?: string;
  serviceName?: string;
  metricFlushInterval?: number;  // ms
  traceFlushInterval?: number;   // ms
  traceSampleRate?: number;      // 0.0 - 1.0
  enabled?: boolean;
  environment?: string;
}

const DEFAULT_CONFIG: Required<ParytyConfig> = {
  agentAddr: 'localhost:9090',
  serviceName: 'unknown',
  metricFlushInterval: 10_000,
  traceFlushInterval: 15_000,
  traceSampleRate: 1.0,
  enabled: true,
  environment: 'dev',
};

export class ParytyClient {
  private config: Required<ParytyConfig>;
  private channel: grpc.Channel | null = null;
  private stub: SDKServiceClient | null = null;
  private metrics: messages.CustomMetric[] = [];
  private connected = false;
  private flushTimer: NodeJS.Timeout | null = null;

  constructor(config: ParytyConfig = {}) {
    // Merge with env vars
    this.config = {
      ...DEFAULT_CONFIG,
      agentAddr: process.env.PARYTY_AGENT_ADDR ?? config.agentAddr ?? DEFAULT_CONFIG.agentAddr,
      serviceName: process.env.PARYTY_SERVICE_NAME ?? config.serviceName ?? DEFAULT_CONFIG.serviceName,
      ...config,
    };
  }

  async connect(): Promise<void> {
    if (!this.config.enabled) return;

    this.channel = new grpc.Channel(
      this.config.agentAddr,
      grpc.credentials.createInsecure(),
      {
        'grpc.keepalive_time_ms': 10_000,
        'grpc.keepalive_timeout_ms': 3_000,
      },
    );
    this.stub = new SDKServiceClient(this.config.agentAddr, grpc.credentials.createInsecure());
    this.connected = true;

    // Start periodic flush
    this.flushTimer = setInterval(() => this.flush(), this.config.metricFlushInterval);
  }

  async close(): Promise<void> {
    if (this.flushTimer) clearInterval(this.flushTimer);
    await this.flush();
    if (this.channel) this.channel.close();
    this.connected = false;
  }

  // === Metrics ===

  counter(name: string, opts: { description?: string; unit?: string } = {}): Counter {
    return new Counter(this, name, opts);
  }

  gauge(name: string, opts: { description?: string; unit?: string } = {}): Gauge {
    return new Gauge(this, name, opts);
  }

  histogram(name: string, opts: { description?: string; unit?: string; buckets?: number[] } = {}): Histogram {
    return new Histogram(this, name, opts);
  }

  // === Tracing ===

  startSpan(name: string, kind: string = 'internal'): Span {
    return new Span(this, name, kind);
  }

  // === Internal ===

  _recordMetric(metric: messages.CustomMetric): void {
    this.metrics.push(metric);
  }

  private async flush(): Promise<void> {
    if (!this.connected || !this.stub || this.metrics.length === 0) return;
    const batch = this.metrics.splice(0);
    // Send via gRPC
  }
}

export class Counter {
  private value = 0;
  constructor(private client: ParytyClient, private name: string, private opts: any) {}
  inc(delta: number = 1): void { this.value += delta; }
  get(): number { return this.value; }
}

export class Gauge {
  private value = 0;
  constructor(private client: ParytyClient, private name: string, private opts: any) {}
  set(value: number): void { this.value = value; }
  inc(delta: number = 1): void { this.value += delta; }
  dec(delta: number = 1): void { this.value -= delta; }
}

export class Histogram {
  private observations: number[] = [];
  constructor(private client: ParytyClient, private name: string, private opts: any) {}
  observe(value: number): void { this.observations.push(value); }
}

export class Span {
  private attributes: Record<string, string> = {};
  private startTime = Date.now();
  private endTime: number | null = null;

  constructor(private client: ParytyClient, private name: string, private kind: string) {}

  setAttribute(key: string, value: string): void {
    this.attributes[key] = value;
  }

  addEvent(name: string, attributes?: Record<string, string>): void {
    // Record event
  }

  setError(message: string): void {
    this.attributes['error'] = message;
  }

  end(): void {
    this.endTime = Date.now();
    // Record to client
  }
}

// Express middleware
export function expressMiddleware(client: ParytyClient) {
  return (req: any, res: any, next: any) => {
    const span = client.startSpan(`${req.method} ${req.path}`, 'server');
    span.setAttribute('http.method', req.method);
    span.setAttribute('http.url', req.originalUrl);

    res.on('finish', () => {
      span.setAttribute('http.status_code', String(res.statusCode));
      span.end();
    });

    next();
  };
}
```

---

## 5. Verification Gates

### Gate 1: Go SDK OTel Bridge (Automated)

```go
// Test: OTel metrics bridge to Paryty
// 1. Create OTel bridge with Paryty client
// 2. Create OTel counter, add 10
// 3. Wait for export interval
// 4. Assert: Paryty agent received metric with value 10

// Test: OTel trace bridge to Paryty
// 1. Create OTel bridge
// 2. Create OTel span, add attributes
// 3. End span
// 4. Assert: Paryty agent received trace with attributes

// Test: Environment config
// 1. Set PARYTY_AGENT_ADDR=test:9090
// 2. Create client from env
// 3. Assert: client.AgentAddr == "test:9090"
```

### Gate 2: Go SDK Middleware (Automated)

```go
// Test: HTTP middleware creates span
// 1. Create HTTP handler with Paryty middleware
// 2. Send HTTP request
// 3. Assert: span created with method, path, status

// Test: gRPC interceptor creates span
// 1. Create gRPC server with Paryty interceptor
// 2. Send gRPC request
// 3. Assert: span created with method, status

// Test: Metrics recorded
// 1. Send 10 HTTP requests
// 2. Assert: http_requests_total == 10
// 3. Assert: http_request_duration_seconds has 10 observations
```

### Gate 3: Python SDK (Automated)

```python
# Test: Client connects and sends metrics
# 1. Create ParytyClient
# 2. Create counter, increment 5
# 3. Flush
# 4. Assert: agent received metric

# Test: Context manager span
# 1. Create client
# 2. Use start_span context manager
# 3. Set attributes
# 4. Assert: span recorded with attributes

# Test: Django middleware
# 1. Create Django app with Paryty middleware
# 2. Send HTTP request
# 3. Assert: span created
```

### Gate 4: SDK Cross-Language Parity (Automated)

```
# Test: All SDKs produce identical proto messages
# 1. Create counter in Go, Python, Java, Node.js
# 2. Increment by 10
# 3. Serialize to proto
# 4. Assert: all produce identical ReportMetricsRequest
```

---

## 6. Performance Targets

| Metric | Target | Measurement |
|--------|--------|-------------|
| SDK overhead per request | < 0.1ms | HTTP middleware added latency |
| Metric flush latency | < 10ms | Time to send 100 metrics |
| Memory per metric | < 100 bytes | Counter/Gauge/Histogram |
| Max buffered metrics | 10,000 | Per client |
| Max buffered spans | 10,000 | Per client |
| Reconnection time | < 5s | After agent restart |
| Proto serialization | < 1ms | Per metric batch |

---

## 7. Contingency & Rollback

### 7.1 Rollback Strategy

**Scenario 1: OTel bridge causes performance issues**
- Disable OTel bridge (config.Enabled = false)
- Use Paryty SDK directly without OTel
- No OTel dependency needed

**Scenario 2: Multi-language SDKs have bugs**
- Fall back to raw proto-generated stubs
- Users can always use gRPC directly

**Scenario 3: Package manager publishing fails**
- Users can import directly from monorepo
- `go get github.com/paryty/paryty-v1.0/agent/go_sdk`

---

## 8. Appendices

### Appendix A: Files to Create/Modify

| File | Action | LOC | Language |
|------|--------|-----|----------|
| `agent/go_sdk/paryty/otel_bridge.go` | CREATE | ~300 | Go |
| `agent/go_sdk/paryty/config.go` | CREATE | ~200 | Go |
| `agent/go_sdk/paryty/middleware.go` | CREATE | ~400 | Go |
| `agent/go_sdk/paryty/sampling.go` | CREATE | ~150 | Go |
| `agent/go_sdk/paryty/client.go` | ENHANCE | +100 | Go |
| `agent/go_sdk/paryty/examples_test.go` | CREATE | ~200 | Go |
| `agent/go_sdk/examples/http_server/main.go` | CREATE | ~100 | Go |
| `agent/go_sdk/examples/grpc_server/main.go` | CREATE | ~100 | Go |
| `agent/go_sdk/examples/worker/main.go` | CREATE | ~100 | Go |
| `agent/go_sdk/go.mod` | UPDATE | +10 | Go |
| `sdks/python/paryty/client.py` | CREATE | ~300 | Python |
| `sdks/python/paryty/metrics.py` | CREATE | ~150 | Python |
| `sdks/python/paryty/tracing.py` | CREATE | ~100 | Python |
| `sdks/python/paryty/health.py` | CREATE | ~100 | Python |
| `sdks/python/paryty/middleware/django.py` | CREATE | ~50 | Python |
| `sdks/python/paryty/middleware/flask.py` | CREATE | ~50 | Python |
| `sdks/python/paryty/middleware/fastapi.py` | CREATE | ~50 | Python |
| `sdks/python/pyproject.toml` | CREATE | ~40 | Config |
| `sdks/java/src/main/java/io/paryty/ParytyClient.java` | CREATE | ~300 | Java |
| `sdks/java/src/main/java/io/paryty/Metrics.java` | CREATE | ~200 | Java |
| `sdks/java/src/main/java/io/paryty/middleware/ServletFilter.java` | CREATE | ~100 | Java |
| `sdks/java/src/main/java/io/paryty/middleware/GrpcInterceptor.java` | CREATE | ~100 | Java |
| `sdks/java/pom.xml` | CREATE | ~80 | Config |
| `sdks/nodejs/src/client.ts` | CREATE | ~250 | TypeScript |
| `sdks/nodejs/src/metrics.ts` | CREATE | ~150 | TypeScript |
| `sdks/nodejs/src/tracing.ts` | CREATE | ~100 | TypeScript |
| `sdks/nodejs/src/middleware/express.ts` | CREATE | ~50 | TypeScript |
| `sdks/nodejs/src/middleware/grpc.ts` | CREATE | ~50 | TypeScript |
| `sdks/nodejs/package.json` | CREATE | ~40 | Config |
| **TOTAL** | | **~4,200** | |

---

**END OF PHASE 7 HARDENED SPECIFICATION**
