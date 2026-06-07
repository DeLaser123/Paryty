package timeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// DiffCalculator generates human-readable diff reports between two points in time.
//
// V2.0 Migration: Replaces the Python DiffCalculator class. The Go version
// produces structured DiffReport objects that can be serialized to JSON or
// rendered as HTML without a Python dependency.
type DiffCalculator struct {
	snapshots *SnapshotManager
	logger    *zap.Logger
}

// NewDiffCalculator creates a new diff calculator.
func NewDiffCalculator(snapshots *SnapshotManager, logger *zap.Logger) *DiffCalculator {
	return &DiffCalculator{
		snapshots: snapshots,
		logger:    logger,
	}
}

// DiffReport contains a complete diff analysis between two time points.
//
// V2.0 Migration: Replaces the Python DiffReport dataclass. The Summary field
// contains a pre-formatted human-readable summary, replacing the Python
// __str__ method.
type DiffReport struct {
	// From is the start time of the comparison.
	From time.Time `json:"from"`
	// To is the end time of the comparison.
	To time.Time `json:"to"`
	// Diff contains the structured differences.
	Diff *SnapshotDiff `json:"diff"`
	// Summary is a human-readable summary of the changes.
	Summary string `json:"summary"`
}

// CalculateDiff generates a human-readable diff report between two time points.
// It finds the nearest snapshots to each time, computes the diff, and generates
// a formatted summary.
func (dc *DiffCalculator) CalculateDiff(ctx context.Context, tenant string, from, to time.Time) (*DiffReport, error) {
	if from.After(to) {
		return nil, fmt.Errorf("from time %v is after to time %v", from, to)
	}

	dc.logger.Info("Calculating diff",
		zap.String("tenant", tenant),
		zap.Time("from", from),
		zap.Time("to", to),
	)

	// Get nearest snapshots.
	fromSnap, err := dc.snapshots.GetNearestSnapshot(ctx, tenant, from)
	if err != nil {
		return nil, fmt.Errorf("get snapshot for %v: %w", from, err)
	}

	toSnap, err := dc.snapshots.GetNearestSnapshot(ctx, tenant, to)
	if err != nil {
		return nil, fmt.Errorf("get snapshot for %v: %w", to, err)
	}

	// Use the snapshot IDs to compute the diff.
	diff, err := dc.snapshots.DiffSnapshots(ctx, tenant, fromSnap.ID, toSnap.ID)
	if err != nil {
		return nil, fmt.Errorf("diff snapshots: %w", err)
	}

	summary := dc.generateSummary(diff)

	report := &DiffReport{
		From:    from,
		To:      to,
		Diff:    diff,
		Summary: summary,
	}

	dc.logger.Info("Diff calculated",
		zap.String("tenant", tenant),
		zap.Int("nodes_added", len(diff.NodesAdded)),
		zap.Int("nodes_removed", len(diff.NodesRemoved)),
		zap.Int("metric_deltas", len(diff.MetricDeltas)),
	)

	return report, nil
}

// generateSummary produces a human-readable summary of the diff.
func (dc *DiffCalculator) generateSummary(diff *SnapshotDiff) string {
	var sb strings.Builder

	duration := diff.ToTimestamp.Sub(diff.FromTimestamp)
	sb.WriteString(fmt.Sprintf("Changes between %s and %s (%s)\n",
		diff.FromTimestamp.Format(time.RFC3339),
		diff.ToTimestamp.Format(time.RFC3339),
		duration.Round(time.Minute),
	))
	sb.WriteString(strings.Repeat("=", 60) + "\n")

	// Node changes.
	totalNodeChanges := len(diff.NodesAdded) + len(diff.NodesRemoved)
	if totalNodeChanges > 0 {
		sb.WriteString(fmt.Sprintf("\nNodes: %d added, %d removed\n", len(diff.NodesAdded), len(diff.NodesRemoved)))
		for _, n := range diff.NodesAdded {
			sb.WriteString(fmt.Sprintf("  + %s (%s) [%s]\n", n.Name, n.ID, n.Type))
		}
		for _, n := range diff.NodesRemoved {
			sb.WriteString(fmt.Sprintf("  - %s (%s) [%s]\n", n.Name, n.ID, n.Type))
		}
	} else {
		sb.WriteString("\nNodes: no changes\n")
	}

	// Edge changes.
	totalEdgeChanges := len(diff.EdgesAdded) + len(diff.EdgesRemoved)
	if totalEdgeChanges > 0 {
		sb.WriteString(fmt.Sprintf("\nEdges: %d added, %d removed\n", len(diff.EdgesAdded), len(diff.EdgesRemoved)))
		for _, e := range diff.EdgesAdded {
			sb.WriteString(fmt.Sprintf("  + %s → %s (%s)\n", e.SourceID, e.TargetID, e.Type))
		}
		for _, e := range diff.EdgesRemoved {
			sb.WriteString(fmt.Sprintf("  - %s → %s (%s)\n", e.SourceID, e.TargetID, e.Type))
		}
	} else {
		sb.WriteString("\nEdges: no changes\n")
	}

	// Metric changes.
	if len(diff.MetricDeltas) > 0 {
		sb.WriteString(fmt.Sprintf("\nMetrics: %d changed\n", len(diff.MetricDeltas)))
		for _, m := range diff.MetricDeltas {
			sign := "+"
			if m.Change < 0 {
				sign = ""
			}
			sb.WriteString(fmt.Sprintf("  %s: %.2f → %.2f (%s%.2f, %s%.1f%%)\n",
				m.Name, m.From, m.To, sign, m.Change, sign, m.ChangePct))
		}
	} else {
		sb.WriteString("\nMetrics: no significant changes\n")
	}

	// Alert changes.
	totalAlertChanges := len(diff.AlertsAdded) + len(diff.AlertsResolved)
	if totalAlertChanges > 0 {
		sb.WriteString(fmt.Sprintf("\nAlerts: %d new, %d resolved\n", len(diff.AlertsAdded), len(diff.AlertsResolved)))
		for _, a := range diff.AlertsAdded {
			sb.WriteString(fmt.Sprintf("  + [%s] %s: %s\n", a.Severity, a.Name, a.Description))
		}
		for _, a := range diff.AlertsResolved {
			sb.WriteString(fmt.Sprintf("  - [%s] %s\n", a.Severity, a.Name))
		}
	} else {
		sb.WriteString("\nAlerts: no changes\n")
	}

	// Overall summary.
	if totalNodeChanges == 0 && totalEdgeChanges == 0 && len(diff.MetricDeltas) == 0 && totalAlertChanges == 0 {
		sb.WriteString("\nNo changes detected between the two time points.\n")
	}

	return sb.String()
}
