// Synthetic Data Generator for Paryty v1.0
//
// Generates realistic observability data and injects it directly into Redpanda,
// bypassing the agent entirely. This validates the cluster pipeline without needing
// a Linux VM.
//
// Usage:
//
//	go run tests/synthetic/main.go --dry-run                    # Print sample data
//	go run tests/synthetic/main.go --brokers localhost:9092     # Send to Redpanda
//	go run tests/synthetic/main.go --scenario cpu-spike         # Run specific scenario
//	go run tests/synthetic/main.go --agents 5 --interval 5s     # 5 agents, 5s interval
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

// --- Data types matching cluster/internal/models (JSON-serialized) ---

// AgentDef defines a simulated agent.
type AgentDef struct {
	ID       string
	Hostname string
	IP       string
	OS       string
	Arch     string
	Version  string
}

// MetricBatchData matches models.MetricBatch.
type MetricBatchData struct {
	AgentID   string        `json:"agent_id"`
	Timestamp time.Time     `json:"timestamp"`
	CPU       []CPUData     `json:"cpu,omitempty"`
	Memory    []MemoryData  `json:"memory,omitempty"`
	Disk      []DiskData    `json:"disk,omitempty"`
	Network   []NetworkData `json:"network,omitempty"`
	Processes []ProcessData `json:"processes,omitempty"`
}

type CPUData struct {
	AgentID         string    `json:"agent_id"`
	Timestamp       time.Time `json:"timestamp"`
	TotalUsagePct   float64   `json:"total_usage_percent"`
	PerCorePct      []float64 `json:"per_core_percent"`
	LoadAvg1m       float64   `json:"load_average_1m"`
	LoadAvg5m       float64   `json:"load_average_5m"`
	LoadAvg15m      float64   `json:"load_average_15m"`
	FrequencyMHz    float64   `json:"frequency_mhz"`
	ContextSwitches uint64    `json:"context_switches"`
}

type MemoryData struct {
	AgentID        string    `json:"agent_id"`
	Timestamp      time.Time `json:"timestamp"`
	TotalBytes     uint64    `json:"total_bytes"`
	UsedBytes      uint64    `json:"used_bytes"`
	AvailableBytes uint64    `json:"available_bytes"`
	CachedBytes    uint64    `json:"cached_bytes"`
	SwapTotalBytes uint64    `json:"swap_total_bytes"`
	SwapUsedBytes  uint64    `json:"swap_used_bytes"`
}

type DiskData struct {
	AgentID          string    `json:"agent_id"`
	Timestamp        time.Time `json:"timestamp"`
	Device           string    `json:"device"`
	MountPoint       string    `json:"mount_point"`
	TotalBytes       uint64    `json:"total_bytes"`
	UsedBytes        uint64    `json:"used_bytes"`
	ReadBytesPerSec  uint64    `json:"read_bytes_per_sec"`
	WriteBytesPerSec uint64    `json:"write_bytes_per_sec"`
	IOPSRead         uint64    `json:"iops_read"`
	IOPSWrite        uint64    `json:"iops_write"`
}

type NetworkData struct {
	AgentID       string    `json:"agent_id"`
	Timestamp     time.Time `json:"timestamp"`
	Interface     string    `json:"interface"`
	RxBytesPerSec uint64    `json:"rx_bytes_per_sec"`
	TxBytesPerSec uint64    `json:"tx_bytes_per_sec"`
	RxPackets     uint64    `json:"rx_packets"`
	TxPackets     uint64    `json:"tx_packets"`
	Errors        uint64    `json:"errors"`
}

type ProcessData struct {
	AgentID     string    `json:"agent_id"`
	Timestamp   time.Time `json:"timestamp"`
	PID         uint32    `json:"pid"`
	Name        string    `json:"name"`
	CPUUsagePct float64   `json:"cpu_usage_percent"`
	MemoryBytes uint64    `json:"memory_bytes"`
	Threads     uint32    `json:"threads"`
}

