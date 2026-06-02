package processing

import (
	"context"
	"fmt"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// Correlator correlates metrics across different sources.
type Correlator struct {
	logger *zap.Logger
}

// NewCorrelator creates a new correlator.
func NewCorrelator(logger *zap.Logger) *Correlator {
	return &Correlator{
		logger: logger,
	}
}

// Correlate correlates metrics and builds dependency relationships.
func (c *Correlator) Correlate(ctx context.Context, batch *models.MetricBatch, networkEvents []models.NetworkEvent) (*CorrelationResult, error) {
	result := &CorrelationResult{
		AgentID:   batch.AgentID,
		Timestamp: time.Now(),
	}

	// Build process-to-service mapping
	processServices := c.mapProcessToServices(batch)
	result.ProcessServices = processServices

	// Build network dependencies from network events
	dependencies := c.extractDependencies(networkEvents)
	result.Dependencies = dependencies

	// Detect topology changes
	changes := c.detectTopologyChanges(batch, networkEvents)
	result.TopologyChanges = changes

	c.logger.Debug("Correlated data",
		zap.String("agent_id", batch.AgentID),
		zap.Int("process_services", len(processServices)),
		zap.Int("dependencies", len(dependencies)),
		zap.Int("topology_changes", len(changes)),
	)

	return result, nil
}

// CorrelationResult contains the result of correlation.
type CorrelationResult struct {
	AgentID         string                  `json:"agent_id"`
	Timestamp       time.Time               `json:"timestamp"`
	ProcessServices map[string]string       `json:"process_services"`
	Dependencies    []Dependency            `json:"dependencies"`
	TopologyChanges []models.TopologyChange `json:"topology_changes"`
}

// Dependency represents a dependency between services.
type Dependency struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Protocol  string `json:"protocol"`
	Port      uint32 `json:"port"`
	Frequency int    `json:"frequency"`
}

func (c *Correlator) mapProcessToServices(batch *models.MetricBatch) map[string]string {
	result := make(map[string]string)

	for _, proc := range batch.Processes {
		// Map process names to service names
		serviceName := c.inferServiceName(proc.Name)
		if serviceName != "" {
			key := fmt.Sprintf("%s:%d", proc.Name, proc.PID)
			result[key] = serviceName
		}
	}

	return result
}

func (c *Correlator) inferServiceName(processName string) string {
	// Common service name mappings
	serviceMap := map[string]string{
		"nginx":    "nginx",
		"postgres": "postgresql",
		"redis":    "redis",
		"mongod":   "mongodb",
		"node":     "nodejs",
		"python":   "python",
		"java":     "java",
		"go":       "golang",
	}

	for prefix, service := range serviceMap {
		if len(processName) >= len(prefix) && processName[:len(prefix)] == prefix {
			return service
		}
	}

	return processName
}

func (c *Correlator) extractDependencies(events []models.NetworkEvent) []Dependency {
	depMap := make(map[string]*Dependency)

	for _, event := range events {
		for _, tcp := range event.TCP {
			key := fmt.Sprintf("%s:%d", tcp.DstIP, tcp.DstPort)
			if dep, ok := depMap[key]; ok {
				dep.Frequency++
			} else {
				depMap[key] = &Dependency{
					Target:    tcp.DstIP,
					Port:      tcp.DstPort,
					Protocol:  "tcp",
					Frequency: 1,
				}
			}
		}

		for _, http := range event.HTTP {
			key := http.Host
			if dep, ok := depMap[key]; ok {
				dep.Frequency++
			} else {
				depMap[key] = &Dependency{
					Target:    http.Host,
					Protocol:  "http",
					Frequency: 1,
				}
			}
		}
	}

	result := make([]Dependency, 0, len(depMap))
	for _, dep := range depMap {
		result = append(result, *dep)
	}

	return result
}

func (c *Correlator) detectTopologyChanges(batch *models.MetricBatch, events []models.NetworkEvent) []models.TopologyChange {
	var changes []models.TopologyChange

	// Detect new connections
	for _, event := range events {
		for _, tcp := range event.TCP {
			if tcp.State == "SYN_SENT" || tcp.State == "SYN_RECV" {
				changes = append(changes, models.TopologyChange{
					Type: "edge_added",
					Edge: &models.TopologyEdge{
						SourceID: batch.AgentID,
						TargetID: tcp.DstIP,
						Type:     models.EdgeTypeTCP,
						Protocol: "tcp",
					},
					Timestamp: time.Now(),
				})
			}
		}
	}

	return changes
}
