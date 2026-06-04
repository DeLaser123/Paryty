package warm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
)

// ---- Test Helpers ----

// jsonResponse writes a JSON response to the httptest response writer.
func jsonResponse(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ---- TestRESTWriter_Ping ----

func TestRESTWriter_Ping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query().Get("query")
		if query != "SELECT 1" {
			t.Errorf("unexpected query: %s", query)
		}
		jsonResponse(w, http.StatusOK, QuestDBResponse{
			Query:   "SELECT 1",
			Columns: []ColumnDesc{{Name: "1", Type: "INT"}},
			Dataset: [][]any{{1}},
			Count:   1,
		})
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)
	err := writer.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping() returned error: %v", err)
	}
}

func TestRESTWriter_Ping_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, QuestDBResponse{})
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := writer.Ping(ctx)
	if err == nil {
		t.Fatal("Ping() with canceled context should return error")
	}
}

// ---- TestRESTWriter_ExecuteQuery ----

func TestRESTWriter_ExecuteQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query().Get("query")
		if query == "" {
			jsonResponse(w, http.StatusBadRequest, QuestDBResponse{Error: "missing query"})
			return
		}

		jsonResponse(w, http.StatusOK, QuestDBResponse{
			Query: query,
			Columns: []ColumnDesc{
				{Name: "agent_id", Type: "SYMBOL"},
				{Name: "value", Type: "DOUBLE"},
			},
			Dataset: [][]any{
				{"agent-1", 42.5},
				{"agent-2", 99.1},
			},
			Count:  2,
			Timing: 1234,
		})
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)
	raw, err := writer.ExecuteQuery(context.Background(), "SELECT agent_id, value FROM cpu_metrics LIMIT 2")
	if err != nil {
		t.Fatalf("ExecuteQuery() returned error: %v", err)
	}

	var result QuestDBResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if result.Count != 2 {
		t.Errorf("expected count 2, got %d", result.Count)
	}
	if len(result.Dataset) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Dataset))
	}
	if result.Dataset[0][0] != "agent-1" {
		t.Errorf("expected first agent_id 'agent-1', got %v", result.Dataset[0][0])
	}
}

func TestRESTWriter_ExecuteQuery_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, QuestDBResponse{
			Error: "table does not exist: bogus_table",
		})
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)
	_, err := writer.ExecuteQuery(context.Background(), "SELECT * FROM bogus_table")
	if err == nil {
		t.Fatal("ExecuteQuery() should return error for QuestDB error response")
	}
	if !strings.Contains(err.Error(), "questdb error") {
		t.Errorf("expected 'questdb error' in message, got: %v", err)
	}
}

func TestRESTWriter_ExecuteQuery_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)
	_, err := writer.ExecuteQuery(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("ExecuteQuery() should return error for 500 status")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected 'status 500' in message, got: %v", err)
	}
}

// ---- TestRESTWriter_BulkInsertCSV ----

func TestRESTWriter_BulkInsertCSV(t *testing.T) {
	var receivedBody []byte
	var receivedContentType string
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedContentType = r.Header.Get("Content-Type")

		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, QuestDBResponse{})
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)

	columns := []string{"timestamp", "agent_id", "value"}
	data := [][]any{
		{time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC), "agent-1", 42.5},
		{time.Date(2025, 1, 15, 10, 1, 0, 0, time.UTC), "agent-2", 99.1},
	}

	err := writer.BulkInsertCSV(context.Background(), "cpu_metrics", columns, data)
	if err != nil {
		t.Fatalf("BulkInsertCSV() returned error: %v", err)
	}

	if receivedPath != "/imp" {
		t.Errorf("expected path /imp, got %s", receivedPath)
	}
	if !strings.HasPrefix(receivedContentType, "text/csv") {
		t.Errorf("expected Content-Type text/csv, got %s", receivedContentType)
	}

	csvStr := string(receivedBody)
	lines := strings.Split(strings.TrimSpace(csvStr), "\n")

	// Verify header.
	if lines[0] != "timestamp,agent_id,value" {
		t.Errorf("unexpected header: %q", lines[0])
	}

	// Verify row count: header + 2 data rows.
	if len(lines) != 3 {
		t.Errorf("expected 3 CSV lines, got %d", len(lines))
	}

	// Verify first row contains agent-1.
	if !strings.Contains(lines[1], "agent-1") {
		t.Errorf("expected 'agent-1' in first row: %q", lines[1])
	}
}

func TestRESTWriter_BulkInsertCSV_EmptyData(t *testing.T) {
	writer := NewRESTWriter("http://localhost:19000")
	err := writer.BulkInsertCSV(context.Background(), "test", []string{"col1"}, [][]any{})
	if err != nil {
		t.Fatalf("BulkInsertCSV with empty data should return nil, got: %v", err)
	}
}

func TestRESTWriter_BulkInsertCSV_EmptyTable(t *testing.T) {
	writer := NewRESTWriter("http://localhost:19000")
	err := writer.BulkInsertCSV(context.Background(), "", []string{"col1"}, [][]any{{"val"}})
	if err == nil {
		t.Fatal("BulkInsertCSV with empty table should return error")
	}
}

