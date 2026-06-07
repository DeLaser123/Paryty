package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ExportManager handles exporting snapshots and diffs to various formats.
//
// V2.0 Migration: Replaces the Python ExportManager class. The Go version
// generates JSON and HTML directly without Jinja2 templates, using Go's
// text/template for HTML rendering.
type ExportManager struct {
	snapshots *SnapshotManager
	diffCalc  *DiffCalculator
	logger    *zap.Logger
}

// NewExportManager creates a new export manager.
func NewExportManager(snapshots *SnapshotManager, diffCalc *DiffCalculator, logger *zap.Logger) *ExportManager {
	return &ExportManager{
		snapshots: snapshots,
		diffCalc:  diffCalc,
		logger:    logger,
	}
}

// ExportJSON exports a snapshot as a JSON byte slice.
func (em *ExportManager) ExportJSON(ctx context.Context, tenant, snapshotID string) ([]byte, error) {
	snap, err := em.snapshots.GetNearestSnapshot(ctx, tenant, time.Now())
	if err != nil {
		// Try direct get if nearest fails.
		return nil, fmt.Errorf("get snapshot: %w", err)
	}

	// If a specific ID was requested, try to get that one.
	if snapshotID != "" {
		store := em.snapshots.store
		specific, err := store.GetSnapshot(ctx, tenant, snapshotID)
		if err == nil {
			snap = specific
		}
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}

	em.logger.Info("Snapshot exported as JSON",
		zap.String("tenant", tenant),
		zap.String("snapshot_id", snap.ID),
		zap.Int("bytes", len(data)),
	)

	return data, nil
}

// ExportDiffJSON exports a diff report as a JSON byte slice.
func (em *ExportManager) ExportDiffJSON(ctx context.Context, tenant string, from, to time.Time) ([]byte, error) {
	report, err := em.diffCalc.CalculateDiff(ctx, tenant, from, to)
	if err != nil {
		return nil, fmt.Errorf("calculate diff: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal diff report: %w", err)
	}

	em.logger.Info("Diff exported as JSON",
		zap.String("tenant", tenant),
		zap.Time("from", from),
		zap.Time("to", to),
		zap.Int("bytes", len(data)),
	)

	return data, nil
}

// ExportHTML generates an interactive HTML timeline report.
func (em *ExportManager) ExportHTML(ctx context.Context, tenant string, from, to time.Time) ([]byte, error) {
	report, err := em.diffCalc.CalculateDiff(ctx, tenant, from, to)
	if err != nil {
		return nil, fmt.Errorf("calculate diff: %w", err)
	}

	html := em.renderHTML(report, tenant)

	em.logger.Info("Timeline report exported as HTML",
		zap.String("tenant", tenant),
		zap.Time("from", from),
		zap.Time("to", to),
		zap.Int("bytes", len(html)),
	)

	return []byte(html), nil
}