// NetworkEventData matches models.NetworkEvent.
type NetworkEventData struct {
	AgentID   string     `json:"agent_id"`
	Timestamp time.Time  `json:"timestamp"`
	TCP       []TCPData  `json:"tcp,omitempty"`
	DNS       []DNSData  `json:"dns,omitempty"`
	HTTP      []HTTPData `json:"http,omitempty"`
}

type TCPData struct {
	SrcIP     string `json:"src_ip"`
	DstIP     string `json:"dst_ip"`
	SrcPort   uint32 `json:"src_port"`
	DstPort   uint32 `json:"dst_port"`
	State     string `json:"state"`
	BytesSent uint64 `json:"bytes_sent"`
	BytesRecv uint64 `json:"bytes_received"`
}

type DNSData struct {
	Query     string `json:"query"`
	Response  string `json:"response"`
	LatencyMs uint64 `json:"latency_ms"`
	RCode     uint32 `json:"rcode"`
}

type HTTPData struct {
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    uint32 `json:"status"`
	LatencyMs uint64 `json:"latency_ms"`
	Host      string `json:"host"`
}

// SpanData matches models.Span.
type SpanData struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	ServiceName  string            `json:"service_name"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	Duration     time.Duration     `json:"duration"`
	Status       string            `json:"status"`
	Attributes   map[string]string `json:"attributes"`
}

// EventData matches models.Event.
type EventData struct {
	ID          string            `json:"id"`
	AgentID     string            `json:"agent_id"`
	Source      string            `json:"source"`
	Category    string            `json:"category"`
	Severity    string            `json:"severity"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
}

// Topic helpers generate tenant-prefixed topic names.
func topicMetricsRaw(tenant string) string    { return "paryty." + tenant + ".metrics.raw" }
func topicTraces(tenant string) string        { return "paryty." + tenant + ".traces" }
func topicEvents(tenant string) string        { return "paryty." + tenant + ".events" }
func topicNetworkEvents(tenant string) string { return "paryty." + tenant + ".network.events" }

func main() {
	// CLI flags
	brokers := flag.String("brokers", "localhost:19092", "Comma-separated Redpanda broker addresses")
	agentsCount := flag.Int("agents", 3, "Number of agents to simulate (1-10)")
	interval := flag.Duration("interval", 10*time.Second, "Interval between data points")
	scenarioName := flag.String("scenario", "steady-state", "Scenario to run: steady-state, cpu-spike, network-storm, service-discovery, alert-trigger, all")
	dryRun := flag.Bool("dry-run", false, "Print sample data without sending to Redpanda")
	tenant := flag.String("tenant", "default", "Tenant ID for multi-tenant routing (key format: tenant:agent)")
	flag.Parse()

	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Generate agent definitions
	agents := generateAgents(*agentsCount)
	logger.Info("Synthetic data generator starting",
		zap.Int("agents", len(agents)),
		zap.Duration("interval", *interval),
		zap.String("scenario", *scenarioName),
		zap.Bool("dry_run", *dryRun),
	)

	// Find the scenario
	var scenarios []Scenario
	if *scenarioName == "all" {
		scenarios = AllScenarios()
	} else {
		found := false
		for _, s := range AllScenarios() {
			if s.Name == *scenarioName {
				scenarios = []Scenario{s}
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "Unknown scenario: %s\nAvailable: steady-state, cpu-spike, network-storm, service-discovery, alert-trigger, all\n", *scenarioName)
			os.Exit(1)
		}
	}

	if *dryRun {
		runDryRun(scenarios, agents)
		return
	}

	// Connect to Redpanda
	brokerList := strings.Split(*brokers, ",")
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokerList...),
		kgo.ClientID("paryty-synthetic-generator"),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(5*time.Millisecond),
	)
	if err != nil {
		logger.Fatal("Failed to connect to Redpanda", zap.Error(err))
	}
	defer client.Close()

	logger.Info("Connected to Redpanda", zap.Strings("brokers", brokerList))

	// Run scenarios
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("Shutting down...")
		cancel()
	}()

	for _, scenario := range scenarios {
		logger.Info("Running scenario", zap.String("name", scenario.Name), zap.String("description", scenario.Description))

		for tick := 0; tick < scenario.TotalTicks; tick++ {
			select {
			case <-ctx.Done():
				logger.Info("Context cancelled, stopping")
				return
			default:
			}

			dataPoints := scenario.Generate(tick, agents)
			msgCount := publishDataPoints(ctx, client, logger, *tenant, dataPoints)
			logger.Info("Tick completed",
				zap.String("scenario", scenario.Name),
				zap.Int("tick", tick),
				zap.Int("messages", msgCount),
			)

			if tick < scenario.TotalTicks-1 {
				time.Sleep(*interval)
			}
		}

		logger.Info("Scenario completed", zap.String("name", scenario.Name))
	}

	logger.Info("All scenarios completed")
}

