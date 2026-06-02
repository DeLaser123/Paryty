// Package main provides a synthetic data generator for testing the Paryty cluster pipeline.
// This file defines 5 test scenarios that generate realistic observability data.
package main

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

// Scenario defines a test scenario that generates a sequence of data points.
type Scenario struct {
	Name        string
	Description string
	// Generate returns the next batch of synthetic data for all agents.
	// tick is the iteration number (0-based), agents is the agent definitions.
	Generate func(tick int, agents []AgentDef) []AgentDataPoint
	// TotalTicks is how many ticks this scenario runs for.
	TotalTicks int
}

// AgentDataPoint contains all the data a single agent would report in one tick.
type AgentDataPoint struct {
	AgentID       string
	Metrics       MetricBatchData
	NetworkEvents NetworkEventData
	Spans         []SpanData
	Events        []EventData
}

// ScenarioSteadyState generates normal metrics with no anomalies.
// All agents report healthy, moderate CPU/memory, steady network traffic.
func ScenarioSteadyState() Scenario {
	return Scenario{
		Name:        "steady-state",
		Description: "Normal metrics, no anomalies. All agents healthy with moderate load.",
		TotalTicks:  60, // 10 minutes at 10s intervals
		Generate: func(tick int, agents []AgentDef) []AgentDataPoint {
			points := make([]AgentDataPoint, len(agents))
			now := time.Now()

			for i, agent := range agents {
				// Stable CPU around 25-40%
				cpuBase := 25.0 + float64(i)*5.0
				cpuJitter := math.Sin(float64(tick)*0.1+float64(i)) * 5.0

				// Stable memory around 50-65%
				memBase := 50.0 + float64(i)*5.0
				memJitter := math.Sin(float64(tick)*0.05+float64(i)) * 3.0

				totalMem := uint64(16 * 1024 * 1024 * 1024) // 16GB
				usedMem := uint64(float64(totalMem) * (memBase + memJitter) / 100.0)

				points[i] = AgentDataPoint{
					AgentID: agent.ID,
					Metrics: MetricBatchData{
						AgentID:   agent.ID,
						Timestamp: now,
						CPU: []CPUData{
							{
								AgentID:       agent.ID,
								Timestamp:     now,
								TotalUsagePct: cpuBase + cpuJitter,
								PerCorePct:    []float64{cpuBase + cpuJitter - 3, cpuBase + cpuJitter + 3, cpuBase + cpuJitter - 1, cpuBase + cpuJitter + 1},
								LoadAvg1m:     (cpuBase + cpuJitter) / 25.0,
								LoadAvg5m:     (cpuBase + cpuJitter) / 27.0,
								LoadAvg15m:    (cpuBase + cpuJitter) / 30.0,
								FrequencyMHz:  3200.0,
							},
						},
						Memory: []MemoryData{
							{
								AgentID:        agent.ID,
								Timestamp:      now,
								TotalBytes:     totalMem,
								UsedBytes:      usedMem,
								AvailableBytes: totalMem - usedMem,
								CachedBytes:    totalMem / 8,
								SwapTotalBytes: 4 * 1024 * 1024 * 1024,
								SwapUsedBytes:  0,
							},
						},
						Disk: []DiskData{
							{
								AgentID:          agent.ID,
								Timestamp:        now,
								Device:           "/dev/sda1",
								MountPoint:       "/",
								TotalBytes:       500 * 1024 * 1024 * 1024,
								UsedBytes:        200 * 1024 * 1024 * 1024,
								ReadBytesPerSec:  1024 * 100,
								WriteBytesPerSec: 1024 * 200,
								IOPSRead:         50,
								IOPSWrite:        80,
							},
						},
						Network: []NetworkData{
							{
								AgentID:       agent.ID,
								Timestamp:     now,
								Interface:     "eth0",
								RxBytesPerSec: 1024 * 500,
								TxBytesPerSec: 1024 * 300,
								RxPackets:     500,
								TxPackets:     300,
								Errors:        0,
							},
						},
						Processes: generateSteadyProcesses(agent.ID, now),
					},
					NetworkEvents: NetworkEventData{
						AgentID:   agent.ID,
						Timestamp: now,
						TCP: []TCPData{
							{SrcIP: agent.IP, DstIP: "10.0.1.10", SrcPort: uint32(40000 + tick%1000), DstPort: 8080, State: "ESTABLISHED", BytesSent: 10240, BytesRecv: 51200},
							{SrcIP: agent.IP, DstIP: "10.0.1.20", SrcPort: uint32(41000 + tick%1000), DstPort: 5432, State: "ESTABLISHED", BytesSent: 512, BytesRecv: 2048},
						},
						DNS: []DNSData{
							{Query: "api.internal.paryty.io", Response: "10.0.1.10", LatencyMs: 2, RCode: 0},
						},
						HTTP: []HTTPData{
							{Method: "GET", Path: "/api/v1/health", Status: 200, LatencyMs: 5, Host: "api.internal.paryty.io"},
						},
					},
					Spans: generateSteadySpans(agent.ID, agent.Hostname, tick, now),
					Events: []EventData{
						{
							ID:          fmt.Sprintf("evt-%s-%d", agent.ID, tick),
							AgentID:     agent.ID,
							Source:      "health-check",
							Category:    "system",
							Severity:    "info",
							Title:       "Health check passed",
							Description: "All components healthy",
							Timestamp:   now,
						},
					},
				}
			}
			return points
		},
	}
}

