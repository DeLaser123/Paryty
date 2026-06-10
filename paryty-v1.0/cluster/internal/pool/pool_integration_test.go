// Package pool provides shared object pools for Zstd compression, JSON
// serialization, and bytes buffers.
// This file contains integration tests that verify pool correctness under
// high concurrency, data integrity through compress/decompress cycles, and
// JSON marshal/unmarshal round-trips.
//
// Run with: go test -v -run Integration ./internal/pool/
// Skip in CI: go test -v -short ./internal/pool/
package pool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// =============================================================================
// TestPool_ZstdRoundTrip_Integration
// Verify compress → decompress round-trip preserves data integrity.
// =============================================================================

func TestPool_ZstdRoundTrip_Integration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello world")},
		{"medium", bytes.Repeat([]byte("paryty-metrics-payload-"), 100)},
		{"large", bytes.Repeat([]byte("x"), 100_000)},
		{"json", []byte(`{"agent_id":"agent-1","cpu":{"usage":75.3},"memory":{"used":8589934592}}`)},
		{"binary", func() []byte {
			b := make([]byte, 1000)
			for i := range b {
				b[i] = byte(i % 256)
			}
			return b
		}()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			compressed, err := PooledCompress(tc.data)
			if err != nil {
				t.Fatalf("PooledCompress failed: %v", err)
			}

			decompressed, err := PooledDecompress(compressed)
			if err != nil {
				t.Fatalf("PooledDecompress failed: %v", err)
			}

			if !bytes.Equal(tc.data, decompressed) {
				t.Errorf("data mismatch:\n  original:     %d bytes\n  decompressed: %d bytes",
					len(tc.data), len(decompressed))
			}
		})
	}
}

// =============================================================================
// TestPool_JSONRoundTrip_Integration
// Verify JSON marshal → unmarshal round-trip preserves data.
// =============================================================================

func TestPool_JSONRoundTrip_Integration(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		ID        int               `json:"id"`
		Name      string            `json:"name"`
		Tags      []string          `json:"tags"`
		Metadata  map[string]string `json:"metadata"`
		Score     float64           `json:"score"`
		Active    bool              `json:"active"`
	}

	testCases := []struct {
		name string
		obj  TestStruct
	}{
		{
			name: "full",
			obj: TestStruct{
				ID:       42,
				Name:     "test-agent",
				Tags:     []string{"prod", "us-east-1", "web"},
				Metadata: map[string]string{"env": "production", "team": "platform"},
				Score:    99.5,
				Active:   true,
			},
		},
		{
			name: "empty_fields",
			obj: TestStruct{
				ID:   0,
				Name: "",
			},
		},
		{
			name: "unicode",
			obj: TestStruct{
				ID:   1,
				Name: "测试代理-αβγ",
				Tags: []string{"日本語", "한국어"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := PooledJSONMarshal(tc.obj)
			if err != nil {
				t.Fatalf("PooledJSONMarshal failed: %v", err)
			}

			var decoded TestStruct
			if err := PooledJSONUnmarshal(data, &decoded); err != nil {
				t.Fatalf("PooledJSONUnmarshal failed: %v", err)
			}

			if tc.obj.ID != decoded.ID {
				t.Errorf("ID mismatch: expected %d, got %d", tc.obj.ID, decoded.ID)
			}
			if tc.obj.Name != decoded.Name {
				t.Errorf("Name mismatch: expected %q, got %q", tc.obj.Name, decoded.Name)
			}
			if tc.obj.Score != decoded.Score {
				t.Errorf("Score mismatch: expected %f, got %f", tc.obj.Score, decoded.Score)
			}
			if tc.obj.Active != decoded.Active {
				t.Errorf("Active mismatch: expected %v, got %v", tc.obj.Active, decoded.Active)
			}
		})
	}
}

// =============================================================================
// TestPool_ConcurrentCompression_Integration
// Verify the Zstd pool handles concurrent compress/decompress without races.
// =============================================================================

func TestPool_ConcurrentCompression_Integration(t *testing.T) {
	t.Parallel()

	const goroutines = 100
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*opsPerGoroutine)

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				data := []byte(fmt.Sprintf("goroutine-%d-operation-%d-payload-data", id, i))

				compressed, err := PooledCompress(data)
				if err != nil {
					errCh <- fmt.Errorf("compress g=%d op=%d: %w", id, i, err)
					return
				}

				decompressed, err := PooledDecompress(compressed)
				if err != nil {
					errCh <- fmt.Errorf("decompress g=%d op=%d: %w", id, i, err)
					return
				}

				if !bytes.Equal(data, decompressed) {
					errCh <- fmt.Errorf("data mismatch g=%d op=%d", id, i)
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent compression error: %v", err)
	}
}

// =============================================================================
// TestPool_ConcurrentJSON_Integration
// Verify the JSON pool handles concurrent marshal/unmarshal without races.
// =============================================================================