// generateAgents creates simulated agent definitions.
func generateAgents(count int) []AgentDef {
	if count < 1 {
		count = 1
	}
	if count > 10 {
		count = 10
	}

	hostnames := []string{"web-prod-01", "api-prod-02", "db-prod-03", "cache-prod-04", "worker-prod-05",
		"web-staging-01", "api-staging-02", "db-staging-03", "cache-staging-04", "worker-staging-05"}
	oses := []string{"ubuntu-22.04", "debian-12", "centos-9", "ubuntu-24.04", "rocky-9"}
	ips := []string{"10.0.1.10", "10.0.1.11", "10.0.1.12", "10.0.1.13", "10.0.1.14",
		"10.0.2.10", "10.0.2.11", "10.0.2.12", "10.0.2.13", "10.0.2.14"}

	agents := make([]AgentDef, count)
	for i := 0; i < count; i++ {
		agents[i] = AgentDef{
			ID:       fmt.Sprintf("agent-%03d", i+1),
			Hostname: hostnames[i],
			IP:       ips[i],
			OS:       oses[i%len(oses)],
			Arch:     "x86_64",
			Version:  "1.0.0",
		}
	}
	return agents
}

// publishDataPoints sends all data points to the appropriate Redpanda topics.
// The message key uses "tenant:agent" format for multi-tenant routing.
func publishDataPoints(ctx context.Context, client *kgo.Client, logger *zap.Logger, tenant string, points []AgentDataPoint) int {
	count := 0
	for _, dp := range points {
		key := tenant + ":" + dp.AgentID

		// Publish metrics to paryty.{tenant}.metrics.raw
		if err := publishJSON(ctx, client, topicMetricsRaw(tenant), key, dp.Metrics); err != nil {
			logger.Error("Failed to publish metrics", zap.String("agent_id", dp.AgentID), zap.Error(err))
		} else {
			count++
		}

		// Publish network events to paryty.{tenant}.network.events
		if len(dp.NetworkEvents.TCP) > 0 || len(dp.NetworkEvents.DNS) > 0 || len(dp.NetworkEvents.HTTP) > 0 {
			if err := publishJSON(ctx, client, topicNetworkEvents(tenant), key, dp.NetworkEvents); err != nil {
				logger.Error("Failed to publish network events", zap.String("agent_id", dp.AgentID), zap.Error(err))
			} else {
				count++
			}
		}

		// Publish spans to paryty.{tenant}.traces
		if len(dp.Spans) > 0 {
			for _, span := range dp.Spans {
				if err := publishJSON(ctx, client, topicTraces(tenant), key, span); err != nil {
					logger.Error("Failed to publish span", zap.String("agent_id", dp.AgentID), zap.Error(err))
				} else {
					count++
				}
			}
		}

		// Publish events to paryty.{tenant}.events
		for _, evt := range dp.Events {
			if err := publishJSON(ctx, client, topicEvents(tenant), key, evt); err != nil {
				logger.Error("Failed to publish event", zap.String("agent_id", dp.AgentID), zap.Error(err))
			} else {
				count++
			}
		}
	}
	return count
}