// ScenarioCPUSpike simulates one agent hitting 95% CPU.
func ScenarioCPUSpike() Scenario {
	return Scenario{
		Name:        "cpu-spike",
		Description: "One agent hits 95% CPU, others stay normal.",
		TotalTicks:  30,
		Generate: func(tick int, agents []AgentDef) []AgentDataPoint {
			points := make([]AgentDataPoint, len(agents))
			now := time.Now()

			for i, agent := range agents {
				cpuBase := 25.0 + float64(i)*5.0
				memBase := 50.0 + float64(i)*5.0

				// First agent gets the CPU spike
				if i == 0 {
					// Ramp up from tick 0-10, stay high 10-20, ramp down 20-30
					switch {
					case tick < 10:
						cpuBase = 25.0 + float64(tick)*7.0 // 25% → 88%
					case tick < 20:
						cpuBase = 95.0 // Sustained spike
					default:
						cpuBase = 95.0 - float64(tick-20)*7.0 // Ramp down
					}
					memBase = 60.0 + float64(tick)*1.0 // Memory also rises
				}

				totalMem := uint64(16 * 1024 * 1024 * 1024)
				usedMem := uint64(float64(totalMem) * memBase / 100.0)

				var processes []ProcessData
				if i == 0 {
					// Show runaway process on spiked agent
					processes = []ProcessData{
						{AgentID: agent.ID, Timestamp: now, PID: 1234, Name: "runaway-worker", CPUUsagePct: cpuBase * 0.8, MemoryBytes: 2 * 1024 * 1024 * 1024, Threads: 64},
						{AgentID: agent.ID, Timestamp: now, PID: 5678, Name: "paryty-agent", CPUUsagePct: 2.0, MemoryBytes: 100 * 1024 * 1024, Threads: 8},
					}
				} else {
					processes = generateSteadyProcesses(agent.ID, now)
				}

				points[i] = AgentDataPoint{
					AgentID: agent.ID,
					Metrics: MetricBatchData{
						AgentID:   agent.ID,
						Timestamp: now,
						CPU: []CPUData{
							{
								AgentID:       agent.ID,
								Timestamp:     now,
								TotalUsagePct: cpuBase,
								PerCorePct:    []float64{cpuBase - 2, cpuBase + 2, cpuBase - 5, cpuBase + 5},
								LoadAvg1m:     cpuBase / 10.0,
								LoadAvg5m:     cpuBase / 12.0,
								LoadAvg15m:    cpuBase / 15.0,
								FrequencyMHz:  3200.0,
							},
						},
						Memory: []MemoryData{
							{
								AgentID:        agent.ID,
								Timestamp:      now,
								TotalBytes:     totalMem,
								UsedBytes:      usedMem,
								AvailableBytes: totalMem - usedMem,
								CachedBytes:    totalMem / 8,
								SwapTotalBytes: 4 * 1024 * 1024 * 1024,
								SwapUsedBytes:  0,
							},
						},
						Network: []NetworkData{
							{AgentID: agent.ID, Timestamp: now, Interface: "eth0", RxBytesPerSec: 1024 * 500, TxBytesPerSec: 1024 * 300, RxPackets: 500, TxPackets: 300},
						},
						Processes: processes,
					},
					NetworkEvents: NetworkEventData{
						AgentID:   agent.ID,
						Timestamp: now,
						TCP: []TCPData{
							{SrcIP: agent.IP, DstIP: "10.0.1.10", SrcPort: uint32(40000 + tick%1000), DstPort: 8080, State: "ESTABLISHED", BytesSent: 10240, BytesRecv: 51200},
						},
					},
				}

				// Add warning event when CPU is high
				if i == 0 && cpuBase > 80 {
					points[i].Events = append(points[i].Events, EventData{
						ID:          fmt.Sprintf("evt-cpu-warn-%d", tick),
						AgentID:     agent.ID,
						Source:      "threshold-monitor",
						Category:    "system",
						Severity:    "warning",
						Title:       "High CPU usage detected",
						Description: fmt.Sprintf("CPU usage at %.1f%%, threshold 80%%", cpuBase),
						Timestamp:   now,
					})
				}
			}
			return points
		},
	}
}