// renderHTML generates an HTML report from a diff report.
func (em *ExportManager) renderHTML(report *DiffReport, tenant string) string {
	diff := report.Diff
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Paryty Timeline Report</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 20px; background: #0d1117; color: #c9d1d9; }
  .header { border-bottom: 1px solid #30363d; padding-bottom: 16px; margin-bottom: 24px; }
  .header h1 { color: #58a6ff; margin: 0 0 8px 0; }
  .header .meta { color: #8b949e; font-size: 14px; }
  .section { margin-bottom: 24px; }
  .section h2 { color: #58a6ff; font-size: 18px; margin-bottom: 12px; }
  .card { background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 16px; margin-bottom: 12px; }
  .added { color: #3fb950; }
  .removed { color: #f85149; }
  .changed { color: #d29922; }
  table { width: 100%; border-collapse: collapse; }
  th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid #21262d; }
  th { color: #8b949e; font-weight: 600; }
  .summary { background: #1c2128; padding: 16px; border-radius: 6px; font-family: monospace; white-space: pre-wrap; font-size: 13px; line-height: 1.5; }
  .badge { display: inline-block; padding: 2px 8px; border-radius: 12px; font-size: 12px; font-weight: 600; }
  .badge-add { background: #238636; color: #fff; }
  .badge-remove { background: #da3633; color: #fff; }
  .badge-info { background: #1f6feb; color: #fff; }
</style>
</head>
<body>
<div class="header">
  <h1>Paryty Timeline Report</h1>
  <div class="meta">Tenant: `)
	sb.WriteString(tenant)
	sb.WriteString(` | From: `)
	sb.WriteString(report.From.Format(time.RFC3339))
	sb.WriteString(` | To: `)
	sb.WriteString(report.To.Format(time.RFC3339))
	sb.WriteString(`</div>
</div>
`)

	// Node changes section.
	sb.WriteString(`<div class="section"><h2>Node Changes</h2>`)
	if len(diff.NodesAdded) > 0 || len(diff.NodesRemoved) > 0 {
		sb.WriteString(`<div class="card"><table><thead><tr><th>Action</th><th>Name</th><th>Type</th><th>ID</th></tr></thead><tbody>`)
		for _, n := range diff.NodesAdded {
			sb.WriteString(fmt.Sprintf(`<tr><td><span class="badge badge-add">+</span></td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				n.Name, string(n.Type), n.ID))
		}
		for _, n := range diff.NodesRemoved {
			sb.WriteString(fmt.Sprintf(`<tr><td><span class="badge badge-remove">-</span></td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				n.Name, string(n.Type), n.ID))
		}
		sb.WriteString(`</tbody></table></div>`)
	} else {
		sb.WriteString(`<p>No node changes detected.</p>`)
	}
	sb.WriteString(`</div>`)

	// Metric changes section.
	sb.WriteString(`<div class="section"><h2>Metric Changes</h2>`)
	if len(diff.MetricDeltas) > 0 {
		sb.WriteString(`<div class="card"><table><thead><tr><th>Metric</th><th>From</th><th>To</th><th>Change</th><th>Change %</th></tr></thead><tbody>`)
		for _, m := range diff.MetricDeltas {
			cls := "changed"
			if m.Change > 0 {
				cls = "removed" // Higher utilization is "bad"
			} else if m.Change < 0 {
				cls = "added" // Lower utilization is "good"
			}
			sb.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%.2f</td><td>%.2f</td><td class="%s">%.2f</td><td class="%s">%.1f%%</td></tr>`,
				m.Name, m.From, m.To, cls, m.Change, cls, m.ChangePct))
		}
		sb.WriteString(`</tbody></table></div>`)
	} else {
		sb.WriteString(`<p>No significant metric changes.</p>`)
	}
	sb.WriteString(`</div>`)

	// Alert changes section.
	sb.WriteString(`<div class="section"><h2>Alert Changes</h2>`)
	if len(diff.AlertsAdded) > 0 || len(diff.AlertsResolved) > 0 {
		sb.WriteString(`<div class="card"><table><thead><tr><th>Action</th><th>Severity</th><th>Name</th><th>Description</th></tr></thead><tbody>`)
		for _, a := range diff.AlertsAdded {
			sb.WriteString(fmt.Sprintf(`<tr><td><span class="badge badge-add">NEW</span></td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				string(a.Severity), a.Name, a.Description))
		}
		for _, a := range diff.AlertsResolved {
			sb.WriteString(fmt.Sprintf(`<tr><td><span class="badge badge-info">RESOLVED</span></td><td>%s</td><td>%s</td><td></td></tr>`,
				string(a.Severity), a.Name))
		}
		sb.WriteString(`</tbody></table></div>`)
	} else {
		sb.WriteString(`<p>No alert changes.</p>`)
	}
	sb.WriteString(`</div>`)

	// Raw diff summary.
	sb.WriteString(`<div class="section"><h2>Summary</h2><div class="summary">`)
	sb.WriteString(report.Summary)
	sb.WriteString(`</div></div>`)

	sb.WriteString(`</body></html>`)

	return sb.String()
}
