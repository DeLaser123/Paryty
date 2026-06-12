package load

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/paryty/paryty-v1.0/cluster/internal/proto"
)

// AgentSimulator simulates multiple agents sending metrics concurrently
type AgentSimulator struct {
	config       ScenarioConfig
	conn         *grpc.ClientConn
	client       pb.IngestionServiceClient
	results      *TestResults
	wg           sync.WaitGroup
	cancel       context.CancelFunc
}

// ScenarioConfig defines a load test scenario
type ScenarioConfig struct {
	Name              string        `yaml:"name"`
	NumAgents         int           `yaml:"num_agents"`
	MetricsPerAgent   int           `yaml:"metrics_per_agent"`
	Interval          time.Duration `yaml:"interval"`
	Duration          time.Duration `yaml:"duration"`
	IngestionAddr     string        `yaml:"ingestion_addr"`
	BurstMode         bool          `yaml:"burst_mode"`
	BurstSize         int           `yaml:"burst_size"`
}

// TestResults holds the results of a load test
type TestResults struct {
	TotalRequests   int64
	SuccessCount    int64
	ErrorCount      int64
	TotalLatency    int64
	MinLatency      int64
	MaxLatency      int64
	Latencies       []int64
	mu              sync.Mutex
}

// NewAgentSimulator creates a new agent simulator
func NewAgentSimulator(config ScenarioConfig) (*AgentSimulator, error) {
	conn, err := grpc.Dial(config.IngestionAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ingestion service: %w", err)
	}

	return &AgentSimulator{
		config:  config,
		conn:    conn,
		client:  pb.NewIngestionServiceClient(conn),
		results: &TestResults{MinLatency: int64(^uint64(0) >> 1)},
	}, nil
}

// Run executes the load test scenario
func (s *AgentSimulator) Run(ctx context.Context) (*TestResults, error) {
	ctx, s.cancel = context.WithTimeout(ctx, s.config.Duration)
	defer s.cancel()

	fmt.Printf("Starting load test: %s\n", s.config.Name)
	fmt.Printf("  Agents: %d\n", s.config.NumAgents)
	fmt.Printf("  Metrics per agent: %d\n", s.config.MetricsPerAgent)
	fmt.Printf("  Duration: %v\n", s.config.Duration)

	startTime := time.Now()

	// Launch agents
	for i := 0; i < s.config.NumAgents; i++ {
		s.wg.Add(1)
		go s.runAgent(ctx, i)
	}

	// Wait for all agents to complete
	s.wg.Wait()

	elapsed := time.Since(startTime)
	fmt.Printf("Load test completed in %v\n", elapsed)

	return s.results, nil
}

// runAgent simulates a single agent sending metrics
func (s *AgentSimulator) runAgent(ctx context.Context, agentID int) {
	defer s.wg.Done()

	agentIDStr := fmt.Sprintf("agent-%d-%d", agentID, time.Now().UnixNano())

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if s.config.BurstMode {
				s.sendBurst(ctx, agentIDStr)
			} else {
				s.sendMetrics(ctx, agentIDStr)
			}
			time.Sleep(s.config.Interval)
		}
	}
}

// sendMetrics sends a single batch of metrics
func (s *AgentSimulator) sendMetrics(ctx context.Context, agentID string) {
	metrics := s.generateMetrics(agentID, s.config.MetricsPerAgent)

	start := time.Now()
	_, err := s.client.IngestMetrics(ctx, &pb.MetricsBatch{
		AgentId:   agentID,
		Timestamp: time.Now().UnixMilli(),
		Metrics:   metrics,
	})
	latency := time.Since(start).Milliseconds()

	s.recordResult(latency, err)
}

// sendBurst sends a burst of metrics
func (s *AgentSimulator) sendBurst(ctx context.Context, agentID string) {
	for i := 0; i < s.config.BurstSize; i++ {
		select {
		case <-ctx.Done():
			return
		default:
			s.sendMetrics(ctx, agentID)
		}
	}
}

// generateMetrics creates realistic test metrics
func (s *AgentSimulator) generateMetrics(agentID string, count int) []*pb.Metric {
	metrics := make([]*pb.Metric, count)

	for i := 0; i < count; i++ {
		metrics[i] = &pb.Metric{
			Name:      fmt.Sprintf("cpu.usage.%d", i),
			Value:     rand.Float64() * 100,
			Unit:      "percent",
			Timestamp: time.Now().UnixMilli(),
			Labels: map[string]string{
				"host":      agentID,
				"core":      fmt.Sprintf("%d", i),
				"region":    "us-east-1",
			},
		}
	}

	return metrics
}

// recordResult records the result of a request
func (s *AgentSimulator) recordResult(latencyMs int64, err error) {
	s.results.mu.Lock()
	defer s.results.mu.Unlock()

	s.results.TotalRequests++
	s.results.TotalLatency += latencyMs
	s.results.Latencies = append(s.results.Latencies, latencyMs)

	if latencyMs < s.results.MinLatency {
		s.results.MinLatency = latencyMs
	}
	if latencyMs > s.results.MaxLatency {
		s.results.MaxLatency = latencyMs
	}

	if err != nil {
		s.results.ErrorCount++
	} else {
		s.results.SuccessCount++
	}
}

// GetPercentile returns the nth percentile latency
func (r *TestResults) GetPercentile(percentile float64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.Latencies) == 0 {
		return 0
	}

	// Sort latencies
	sorted := make([]int64, len(r.Latencies))
	copy(sorted, r.Latencies)
	sortInt64s(sorted)

	index := int(float64(len(sorted)) * percentile / 100)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// GetAverageLatency returns the average latency
func (r *TestResults) GetAverageLatency() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.TotalRequests == 0 {
		return 0
	}

	return r.TotalLatency / r.TotalRequests
}

// GetErrorRate returns the error rate as a percentage
func (r *TestResults) GetErrorRate() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.TotalRequests == 0 {
		return 0
	}

	return float64(r.ErrorCount) / float64(r.TotalRequests) * 100
}

// sortInt64s sorts a slice of int64 values
func sortInt64s(data []int64) {
	for i := 1; i < len(data); i++ {
		key := data[i]
		j := i - 1
		for j >= 0 && data[j] > key {
			data[j+1] = data[j]
			j--
		}
		data[j+1] = key
	}
}

// Close closes the simulator
func (s *AgentSimulator) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return s.conn.Close()
}
