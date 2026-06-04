// Package warm — REST bulk writer for QuestDB HTTP API.
//
// QuestDB exposes a REST API on port 9000 with two key endpoints:
//   - POST /exec?query=... — Execute SQL queries, returns JSON results
//   - POST /imp             — Bulk CSV import for high-throughput ingestion
//
// This writer complements the existing pgxpool-based Client for batch operations
// where HTTP bulk import is preferred over individual PG INSERT statements.
// All operations are idempotent and context-aware.
package warm

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
)

// Default REST writer configuration.
const (
	defaultRESTTimeout         = 30 * time.Second
	defaultIdleConnTimeout     = 90 * time.Second
	defaultMaxIdleConns        = 10
	defaultMaxIdleConnsPerHost = 5
)

// RESTWriter sends queries and bulk data to QuestDB's HTTP REST API.
// Thread-safe: all public methods are safe for concurrent use.
type RESTWriter struct {
	endpoint string       // QuestDB HTTP endpoint, e.g. "http://questdb:9000"
	client   *http.Client
	logger   *slog.Logger
	timeout  time.Duration
}

// RESTOption configures the RESTWriter.
type RESTOption func(*RESTWriter)

// WithRESTTimeout sets the HTTP request timeout.
func WithRESTTimeout(d time.Duration) RESTOption {
	return func(w *RESTWriter) {
		w.timeout = d
		w.client.Timeout = d
	}
}

// WithHTTPClient sets a custom HTTP client. This overrides the default
// connection-pooled client. The caller is responsible for configuring
// timeouts on the provided client.
func WithHTTPClient(c *http.Client) RESTOption {
	return func(w *RESTWriter) {
		w.client = c
	}
}

// WithRESTLogger sets a custom slog logger for the REST writer.
func WithRESTLogger(logger *slog.Logger) RESTOption {
	return func(w *RESTWriter) {
		w.logger = logger
	}
}

// NewRESTWriter creates a new REST writer for QuestDB's HTTP API.
// The endpoint should be a base URL like "http://questdb:9000".
// A default connection-pooled HTTP client with timeout is created if none is
// provided via WithHTTPClient.
func NewRESTWriter(endpoint string, opts ...RESTOption) *RESTWriter {
	endpoint = strings.TrimRight(endpoint, "/")

	w := &RESTWriter{
		endpoint: endpoint,
		timeout:  defaultRESTTimeout,
		client: &http.Client{
			Timeout: defaultRESTTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        defaultMaxIdleConns,
				MaxIdleConnsPerHost: defaultMaxIdleConnsPerHost,
				IdleConnTimeout:     defaultIdleConnTimeout,
			},
		},
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(w)
	}

	w.logger.Info("REST writer created",
		"endpoint", w.endpoint,
		"timeout", w.timeout,
	)

	return w
}

// ---- QuestDB Response Types ----

// QuestDBResponse represents the JSON response from QuestDB's /exec endpoint.
type QuestDBResponse struct {
	Query   string       `json:"query"`
	Columns []ColumnDesc `json:"columns"`
	Dataset [][]any      `json:"dataset"`
	Count   int          `json:"count"`
	Timing  int64        `json:"timing"`
	Error   string       `json:"error,omitempty"`
}

// ColumnDesc describes a column in a QuestDB query result.
type ColumnDesc struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ---- ExecuteQuery ----