func TestRESTWriter_BulkInsertCSV_EmptyColumns(t *testing.T) {
	writer := NewRESTWriter("http://localhost:19000")
	err := writer.BulkInsertCSV(context.Background(), "test", nil, [][]any{{"val"}})
	if err == nil {
		t.Fatal("BulkInsertCSV with empty columns should return error")
	}
}

// ---- TestRESTWriter_ErrorHandling ----

func TestRESTWriter_ErrorHandling_ConnectionRefused(t *testing.T) {
	writer := NewRESTWriter("http://127.0.0.1:1")

	err := writer.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping to closed port should return error")
	}
	if !strings.Contains(err.Error(), "questdb unreachable") {
		t.Errorf("expected 'questdb unreachable' in error, got: %v", err)
	}
}

func TestRESTWriter_ErrorHandling_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("this is not json"))
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)
	_, err := writer.ExecuteQuery(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("ExecuteQuery with invalid JSON should return error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' in error, got: %v", err)
	}
}

func TestRESTWriter_ErrorHandling_BulkInsertServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid CSV format"))
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)
	err := writer.BulkInsertCSV(context.Background(), "test_table", []string{"col1"}, [][]any{{"val"}})
	if err == nil {
		t.Fatal("BulkInsertCSV with 400 response should return error")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("expected 'status 400' in error, got: %v", err)
	}
}

// ---- TestRESTWriter_Timeout ----

func TestRESTWriter_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		jsonResponse(w, http.StatusOK, QuestDBResponse{})
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL, WithRESTTimeout(50*time.Millisecond))

	err := writer.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping should return error on timeout")
	}
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("expected timeout/deadline error, got: %v", err)
	}
}

func TestRESTWriter_Timeout_ContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		jsonResponse(w, http.StatusOK, QuestDBResponse{})
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := writer.Ping(ctx)
	if err == nil {
		t.Fatal("Ping should return error on context deadline")
	}
}

// ---- TestRESTWriter_BulkInsertAggregated ----

func TestRESTWriter_BulkInsertAggregated(t *testing.T) {
	var receivedBody []byte
	var receivedTableName string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/imp" {
			receivedTableName = r.URL.Query().Get("name")
			var err error
			receivedBody, err = io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		jsonResponse(w, http.StatusOK, QuestDBResponse{})
	}))
	defer server.Close()

	writer := NewRESTWriter(server.URL)
	metrics := []models.AggregatedMetric{
		{
			AgentID:   "agent-1",
			Name:      "cpu.usage",
			Labels:    map[string]string{"host": "web-01"},
			Window:    time.Minute,
			AggType:   models.AggregationAvg,
			Value:     42.5,
			Timestamp: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			AgentID:   "agent-2",
			Name:      "memory.used",
			Labels:    map[string]string{},
			Window:    5 * time.Minute,
			AggType:   models.AggregationP99,
			Value:     8589934592,
			Timestamp: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			AgentID:   "", // Invalid: empty agent_id.
			Name:      "disk.read",
			Window:    time.Minute,
			AggType:   models.AggregationSum,
			Value:     100,
			Timestamp: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		},
	}

	err := writer.BulkInsertAggregated(context.Background(), "tenant-a", metrics)
	if err != nil {
		t.Fatalf("BulkInsertAggregated() returned error: %v", err)
	}

	if receivedTableName != "aggregated_metrics" {
		t.Errorf("expected table name 'aggregated_metrics', got %q", receivedTableName)
	}

	csvStr := string(receivedBody)
	lines := strings.Split(strings.TrimSpace(csvStr), "\n")

	// Header + 2 valid metrics (third has empty agent_id → skipped).
	if len(lines) != 3 {
		t.Errorf("expected 3 CSV lines (header + 2 valid), got %d: %q", len(lines), csvStr)
	}

	// Verify header.
	expectedCols := "timestamp,agent_id,tenant_id,name,labels,window,agg_type,value"
	if lines[0] != expectedCols {
		t.Errorf("unexpected header:\n  got:  %q\n  want: %q", lines[0], expectedCols)
	}

	// Verify tenant_id appears in data rows.
	if !strings.Contains(lines[1], "tenant-a") {
		t.Errorf("expected 'tenant-a' in first data row: %q", lines[1])
	}
}

func TestRESTWriter_BulkInsertAggregated_EmptyMetrics(t *testing.T) {
	writer := NewRESTWriter("http://localhost:19000")
	err := writer.BulkInsertAggregated(context.Background(), "tenant-a", nil)
	if err != nil {
		t.Fatalf("BulkInsertAggregated with nil metrics should return nil, got: %v", err)
	}
}

func TestRESTWriter_BulkInsertAggregated_AllInvalid(t *testing.T) {
	writer := NewRESTWriter("http://localhost:19000")
	metrics := []models.AggregatedMetric{
		{AgentID: "", Name: "cpu", Window: time.Minute, Timestamp: time.Now()},
		{AgentID: "", Name: "mem", Window: time.Minute, Timestamp: time.Now()},
	}
	err := writer.BulkInsertAggregated(context.Background(), "tenant-a", metrics)
	if err == nil {
		t.Fatal("BulkInsertAggregated with all invalid metrics should return error")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("expected 'validation' in error, got: %v", err)
	}
}

