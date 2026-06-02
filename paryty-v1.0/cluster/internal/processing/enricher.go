package processing

import (
	"context"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// Enricher enriches metrics with additional metadata.
type Enricher struct {
	logger *zap.Logger
}

// NewEnricher creates a new enricher.
func NewEnricher(logger *zap.Logger) *Enricher {
	return &Enricher{
		logger: logger,
	}
}

// EnrichBatch enriches a metric batch with additional metadata.
func (e *Enricher) EnrichBatch(ctx context.Context, batch *models.MetricBatch, agentInfo *models.AgentInfo) (*models.MetricBatch, error) {
	enriched := *batch

	// Add agent metadata as labels
	if agentInfo != nil {
		// Enrich CPU metrics
		for i := range enriched.CPU {
			enriched.CPU[i].AgentID = agentInfo.ID
		}

		// Enrich memory metrics
		for i := range enriched.Memory {
			enriched.Memory[i].AgentID = agentInfo.ID
		}

		// Enrich disk metrics
		for i := range enriched.Disk {
			enriched.Disk[i].AgentID = agentInfo.ID
		}

		// Enrich network metrics
		for i := range enriched.Network {
			enriched.Network[i].AgentID = agentInfo.ID
		}

		// Enrich process metrics
		for i := range enriched.Processes {
			enriched.Processes[i].AgentID = agentInfo.ID
		}
	}

	e.logger.Debug("Enriched batch",
		zap.String("agent_id", batch.AgentID),
		zap.String("hostname", agentInfo.Hostname),
	)

	return &enriched, nil
}

// EnrichMetric enriches a single metric with labels.
func (e *Enricher) EnrichMetric(ctx context.Context, metric *models.Metric, labels map[string]string) *models.Metric {
	enriched := *metric

	if enriched.Labels == nil {
		enriched.Labels = make(map[string]string)
	}

	// Merge labels
	for k, v := range labels {
		enriched.Labels[k] = v
	}

	return &enriched
}

// EnrichSpan enriches a trace span with service metadata.
func (e *Enricher) EnrichSpan(ctx context.Context, span *models.Span, agentInfo *models.AgentInfo) *models.Span {
	enriched := *span

	if enriched.Attributes == nil {
		enriched.Attributes = make(map[string]string)
	}

	// Add agent metadata
	enriched.Attributes["agent.id"] = agentInfo.ID
	enriched.Attributes["agent.hostname"] = agentInfo.Hostname
	enriched.Attributes["agent.os"] = agentInfo.OS
	enriched.Attributes["agent.arch"] = agentInfo.Arch

	// Add environment labels
	for k, v := range agentInfo.Labels {
		enriched.Attributes["label."+k] = v
	}

	return &enriched
}

// EnrichAlert enriches an alert with agent metadata.
func (e *Enricher) EnrichAlert(ctx context.Context, alert *models.Alert, agentInfo *models.AgentInfo) *models.Alert {
	enriched := *alert

	if enriched.Labels == nil {
		enriched.Labels = make(map[string]string)
	}

	// Add agent metadata
	enriched.Labels["agent_id"] = agentInfo.ID
	enriched.Labels["hostname"] = agentInfo.Hostname

	// Add environment labels
	for k, v := range agentInfo.Labels {
		enriched.Labels[k] = v
	}

	return &enriched
}

// EnrichTopology enriches topology nodes with agent metadata.
func (e *Enricher) EnrichTopology(ctx context.Context, node *models.TopologyNode, agentInfo *models.AgentInfo) *models.TopologyNode {
	enriched := *node

	if enriched.Labels == nil {
		enriched.Labels = make(map[string]string)
	}

	if enriched.Metadata == nil {
		enriched.Metadata = make(map[string]string)
	}

	// Add agent metadata
	enriched.Labels["agent_id"] = agentInfo.ID
	enriched.Labels["hostname"] = agentInfo.Hostname

	// Add metadata
	enriched.Metadata["os"] = agentInfo.OS
	enriched.Metadata["arch"] = agentInfo.Arch
	enriched.Metadata["agent_version"] = agentInfo.AgentVersion

	// Add environment labels
	for k, v := range agentInfo.Labels {
		enriched.Labels[k] = v
	}

	return &enriched
}
