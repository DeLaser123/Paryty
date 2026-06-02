// Package paryty provides the Go SDK for Paryty Agent.
//
// The SDK allows application services to self-report metrics, traces,
// and health status to the Paryty Agent running on the same node.
//
// Usage:
//
//	client, err := paryty.NewClient("localhost:9090")
//	if err != nil { log.Fatal(err) }
//	defer client.Close()
//
//	if err := client.Connect(ctx); err != nil { log.Fatal(err) }
//
//	counter := client.Registry().RegisterCounter("requests_total")
//	counter.Inc()
//
//	ctx, span := client.Tracer().StartSpan(ctx, "handleRequest")
//	defer span.End()
//
//	report := client.Health().Run(ctx)
package paryty

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Config holds the SDK client configuration.
type Config struct {
	// AgentAddr is the address of the Paryty Agent (default: localhost:9090).
	AgentAddr string
	// ServiceName is the name of this service (used in traces and health reports).
	ServiceName string
	// AutoReconnect enables automatic reconnection on disconnect.
	AutoReconnect bool
	// ReconnectInterval is the interval between reconnection attempts.
	ReconnectInterval time.Duration
	// MaxReconnectAttempts is the max number of reconnect attempts (0 = unlimited).
	MaxReconnectAttempts int
	// MetricFlushInterval is the interval between automatic metric flushes.
	MetricFlushInterval time.Duration
	// TraceFlushInterval is the interval between automatic trace flushes.
	TraceFlushInterval time.Duration
	// HealthCheckInterval is the interval between health checks.
	HealthCheckInterval time.Duration
	// MetricCallback is called when metrics are ready to be sent.
	MetricCallback func([]MetricSnapshot)
	// TraceCallback is called when traces are ready to be sent.
	TraceCallback func([]*Trace)
	// HealthCallback is called when health reports are ready to be sent.
	HealthCallback func(*HealthReport)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		AgentAddr:            "localhost:9090",
		ServiceName:          "unknown",
		AutoReconnect:        true,
		ReconnectInterval:    5 * time.Second,
		MaxReconnectAttempts: 0,
		MetricFlushInterval:  10 * time.Second,
		TraceFlushInterval:   15 * time.Second,
		HealthCheckInterval:  30 * time.Second,
	}
}

// Client represents a Paryty SDK client.
type Client struct {
	config    Config
	conn      *grpc.ClientConn
	mu        sync.RWMutex
	connected bool
	registry  *Registry
	tracer    *Tracer
	health    *HealthChecker
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewClient creates a new Paryty SDK client with the given agent address.
func NewClient(agentAddr string) (*Client, error) {
	cfg := DefaultConfig()
	if agentAddr != "" {
		cfg.AgentAddr = agentAddr
	}
	return newClient(cfg)
}

// NewClientWithConfig creates a new Paryty SDK client with custom configuration.
func NewClientWithConfig(cfg Config) (*Client, error) {
	if cfg.AgentAddr == "" {
		cfg.AgentAddr = DefaultConfig().AgentAddr
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = DefaultConfig().ServiceName
	}
	if cfg.ReconnectInterval == 0 {
		cfg.ReconnectInterval = DefaultConfig().ReconnectInterval
	}
	if cfg.MetricFlushInterval == 0 {
		cfg.MetricFlushInterval = DefaultConfig().MetricFlushInterval
	}
	if cfg.TraceFlushInterval == 0 {
		cfg.TraceFlushInterval = DefaultConfig().TraceFlushInterval
	}
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = DefaultConfig().HealthCheckInterval
	}
	return newClient(cfg)
}

func newClient(cfg Config) (*Client, error) {
	c := &Client{
		config:   cfg,
		registry: NewRegistry(),
		tracer:   NewTracer(cfg.ServiceName),
		health:   NewHealthChecker(cfg.ServiceName, WithCheckInterval(cfg.HealthCheckInterval)),
	}
	return c, nil
}

// Connect establishes a connection to the Paryty Agent.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	if err := c.connectLocked(ctx); err != nil {
		return err
	}

	// Start background routines
	ctx, c.cancel = context.WithCancel(context.Background())
	c.startBackground(ctx)

	return nil
}

func (c *Client) connectLocked(ctx context.Context) error {
	conn, err := grpc.DialContext(ctx, c.config.AgentAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to agent at %s: %w", c.config.AgentAddr, err)
	}

	c.conn = conn
	c.connected = true
	return nil
}