// ScenarioNetworkStorm simulates a burst of TCP connections.
func ScenarioNetworkStorm() Scenario {
	return Scenario{
		Name:        "network-storm",
		Description: "Burst of TCP connections from one agent simulating a port scan or DDoS.",
		TotalTicks:  20,
		Generate: func(tick int, agents []AgentDef) []AgentDataPoint {
			points := make([]AgentDataPoint, len(agents))
			now := time.Now()

			for i, agent := range agents {
				cpuBase := 30.0
				totalMem := uint64(16 * 1024 * 1024 * 1024)
				usedMem := uint64(float64(totalMem) * 0.55)

				// First agent gets the network storm
				var tcpEvents []TCPData
				var rxBytes, txBytes uint64
				if i == 0 {
					connCount := 10 + tick*20 // Ramping connections
					for j := 0; j < connCount && j < 200; j++ {
						tcpEvents = append(tcpEvents, TCPData{
							SrcIP:     agent.IP,
							DstIP:     fmt.Sprintf("10.0.%d.%d", j/256, j%256),
							SrcPort:   uint32(40000 + j),
							DstPort:   uint32(80 + j%4),
							State:     "SYN_SENT",
							BytesSent: 64,
							BytesRecv: 0,
						})
					}
					rxBytes = uint64(connCount * 64)
					txBytes = uint64(connCount * 128)
				} else {
					tcpEvents = []TCPData{
						{SrcIP: agent.IP, DstIP: "10.0.1.10", SrcPort: 40000, DstPort: 8080, State: "ESTABLISHED", BytesSent: 10240, BytesRecv: 51200},
					}
					rxBytes = 1024 * 500
					txBytes = 1024 * 300
				}

				points[i] = AgentDataPoint{
					AgentID: agent.ID,
					Metrics: MetricBatchData{
						AgentID:   agent.ID,
						Timestamp: now,
						CPU: []CPUData{
							{AgentID: agent.ID, Timestamp: now, TotalUsagePct: cpuBase, PerCorePct: []float64{28, 32, 30, 30}, LoadAvg1m: 1.2, LoadAvg5m: 1.0, LoadAvg15m: 0.8, FrequencyMHz: 3200},
						},
						Memory: []MemoryData{
							{AgentID: agent.ID, Timestamp: now, TotalBytes: totalMem, UsedBytes: usedMem, AvailableBytes: totalMem - usedMem, CachedBytes: totalMem / 8},
						},
						Network: []NetworkData{
							{AgentID: agent.ID, Timestamp: now, Interface: "eth0", RxBytesPerSec: rxBytes, TxBytesPerSec: txBytes, RxPackets: rxBytes / 64, TxPackets: txBytes / 128},
						},
					},
					NetworkEvents: NetworkEventData{
						AgentID:   agent.ID,
						Timestamp: now,
						TCP:       tcpEvents,
					},
				}

				// Add security event on first agent
				if i == 0 && tick > 5 {
					points[i].Events = append(points[i].Events, EventData{
						ID:          fmt.Sprintf("evt-net-storm-%d", tick),
						AgentID:     agent.ID,
						Source:      "ebpf-monitor",
						Category:    "security",
						Severity:    "critical",
						Title:       "Network anomaly detected",
						Description: fmt.Sprintf("Unusual TCP connection burst: %d connections in interval", len(tcpEvents)),
						Timestamp:   now,
					})
				}
			}
			return points
		},
	}
}