// ---- Test ValueToCSVString ----

func TestValueToCSVString(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"float64", 3.14, "3.14"},
		{"int64", int64(42), "42"},
		{"uint64", uint64(100), "100"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"zero time", time.Time{}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := valueToCSVString(tc.val)
			if got != tc.want {
				t.Errorf("valueToCSVString(%v) = %q, want %q", tc.val, got, tc.want)
			}
		})
	}
}

func TestValueToCSVString_NonZeroTime(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	got := valueToCSVString(ts)
	if !strings.HasPrefix(got, "2025-06-01T12:00:00") {
		t.Errorf("valueToCSVString(%v) = %q, want prefix '2025-06-01T12:00:00'", ts, got)
	}
}

// ---- Test FormatTimestamp ----

func TestFormatTimestamp(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 30, 45, 0, time.UTC)
	got := formatTimestamp(ts)
	want := "2025-06-01T12:30:45.000000Z"
	if got != want {
		t.Errorf("formatTimestamp() = %q, want %q", got, want)
	}
}

// ---- Test ValidateAggregatedMetric ----

func TestValidateAggregatedMetric(t *testing.T) {
	validTime := time.Now()

	tests := []struct {
		name    string
		metric  models.AggregatedMetric
		wantErr bool
	}{
		{
			name:    "valid",
			metric:  models.AggregatedMetric{AgentID: "a1", Name: "cpu", Timestamp: validTime, Window: time.Minute},
			wantErr: false,
		},
		{
			name:    "empty agent_id",
			metric:  models.AggregatedMetric{AgentID: "", Name: "cpu", Timestamp: validTime, Window: time.Minute},
			wantErr: true,
		},
		{
			name:    "empty name",
			metric:  models.AggregatedMetric{AgentID: "a1", Name: "", Timestamp: validTime, Window: time.Minute},
			wantErr: true,
		},
		{
			name:    "zero timestamp",
			metric:  models.AggregatedMetric{AgentID: "a1", Name: "cpu", Timestamp: time.Time{}, Window: time.Minute},
			wantErr: true,
		},
		{
			name:    "zero window",
			metric:  models.AggregatedMetric{AgentID: "a1", Name: "cpu", Timestamp: validTime, Window: 0},
			wantErr: true,
		},
		{
			name:    "negative window",
			metric:  models.AggregatedMetric{AgentID: "a1", Name: "cpu", Timestamp: validTime, Window: -time.Minute},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAggregatedMetric(&tc.metric)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateAggregatedMetric() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ---- Test NewRESTWriter Options ----

func TestNewRESTWriter_DefaultTimeout(t *testing.T) {
	writer := NewRESTWriter("http://localhost:9000")
	if writer.timeout != defaultRESTTimeout {
		t.Errorf("expected default timeout %v, got %v", defaultRESTTimeout, writer.timeout)
	}
}

func TestNewRESTWriter_WithTimeout(t *testing.T) {
	writer := NewRESTWriter("http://localhost:9000", WithRESTTimeout(10*time.Second))
	if writer.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", writer.timeout)
	}
	if writer.client.Timeout != 10*time.Second {
		t.Errorf("expected client timeout 10s, got %v", writer.client.Timeout)
	}
}

func TestNewRESTWriter_WithHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 5 * time.Second}
	writer := NewRESTWriter("http://localhost:9000", WithHTTPClient(customClient))
	if writer.client != customClient {
		t.Error("expected custom client to be set")
	}
}

func TestNewRESTWriter_TrailingSlash(t *testing.T) {
	writer := NewRESTWriter("http://localhost:9000/")
	if writer.endpoint != "http://localhost:9000" {
		t.Errorf("expected trailing slash stripped, got %q", writer.endpoint)
	}
}

// ---- Test BuildCSVBody ----

func TestBuildCSVBody(t *testing.T) {
	columns := []string{"name", "value"}
	data := [][]any{
		{"cpu_usage", 75.5},
		{"mem_usage", 50.0},
	}

	body, err := buildCSVBody(columns, data)
	if err != nil {
		t.Fatalf("buildCSVBody() returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "name,value" {
		t.Errorf("unexpected header: %q", lines[0])
	}
	if lines[1] != "cpu_usage,75.5" {
		t.Errorf("unexpected first row: %q", lines[1])
	}
	if lines[2] != "mem_usage,50" {
		t.Errorf("unexpected second row: %q", lines[2])
	}
}

// ---- Test Truncate ----

func TestTruncate(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is a ..."},
		{"", 5, ""},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := truncate(tc.s, tc.maxLen)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.s, tc.maxLen, got, tc.want)
			}
		})
	}
}

// ---- Test TranslateHTTPError ----

func TestTranslateHTTPError(t *testing.T) {
	if got := translateHTTPError(nil); got != nil {
		t.Errorf("translateHTTPError(nil) = %v, want nil", got)
	}
}