// ExecuteQuery sends a SQL query to QuestDB's /exec endpoint and returns the
// raw JSON response. Use this for arbitrary SQL queries where you need to parse
// the result set yourself.
//
// Args are not used for parameter substitution — QuestDB's REST /exec endpoint
// does not support parameterized queries. Embed values directly in the query
// string. The variadic args parameter is accepted for interface compatibility
// but ignored.
func (w *RESTWriter) ExecuteQuery(ctx context.Context, query string, _ ...any) (json.RawMessage, error) {
	url := w.endpoint + "/exec"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	q := req.URL.Query()
	q.Set("query", query)
	req.URL.RawQuery = q.Encode()

	start := time.Now()
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", translateHTTPError(err))
	}
	defer closeAndLog(resp.Body, w.logger)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	w.logger.Debug("exec query completed",
		"query", truncate(query, 200),
		"status", resp.StatusCode,
		"duration", time.Since(start),
		"bytes", len(body),
	)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("questdb returned status %d: %s", resp.StatusCode, string(body))
	}

	// Validate that the response is valid JSON.
	var check QuestDBResponse
	if err := json.Unmarshal(body, &check); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}

	if check.Error != "" {
		return nil, fmt.Errorf("questdb error: %s", check.Error)
	}

	return json.RawMessage(body), nil
}

// ---- BulkInsertCSV ----