// publishJSON marshals a value to JSON and publishes to a Redpanda topic.
func publishJSON(ctx context.Context, client *kgo.Client, topic, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: data,
	}

	return client.ProduceSync(ctx, record).FirstErr()
}

// runDryRun prints sample data without sending to Redpanda.
func runDryRun(scenarios []Scenario, agents []AgentDef) {
	r := rand.New(rand.NewSource(42))
	_ = r

	for _, scenario := range scenarios {
		fmt.Printf("\n=== Scenario: %s ===\n", scenario.Name)
		fmt.Printf("Description: %s\n", scenario.Description)
		fmt.Printf("Total ticks: %d\n", scenario.TotalTicks)

		// Generate first 3 ticks as samples
		for tick := 0; tick < 3 && tick < scenario.TotalTicks; tick++ {
			dataPoints := scenario.Generate(tick, agents)
			fmt.Printf("\n--- Tick %d ---\n", tick)

			for _, dp := range dataPoints {
				fmt.Printf("\nAgent: %s\n", dp.AgentID)

				// Print metrics summary
				if len(dp.Metrics.CPU) > 0 {
					fmt.Printf("  CPU: %.1f%%\n", dp.Metrics.CPU[0].TotalUsagePct)
				}
				if len(dp.Metrics.Memory) > 0 {
					usedGB := float64(dp.Metrics.Memory[0].UsedBytes) / (1024 * 1024 * 1024)
					totalGB := float64(dp.Metrics.Memory[0].TotalBytes) / (1024 * 1024 * 1024)
					fmt.Printf("  Memory: %.1f GB / %.1f GB (%.1f%%)\n", usedGB, totalGB, float64(dp.Metrics.Memory[0].UsedBytes)/float64(dp.Metrics.Memory[0].TotalBytes)*100)
				}
				if len(dp.Metrics.Disk) > 0 {
					usedGB := float64(dp.Metrics.Disk[0].UsedBytes) / (1024 * 1024 * 1024)
					totalGB := float64(dp.Metrics.Disk[0].TotalBytes) / (1024 * 1024 * 1024)
					fmt.Printf("  Disk: %.1f GB / %.1f GB\n", usedGB, totalGB)
				}
				if len(dp.Metrics.Processes) > 0 {
					fmt.Printf("  Processes: %d\n", len(dp.Metrics.Processes))
					for _, p := range dp.Metrics.Processes {
						fmt.Printf("    - PID %d (%s): CPU=%.1f%% MEM=%dMB\n", p.PID, p.Name, p.CPUUsagePct, p.MemoryBytes/(1024*1024))
					}
				}
				if len(dp.NetworkEvents.TCP) > 0 {
					fmt.Printf("  TCP events: %d\n", len(dp.NetworkEvents.TCP))
				}
				if len(dp.NetworkEvents.HTTP) > 0 {
					fmt.Printf("  HTTP events: %d\n", len(dp.NetworkEvents.HTTP))
				}
				if len(dp.Spans) > 0 {
					fmt.Printf("  Spans: %d\n", len(dp.Spans))
				}
				if len(dp.Events) > 0 {
					fmt.Printf("  Events: %d\n", len(dp.Events))
					for _, evt := range dp.Events {
						fmt.Printf("    - [%s] %s: %s\n", evt.Severity, evt.Title, evt.Description)
					}
				}
			}
		}

		// Print sample JSON for the first agent, first tick
		dataPoints := scenario.Generate(0, agents)
		if len(dataPoints) > 0 {
			fmt.Printf("\n--- Sample JSON (agent %s, tick 0) ---\n", dataPoints[0].AgentID)
			sample, _ := json.MarshalIndent(dataPoints[0].Metrics, "", "  ")
			fmt.Println(string(sample))
		}
	}

	fmt.Println("\n=== Dry run complete ===")
	fmt.Println("To send data to Redpanda, run without --dry-run flag:")
	fmt.Println("  go run tests/synthetic/main.go --brokers localhost:19092")
}