// ScenarioServiceDiscovery simulates new containers appearing and disappearing.
func ScenarioServiceDiscovery() Scenario {
	return Scenario{
		Name:        "service-discovery",
		Description: "New containers appear and disappear, topology changes dynamically.",
		TotalTicks:  40,
		Generate: func(tick int, agents []AgentDef) []AgentDataPoint {
			points := make([]AgentDataPoint, len(agents))
			now := time.Now()

			for i, agent := range agents {
				totalMem := uint64(16 * 1024 * 1024 * 1024)
				usedMem := uint64(float64(totalMem) * 0.55)

				// Base processes
				processes := []ProcessData{
					{AgentID: agent.ID, Timestamp: now, PID: 1, Name: "systemd", CPUUsagePct: 0.1, MemoryBytes: 10 * 1024 * 1024, Threads: 1},
					{AgentID: agent.ID, Timestamp: now, PID: 100, Name: "paryty-agent", CPUUsagePct: 2.0, MemoryBytes: 100 * 1024 * 1024, Threads: 8},
				}

				// Add dynamic containers based on tick
				if tick >= 5 {
					processes = append(processes, ProcessData{AgentID: agent.ID, Timestamp: now, PID: 2001, Name: "nginx", CPUUsagePct: 1.5, MemoryBytes: 50 * 1024 * 1024, Threads: 4})
				}
				if tick >= 10 {
					processes = append(processes, ProcessData{AgentID: agent.ID, Timestamp: now, PID: 3001, Name: "api-gateway", CPUUsagePct: 8.0, MemoryBytes: 256 * 1024 * 1024, Threads: 16})
					processes = append(processes, ProcessData{AgentID: agent.ID, Timestamp: now, PID: 3002, Name: "user-service", CPUUsagePct: 5.0, MemoryBytes: 200 * 1024 * 1024, Threads: 12})
				}
				if tick >= 15 {
					processes = append(processes, ProcessData{AgentID: agent.ID, Timestamp: now, PID: 4001, Name: "postgres", CPUUsagePct: 12.0, MemoryBytes: 1 * 1024 * 1024 * 1024, Threads: 20})
				}
				if tick >= 20 {
					processes = append(processes, ProcessData{AgentID: agent.ID, Timestamp: now, PID: 5001, Name: "redis", CPUUsagePct: 3.0, MemoryBytes: 512 * 1024 * 1024, Threads: 4})
				}
				// Remove api-gateway at tick 30 (simulating crash)
				if tick >= 30 {
					filtered := processes[:0]
					for _, p := range processes {
						if p.Name != "api-gateway" {
							filtered = append(filtered, p)
						}
					}
					processes = filtered
				}

				// Network events change with services
				var tcpEvents []TCPData
				var httpEvents []HTTPData
				if tick >= 10 {
					tcpEvents = append(tcpEvents, TCPData{SrcIP: agent.IP, DstIP: "172.17.0.2", SrcPort: 40000, DstPort: 80, State: "ESTABLISHED", BytesSent: 5000, BytesRecv: 20000})
					httpEvents = append(httpEvents, HTTPData{Method: "GET", Path: "/api/users", Status: 200, LatencyMs: 12, Host: "api-gateway"})
				}
				if tick >= 15 {
					tcpEvents = append(tcpEvents, TCPData{SrcIP: "172.17.0.3", DstIP: "172.17.0.4", SrcPort: 5432, DstPort: 40001, State: "ESTABLISHED", BytesSent: 1024, BytesRecv: 4096})
				}

				var events []EventData
				// Discovery events
				if tick == 5 {
					events = append(events, EventData{ID: fmt.Sprintf("disc-nginx-%d", tick), AgentID: agent.ID, Source: "service-discovery", Category: "deployment", Severity: "info", Title: "Service discovered: nginx", Description: "New container nginx detected on port 80", Timestamp: now})
				}
				if tick == 10 {
					events = append(events, EventData{ID: fmt.Sprintf("disc-api-%d", tick), AgentID: agent.ID, Source: "service-discovery", Category: "deployment", Severity: "info", Title: "Service discovered: api-gateway", Description: "New container api-gateway detected on port 8080", Timestamp: now})
				}
				if tick == 30 {
					events = append(events, EventData{ID: fmt.Sprintf("disc-api-down-%d", tick), AgentID: agent.ID, Source: "service-discovery", Category: "deployment", Severity: "error", Title: "Service lost: api-gateway", Description: "Container api-gateway stopped responding", Timestamp: now})
				}

				points[i] = AgentDataPoint{
					AgentID: agent.ID,
					Metrics: MetricBatchData{
						AgentID:   agent.ID,
						Timestamp: now,
						CPU: []CPUData{
							{AgentID: agent.ID, Timestamp: now, TotalUsagePct: 30 + float64(len(processes))*3, PerCorePct: []float64{28, 32, 30, 30}, LoadAvg1m: 1.2, LoadAvg5m: 1.0, LoadAvg15m: 0.8, FrequencyMHz: 3200},
						},
						Memory: []MemoryData{
							{AgentID: agent.ID, Timestamp: now, TotalBytes: totalMem, UsedBytes: usedMem, AvailableBytes: totalMem - usedMem, CachedBytes: totalMem / 8},
						},
						Network: []NetworkData{
							{AgentID: agent.ID, Timestamp: now, Interface: "eth0", RxBytesPerSec: 1024 * 500, TxBytesPerSec: 1024 * 300},
						},
						Processes: processes,
					},
					NetworkEvents: NetworkEventData{
						AgentID:   agent.ID,
						Timestamp: now,
						TCP:       tcpEvents,
						HTTP:      httpEvents,
					},
					Events: events,
				}
			}
			return points
		},
	}
}

