package load

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Performance targets
const (
	TargetIngestionLatencyP99 = 100  // ms
	TargetQueryLatencyP99     = 500  // ms
	TargetErrorRate           = 0.1  // percent
)

// TestLoadBaseline runs the baseline load test
func TestLoadBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	results, err := runScenario(t, "baseline.yaml")
	if err != nil {
		t.Fatalf("Baseline test failed: %v", err)
	}

	validateResults(t, results, "baseline")
}

// TestLoadScale runs the scale load test
func TestLoadScale(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	results, err := runScenario(t, "scale.yaml")
	if err != nil {
		t.Fatalf("Scale test failed: %v", err)
	}

	validateResults(t, results, "scale")
}

// TestLoadBurst runs the burst traffic load test
func TestLoadBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	results, err := runScenario(t, "burst.yaml")
	if err != nil {
		t.Fatalf("Burst test failed: %v", err)
	}

	validateResults(t, results, "burst")
}

// runScenario loads and runs a test scenario
func runScenario(t *testing.T, scenarioFile string) (*TestResults, error) {
	t.Helper()

	// Load scenario config
	scenarioPath := filepath.Join("scenarios", scenarioFile)
	config, err := loadScenario(scenarioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load scenario: %w", err)
	}

	// Create simulator
	simulator, err := NewAgentSimulator(*config)
	if err != nil {
		return nil, fmt.Errorf("failed to create simulator: %w", err)
	}
	defer simulator.Close()

	// Run test
	ctx := context.Background()
	results, err := simulator.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("test failed: %w", err)
	}

	// Print results
	printResults(t, config.Name, results)

	return results, nil
}

// loadScenario loads a scenario config from a YAML file
func loadScenario(path string) (*ScenarioConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := &ScenarioConfig{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

// validateResults validates test results against performance targets
func validateResults(t *testing.T, results *TestResults, scenarioName string) {
	t.Helper()

	// Check error rate
	errorRate := results.GetErrorRate()
	if errorRate > TargetErrorRate {
		t.Errorf("Error rate %.2f%% exceeds target %.2f%%", errorRate, TargetErrorRate)
	}

	// Check P99 latency
	p99 := results.GetPercentile(99)
	if p99 > TargetIngestionLatencyP99 {
		t.Errorf("P99 latency %dms exceeds target %dms", p99, TargetIngestionLatencyP99)
	}

	// Print summary
	fmt.Printf("\n=== %s Performance Summary ===\n", scenarioName)
	fmt.Printf("Total Requests: %d\n", results.TotalRequests)
	fmt.Printf("Success: %d\n", results.SuccessCount)
	fmt.Printf("Errors: %d\n", results.ErrorCount)
	fmt.Printf("Error Rate: %.2f%%\n", errorRate)
	fmt.Printf("Average Latency: %dms\n", results.GetAverageLatency())
	fmt.Printf("Min Latency: %dms\n", results.MinLatency)
	fmt.Printf("Max Latency: %dms\n", results.MaxLatency)
	fmt.Printf("P50 Latency: %dms\n", results.GetPercentile(50))
	fmt.Printf("P90 Latency: %dms\n", results.GetPercentile(90))
	fmt.Printf("P99 Latency: %dms\n", p99)
	fmt.Printf("==============================\n\n")
}

// printResults prints test results
func printResults(t *testing.T, name string, results *TestResults) {
	t.Helper()

	fmt.Printf("\n--- %s Results ---\n", name)
	fmt.Printf("  Total Requests: %d\n", results.TotalRequests)
	fmt.Printf("  Success Rate: %.2f%%\n", float64(results.SuccessCount)/float64(results.TotalRequests)*100)
	fmt.Printf("  Error Rate: %.2f%%\n", results.GetErrorRate())
	fmt.Printf("  Latency (ms): avg=%d, min=%d, max=%d\n",
		results.GetAverageLatency(), results.MinLatency, results.MaxLatency)
	fmt.Printf("  Percentiles (ms): p50=%d, p90=%d, p99=%d\n",
		results.GetPercentile(50), results.GetPercentile(90), results.GetPercentile(99))
	fmt.Printf("--- End %s ---\n\n", name)
}