// Close closes the connection and stops all background routines.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Stop background routines
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()

	// Flush remaining data
	c.flushAll()

	// Close connection
	if c.conn != nil {
		c.connected = false
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns whether the client is connected to the agent.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Conn returns the underlying gRPC connection.
func (c *Client) Conn() *grpc.ClientConn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

// Registry returns the metric registry for this client.
func (c *Client) Registry() *Registry {
	return c.registry
}

// Tracer returns the tracer for this client.
func (c *Client) Tracer() *Tracer {
	return c.tracer
}

// Health returns the health checker for this client.
func (c *Client) Health() *HealthChecker {
	return c.health
}

// Config returns the client configuration.
func (c *Client) ClientConfig() Config {
	return c.config
}

// startBackground starts all background goroutines.
func (c *Client) startBackground(ctx context.Context) {
	if c.config.AutoReconnect {
		c.wg.Add(1)
		go c.reconnectLoop(ctx)
	}

	if c.config.MetricCallback != nil {
		c.wg.Add(1)
		go c.metricFlushLoop(ctx)
	}

	if c.config.TraceCallback != nil {
		c.wg.Add(1)
		go c.traceFlushLoop(ctx)
	}

	if c.config.HealthCallback != nil {
		healthCtx := context.WithValue(ctx, struct{}{}, true)
		c.health.StartPeriodic(healthCtx, c.config.HealthCallback)
	}
}

// reconnectLoop handles automatic reconnection.
func (c *Client) reconnectLoop(ctx context.Context) {
	defer c.wg.Done()
	attempts := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.RLock()
		connected := c.connected
		c.mu.RUnlock()

		if connected {
			// Check if connection is still alive
			if c.conn != nil {
				state := c.conn.GetState()
				if state.String() == "TRANSIENT_FAILURE" || state.String() == "SHUTDOWN" {
					c.mu.Lock()
					c.connected = false
					c.mu.Unlock()
				}
			}
		}

		c.mu.RLock()
		connected = c.connected
		c.mu.RUnlock()

		if !connected {
			if c.config.MaxReconnectAttempts > 0 && attempts >= c.config.MaxReconnectAttempts {
				return
			}

			reconnectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.connectLocked(reconnectCtx)
			cancel()

			if err != nil {
				attempts++
				time.Sleep(c.config.ReconnectInterval)
				continue
			}
			attempts = 0
		}

		time.Sleep(c.config.ReconnectInterval)
	}
}

// metricFlushLoop periodically flushes metrics.
func (c *Client) metricFlushLoop(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(c.config.MetricFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snaps := c.registry.Snapshots()
			if len(snaps) > 0 && c.config.MetricCallback != nil {
				c.config.MetricCallback(snaps)
			}
		}
	}
}

// traceFlushLoop periodically flushes traces.
func (c *Client) traceFlushLoop(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(c.config.TraceFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			traces := c.tracer.ToTraces()
			if len(traces) > 0 && c.config.TraceCallback != nil {
				c.config.TraceCallback(traces)
				c.tracer.Flush()
			}
		}
	}
}

// flushAll flushes all remaining metrics and traces synchronously.
func (c *Client) flushAll() {
	if c.config.MetricCallback != nil {
		snaps := c.registry.Snapshots()
		if len(snaps) > 0 {
			c.config.MetricCallback(snaps)
		}
	}
	if c.config.TraceCallback != nil {
		traces := c.tracer.ToTraces()
		if len(traces) > 0 {
			c.config.TraceCallback(traces)
			c.tracer.Flush()
		}
	}
}

// SendMetrics sends the current metrics snapshot immediately.
func (c *Client) SendMetrics() []MetricSnapshot {
	snaps := c.registry.Snapshots()
	if c.config.MetricCallback != nil && len(snaps) > 0 {
		c.config.MetricCallback(snaps)
	}
	return snaps
}

// SendTraces sends the current trace buffer immediately.
func (c *Client) SendTraces() []*Trace {
	traces := c.tracer.ToTraces()
	if c.config.TraceCallback != nil && len(traces) > 0 {
		c.config.TraceCallback(traces)
		c.tracer.Flush()
	}
	return traces
}

// SendHealth runs health checks and sends the report immediately.
func (c *Client) SendHealth(ctx context.Context) *HealthReport {
	report := c.health.Run(ctx)
	if c.config.HealthCallback != nil {
		c.config.HealthCallback(report)
	}
	return report
}

// StartSpan is a convenience method that delegates to the tracer.
func (c *Client) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, *Span) {
	return c.tracer.StartSpan(ctx, name, opts...)
}