// ScenarioAlertTrigger simulates metrics breaching thresholds.
func ScenarioAlertTrigger() Scenario {
	return Scenario{
		Name:        "alert-trigger",
		Description: "Metrics breach multiple thresholds: high CPU, low disk, high memory.",
		TotalTicks:  30,
		Generate: func(tick int, agents []AgentDef) []AgentDataPoint {
			points := make([]AgentDataPoint, len(agents))
			now := time.Now()

			for i, agent := range agents {
				cpuBase := 30.0
				memPct := 55.0
				diskUsedPct := 60.0

				// First agent: CPU alert (tick 5+)
				if i == 0 && tick >= 5 {
					cpuBase = 85.0 + float64(tick-5)*1.5 // Ramp to >95%
				}
				// Second agent: Memory alert (tick 8+)
				if i == 1 && tick >= 8 {
					memPct = 85.0 + float64(tick-8)*1.0 // Ramp to >95%
				}
				// Third agent: Disk alert (tick 10+)
				if i == 2 && tick >= 10 {
					diskUsedPct = 88.0 + float64(tick-10)*0.8 // Ramp to >95%
				}

				totalMem := uint64(16 * 1024 * 1024 * 1024)
				usedMem := uint64(float64(totalMem) * memPct / 100.0)
				totalDisk := uint64(500 * 1024 * 1024 * 1024)
				usedDisk := uint64(float64(totalDisk) * diskUsedPct / 100.0)

				events := make([]EventData, 0)

				// Alert events
				if i == 0 && tick == 5 {
					events = append(events, EventData{
						ID: fmt.Sprintf("alert-cpu-%d", tick), AgentID: agent.ID, Source: "alert-manager",
						Category: "system", Severity: "warning", Title: "CPU threshold breached",
						Description: "CPU usage exceeded 80% threshold", Timestamp: now,
					})
				}
				if i == 0 && tick >= 15 {
					events = append(events, EventData{
						ID: fmt.Sprintf("alert-cpu-crit-%d", tick), AgentID: agent.ID, Source: "alert-manager",
						Category: "system", Severity: "critical", Title: "CPU critically high",
						Description: fmt.Sprintf("CPU usage at %.1f%%, system may become unresponsive", cpuBase), Timestamp: now,
					})
				}
				if i == 1 && tick == 8 {
					events = append(events, EventData{
						ID: fmt.Sprintf("alert-mem-%d", tick), AgentID: agent.ID, Source: "alert-manager",
						Category: "system", Severity: "warning", Title: "Memory threshold breached",
						Description: "Memory usage exceeded 85% threshold", Timestamp: now,
					})
				}
				if i == 2 && tick == 10 {
					events = append(events, EventData{
						ID: fmt.Sprintf("alert-disk-%d", tick), AgentID: agent.ID, Source: "alert-manager",
						Category: "system", Severity: "error", Title: "Disk space low",
						Description: "Disk usage exceeded 85% threshold", Timestamp: now,
					})
				}

				points[i] = AgentDataPoint{
					AgentID: agent.ID,
					Metrics: MetricBatchData{
						AgentID:   agent.ID,
						Timestamp: now,
						CPU: []CPUData{
							{AgentID: agent.ID, Timestamp: now, TotalUsagePct: cpuBase, PerCorePct: []float64{cpuBase - 2, cpuBase + 2, cpuBase - 5, cpuBase + 5}, LoadAvg1m: cpuBase / 10, LoadAvg5m: cpuBase / 12, LoadAvg15m: cpuBase / 15, FrequencyMHz: 3200},
						},
						Memory: []MemoryData{
							{AgentID: agent.ID, Timestamp: now, TotalBytes: totalMem, UsedBytes: usedMem, AvailableBytes: totalMem - usedMem, CachedBytes: totalMem / 16},
						},
						Disk: []DiskData{
							{AgentID: agent.ID, Timestamp: now, Device: "/dev/sda1", MountPoint: "/", TotalBytes: totalDisk, UsedBytes: usedDisk, ReadBytesPerSec: 1024 * 100, WriteBytesPerSec: 1024 * 200, IOPSRead: 50, IOPSWrite: 80},
						},
						Network: []NetworkData{
							{AgentID: agent.ID, Timestamp: now, Interface: "eth0", RxBytesPerSec: 1024 * 500, TxBytesPerSec: 1024 * 300},
						},
					},
					NetworkEvents: NetworkEventData{
						AgentID: agent.ID, Timestamp: now,
						TCP: []TCPData{{SrcIP: agent.IP, DstIP: "10.0.1.10", SrcPort: 40000, DstPort: 8080, State: "ESTABLISHED", BytesSent: 10240, BytesRecv: 51200}},
					},
					Events: events,
				}
			}
			return points
		},
	}
}