func TestPool_ConcurrentJSON_Integration(t *testing.T) {
	t.Parallel()

	type MetricData struct {
		AgentID string  `json:"agent_id"`
		Name    string  `json:"name"`
		Value   float64 `json:"value"`
		Labels  map[string]string `json:"labels"`
	}

	const goroutines = 100
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*opsPerGoroutine)

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				original := MetricData{
					AgentID: fmt.Sprintf("agent-%d", id),
					Name:    fmt.Sprintf("metric-%d", i),
					Value:   float64(id*1000 + i),
					Labels: map[string]string{
						"env":    "prod",
						"region": "us-east-1",
					},
				}

				data, err := PooledJSONMarshal(original)
				if err != nil {
					errCh <- fmt.Errorf("marshal g=%d op=%d: %w", id, i, err)
					return
				}

				var decoded MetricData
				if err := PooledJSONUnmarshal(data, &decoded); err != nil {
					errCh <- fmt.Errorf("unmarshal g=%d op=%d: %w", id, i, err)
					return
				}

				if original.AgentID != decoded.AgentID || original.Name != decoded.Name || original.Value != decoded.Value {
					errCh <- fmt.Errorf("mismatch g=%d op=%d: %+v != %+v", id, i, original, decoded)
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent JSON error: %v", err)
	}
}

// =============================================================================
// TestPool_ConcurrentMixed_Integration
// Verify interleaved compress/decompress and JSON marshal/unmarshal operations
// from many goroutines don't corrupt pool state.
// =============================================================================

func TestPool_ConcurrentMixed_Integration(t *testing.T) {
	t.Parallel()

	type Batch struct {
		AgentID   string    `json:"agent_id"`
		Timestamp string    `json:"timestamp"`
		Values    []float64 `json:"values"`
	}

	const goroutines = 50
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*opsPerGoroutine)

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				// Marshal JSON.
				batch := Batch{
					AgentID:   fmt.Sprintf("agent-%d", id),
					Timestamp: "2025-06-04T10:00:00Z",
					Values:    []float64{float64(i), float64(i * 2), float64(i * 3)},
				}
				jsonData, err := PooledJSONMarshal(batch)
				if err != nil {
					errCh <- fmt.Errorf("marshal g=%d op=%d: %w", id, i, err)
					return
				}

				// Compress the JSON.
				compressed, err := PooledCompress(jsonData)
				if err != nil {
					errCh <- fmt.Errorf("compress g=%d op=%d: %w", id, i, err)
					return
				}

				// Decompress.
				decompressed, err := PooledDecompress(compressed)
				if err != nil {
					errCh <- fmt.Errorf("decompress g=%d op=%d: %w", id, i, err)
					return
				}

				// Unmarshal.
				var decoded Batch
				if err := PooledJSONUnmarshal(decompressed, &decoded); err != nil {
					errCh <- fmt.Errorf("unmarshal g=%d op=%d: %w", id, i, err)
					return
				}

				if batch.AgentID != decoded.AgentID {
					errCh <- fmt.Errorf("agent_id mismatch g=%d op=%d", id, i)
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent mixed error: %v", err)
	}
}

// =============================================================================
// TestPool_JSONMarshal_NoTrailingNewline
// Verify PooledJSONMarshal trims the trailing newline added by json.Encoder.
// =============================================================================

func TestPool_JSONMarshal_NoTrailingNewline(t *testing.T) {
	t.Parallel()

	data, err := PooledJSONMarshal(map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("PooledJSONMarshal failed: %v", err)
	}

	if len(data) > 0 && data[len(data)-1] == '\n' {
		t.Error("PooledJSONMarshal should not produce trailing newline")
	}

	// Verify it matches json.Marshal output.
	stdData, _ := json.Marshal(map[string]string{"key": "value"})
	if !bytes.Equal(data, stdData) {
		t.Errorf("PooledJSONMarshal output doesn't match json.Marshal:\n  pooled: %s\n  std:    %s", data, stdData)
	}
}

// =============================================================================
// TestPool_CompressionRatio_Integration
// Verify compression achieves reasonable ratio on realistic payload.
// =============================================================================

func TestPool_CompressionRatio_Integration(t *testing.T) {
	t.Parallel()

	// Simulate a realistic metric batch payload.
	payload := bytes.Repeat([]byte(`{"agent_id":"agent-1","cpu":{"usage_percent":75.3,"cores":8},"memory":{"used_bytes":8589934592,"total_bytes":17179869184},"timestamp":"2025-06-04T10:00:00Z"}`), 50)

	compressed, err := PooledCompress(payload)
	if err != nil {
		t.Fatalf("PooledCompress failed: %v", err)
	}

	ratio := float64(len(payload)) / float64(len(compressed))
	t.Logf("Original: %d bytes, Compressed: %d bytes, Ratio: %.2fx", len(payload), len(compressed), ratio)

	// Zstd should achieve at least 3x compression on repetitive JSON.
	if ratio < 3.0 {
		t.Errorf("compression ratio %.2f is below expected minimum 3.0", ratio)
	}
}