// BulkInsertCSV performs a bulk CSV import to QuestDB via the /imp endpoint.
// The table parameter specifies the target table. Columns define the CSV header
// order. Data is a slice of rows where each row is a slice of values.
//
// QuestDB's /imp endpoint expects:
//   - Method: POST
//   - Content-Type: text/csv
//   - Body: CSV data with header row as the first line
//   - Query params: name (table name), overwrite (optional), atomicity (optional)
//
// Idempotent: same CSV data produces the same table state.
func (w *RESTWriter) BulkInsertCSV(ctx context.Context, table string, columns []string, data [][]any) error {
	if table == "" {
		return fmt.Errorf("table name must not be empty")
	}
	if len(columns) == 0 {
		return fmt.Errorf("columns must not be empty")
	}
	if len(data) == 0 {
		return nil // nothing to insert
	}

	csvBody, err := buildCSVBody(columns, data)
	if err != nil {
		return fmt.Errorf("build CSV body: %w", err)
	}

	url := fmt.Sprintf("%s/imp?name=%s", w.endpoint, table)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(csvBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "text/csv")

	start := time.Now()
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("bulk insert CSV: %w", translateHTTPError(err))
	}
	defer closeAndLog(resp.Body, w.logger)

	// Read response to drain the connection for reuse.
	body, _ := io.ReadAll(resp.Body)

	w.logger.Info("bulk insert CSV completed",
		"table", table,
		"rows", len(data),
		"columns", len(columns),
		"status", resp.StatusCode,
		"duration", time.Since(start),
		"bytes_sent", len(csvBody),
	)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("questdb /imp returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// buildCSVBody constructs a CSV byte slice from columns and data rows.
// The first line is the header (column names). Subsequent lines are data rows.
func buildCSVBody(columns []string, data [][]any) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(estimateCSVSize(columns, data))

	writer := csv.NewWriter(&buf)

	// Write header row.
	if err := writer.Write(columns); err != nil {
		return nil, fmt.Errorf("write CSV header: %w", err)
	}

	// Write data rows.
	for i, row := range data {
		record := make([]string, len(row))
		for j, val := range row {
			record[j] = valueToCSVString(val)
		}
		if err := writer.Write(record); err != nil {
			return nil, fmt.Errorf("write CSV row %d: %w", i, err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("flush CSV writer: %w", err)
	}

	return buf.Bytes(), nil
}

// estimateCSVSize returns a rough byte estimate for the CSV buffer.
func estimateCSVSize(columns []string, data [][]any) int {
	avgCellLen := 20 // rough average
	headerSize := len(columns) * (avgCellLen + 1)
	dataSize := len(data) * len(columns) * (avgCellLen + 1)
	return headerSize + dataSize + 256 // extra for newlines and safety
}

// valueToCSVString converts a Go value to its CSV string representation.
func valueToCSVString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case int64:
		return strconv.FormatInt(val, 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int:
		return strconv.Itoa(val)
	case uint64:
		return strconv.FormatUint(val, 10)
	case uint32:
		return strconv.FormatUint(uint64(val), 10)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case time.Time:
		if val.IsZero() {
			return ""
		}
		return val.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// ---- BulkInsertAggregated ----

// BulkInsertAggregated performs a bulk insert of aggregated metrics into
// QuestDB's aggregated_metrics table via CSV import.
//
// Each metric is converted to a CSV row matching the table schema:
//
//	timestamp, agent_id, tenant_id, name, labels, window, agg_type, value
//
// Idempotent: same metrics produce the same table state. QuestDB's WAL mode
// with deduplication handles repeated inserts gracefully.
func (w *RESTWriter) BulkInsertAggregated(ctx context.Context, tenant string, metrics []models.AggregatedMetric) error {
	if len(metrics) == 0 {
		return nil
	}

	columns := []string{
		"timestamp", "agent_id", "tenant_id", "name", "labels",
		"window", "agg_type", "value",
	}

	data := make([][]any, 0, len(metrics))
	for i := range metrics {
		m := &metrics[i]
		if err := validateAggregatedMetric(m); err != nil {
			w.logger.Warn("skipping invalid aggregated metric",
				"index", i,
				"error", err,
			)
			continue
		}

		labelsJSON := "{}"
		if len(m.Labels) > 0 {
			b, err := json.Marshal(m.Labels)
			if err == nil {
				labelsJSON = string(b)
			}
		}

		row := []any{
			m.Timestamp.UTC(),
			m.AgentID,
			tenant,
			m.Name,
			labelsJSON,
			int64(m.Window.Seconds()),
			string(m.AggType),
			m.Value,
		}
		data = append(data, row)
	}

	if len(data) == 0 {
		return fmt.Errorf("all %d metrics failed validation", len(metrics))
	}

	w.logger.Info("bulk inserting aggregated metrics",
		"tenant", tenant,
		"total", len(metrics),
		"valid", len(data),
	)

	return w.BulkInsertCSV(ctx, "aggregated_metrics", columns, data)
}

// validateAggregatedMetric checks that required fields are populated.
func validateAggregatedMetric(m *models.AggregatedMetric) error {
	if m.AgentID == "" {
		return fmt.Errorf("agent_id is empty")
	}
	if m.Name == "" {
		return fmt.Errorf("name is empty")
	}
	if m.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is zero")
	}
	if m.Window <= 0 {
		return fmt.Errorf("window must be positive, got %v", m.Window)
	}
	return nil
}

// ---- Ping ----

// Ping checks QuestDB health by executing "SELECT 1" via the /exec endpoint.
// Returns nil if QuestDB responds with HTTP 200 and valid JSON.
func (w *RESTWriter) Ping(ctx context.Context) error {
	url := w.endpoint + "/exec"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create ping request: %w", err)
	}

	q := req.URL.Query()
	q.Set("query", "SELECT 1")
	req.URL.RawQuery = q.Encode()

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("ping questdb: %w", translateHTTPError(err))
	}
	defer closeAndLog(resp.Body, w.logger)

	// Drain body for connection reuse.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read ping response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping returned status %d: %s", resp.StatusCode, string(body))
	}

	// Validate JSON structure.
	var result QuestDBResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("invalid ping response: %w", err)
	}

	return nil
}

// ---- Helper Functions ----

// translateHTTPError converts common HTTP client errors into clearer messages.
func translateHTTPError(err error) error {
	if err == nil {
		return nil
	}
	errStr := err.Error()
	if strings.Contains(errStr, "context deadline exceeded") || strings.Contains(errStr, "context canceled") {
		return fmt.Errorf("request timeout or canceled: %w", err)
	}
	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "No connection could be made") {
		return fmt.Errorf("questdb unreachable: %w", err)
	}
	return err
}

// closeAndLog closes an io.Closer and logs any error. Used for response body cleanup.
func closeAndLog(c io.Closer, logger *slog.Logger) {
	if err := c.Close(); err != nil {
		logger.Warn("failed to close response body", "error", err)
	}
}

// truncate shortens a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// formatTimestamp formats a time.Time for QuestDB CSV import.
// QuestDB accepts ISO 8601 format with microsecond precision.
func formatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000Z")
}