// AllScenarios returns all 5 test scenarios.
func AllScenarios() []Scenario {
	return []Scenario{
		ScenarioSteadyState(),
		ScenarioCPUSpike(),
		ScenarioNetworkStorm(),
		ScenarioServiceDiscovery(),
		ScenarioAlertTrigger(),
	}
}

// --- Helper functions ---

func generateSteadyProcesses(agentID string, now time.Time) []ProcessData {
	return []ProcessData{
		{AgentID: agentID, Timestamp: now, PID: 1, Name: "systemd", CPUUsagePct: 0.1, MemoryBytes: 10 * 1024 * 1024, Threads: 1},
		{AgentID: agentID, Timestamp: now, PID: 100, Name: "paryty-agent", CPUUsagePct: 2.0, MemoryBytes: 100 * 1024 * 1024, Threads: 8},
		{AgentID: agentID, Timestamp: now, PID: 200, Name: "containerd", CPUUsagePct: 1.5, MemoryBytes: 200 * 1024 * 1024, Threads: 12},
		{AgentID: agentID, Timestamp: now, PID: 300, Name: "sshd", CPUUsagePct: 0.05, MemoryBytes: 5 * 1024 * 1024, Threads: 2},
	}
}

func generateSteadySpans(agentID, hostname string, tick int, now time.Time) []SpanData {
	traceID := fmt.Sprintf("trace-%s-%d", agentID, tick)
	return []SpanData{
		{
			TraceID:     traceID,
			SpanID:      fmt.Sprintf("span-%s-root", agentID),
			Name:        "HTTP GET /api/v1/metrics",
			Kind:        "server",
			ServiceName: hostname,
			StartTime:   now.Add(-50 * time.Millisecond),
			EndTime:     now,
			Duration:    50 * time.Millisecond,
			Status:      "ok",
			Attributes:  map[string]string{"http.method": "GET", "http.status_code": "200", "http.path": "/api/v1/metrics"},
		},
		{
			TraceID:      traceID,
			SpanID:       fmt.Sprintf("span-%s-db", agentID),
			ParentSpanID: fmt.Sprintf("span-%s-root", agentID),
			Name:         "SELECT * FROM metrics WHERE agent_id = ?",
			Kind:         "client",
			ServiceName:  "postgres",
			StartTime:    now.Add(-40 * time.Millisecond),
			EndTime:      now.Add(-15 * time.Millisecond),
			Duration:     25 * time.Millisecond,
			Status:       "ok",
			Attributes:   map[string]string{"db.system": "postgresql", "db.statement": "SELECT * FROM metrics"},
		},
	}
}

// randomJitter returns a random float64 in [-jitter, +jitter].
func randomJitter(jitter float64, r *rand.Rand) float64 {
	return (r.Float64()*2 - 1) * jitter
}
