package pool

import (
	"encoding/json"
	"sync"
	"testing"
)

// =============================================================================
// TestPooledCompressDecompress_RoundTrip
// =============================================================================

func TestPooledCompressDecompress_RoundTrip(t *testing.T) {
	t.Parallel()

	original := []byte(`[{"key":{"agent_id":"agent-1","metric_name":"cpu.usage","window_size":60000000000,"window_start":"2025-06-04T10:00:00Z"},"values":[10,20,30],"count":3,"sum":60,"min":10,"max":30}]`)

	compressed, err := PooledCompress(original)
	if err != nil {
		t.Fatalf("PooledCompress failed: %v", err)
	}

	if len(compressed) == 0 {
		t.Fatal("compressed data is empty")
	}

	decompressed, err := PooledDecompress(compressed)
	if err != nil {
		t.Fatalf("PooledDecompress failed: %v", err)
	}

	if string(decompressed) != string(original) {
		t.Errorf("decompressed doesn't match original.\noriginal:     %s\ndecompressed: %s", original, decompressed)
	}
}

// =============================================================================
// TestPooledCompressDecompress_EmptyData
// =============================================================================

func TestPooledCompressDecompress_EmptyData(t *testing.T) {
	t.Parallel()

	compressed, err := PooledCompress([]byte{})
	if err != nil {
		t.Fatalf("PooledCompress empty data failed: %v", err)
	}

	decompressed, err := PooledDecompress(compressed)
	if err != nil {
		t.Fatalf("PooledDecompress empty data failed: %v", err)
	}

	if len(decompressed) != 0 {
		t.Errorf("expected empty result, got %d bytes", len(decompressed))
	}
}

// =============================================================================
// TestPooledCompressDecompress_Concurrent
// Verify no data races when multiple goroutines use the pool simultaneously.
// =============================================================================

func TestPooledCompressDecompress_Concurrent(t *testing.T) {
	t.Parallel()

	original := []byte("test data for concurrent compression")

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			compressed, err := PooledCompress(original)
			if err != nil {
				t.Errorf("PooledCompress failed: %v", err)
				return
			}

			decompressed, err := PooledDecompress(compressed)
			if err != nil {
				t.Errorf("PooledDecompress failed: %v", err)
				return
			}

			if string(decompressed) != string(original) {
				t.Errorf("concurrent round-trip mismatch")
			}
		}()
	}

	wg.Wait()
}

// =============================================================================
// TestPooledJSONMarshalUnmarshal_RoundTrip
// =============================================================================

type testPayload struct {
	AgentID string  `json:"agent_id"`
	Value   float64 `json:"value"`
}

func TestPooledJSONMarshalUnmarshal_RoundTrip(t *testing.T) {
	t.Parallel()

	original := testPayload{AgentID: "agent-1", Value: 42.5}

	data, err := PooledJSONMarshal(original)
	if err != nil {
		t.Fatalf("PooledJSONMarshal failed: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("marshaled data is empty")
	}

	// Verify no trailing newline (must match json.Marshal output).
	if len(data) > 0 && data[len(data)-1] == '\n' {
		t.Error("PooledJSONMarshal produced trailing newline — must match json.Marshal")
	}

	var decoded testPayload
	if err := PooledJSONUnmarshal(data, &decoded); err != nil {
		t.Fatalf("PooledJSONUnmarshal failed: %v", err)
	}

	if decoded.AgentID != original.AgentID {
		t.Errorf("agent_id = %q, want %q", decoded.AgentID, original.AgentID)
	}
	if decoded.Value != original.Value {
		t.Errorf("value = %f, want %f", decoded.Value, original.Value)
	}
}

// =============================================================================
// TestPooledJSONMarshal_MatchesStdlib
// Verify PooledJSONMarshal output is identical to json.Marshal.
// =============================================================================

func TestPooledJSONMarshal_MatchesStdlib(t *testing.T) {
	t.Parallel()

	payloads := []any{
		map[string]string{"key": "value"},
		[]int{1, 2, 3},
		testPayload{AgentID: "agent-1", Value: 99.9},
		"simple string",
		42,
		nil,
	}

	for _, p := range payloads {
		stdlib, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		pooled, err := PooledJSONMarshal(p)
		if err != nil {
			t.Fatalf("PooledJSONMarshal failed: %v", err)
		}

		if string(stdlib) != string(pooled) {
			t.Errorf("PooledJSONMarshal(%v) = %q, want %q (json.Marshal)", p, pooled, stdlib)
		}
	}
}

// =============================================================================
// TestPooledJSONUnmarshal_Concurrent
// =============================================================================

func TestPooledJSONUnmarshal_Concurrent(t *testing.T) {
	t.Parallel()

	data := []byte(`{"agent_id":"agent-1","value":42.5}`)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			var p testPayload
			if err := PooledJSONUnmarshal(data, &p); err != nil {
				t.Errorf("PooledJSONUnmarshal failed: %v", err)
				return
			}

			if p.AgentID != "agent-1" || p.Value != 42.5 {
				t.Errorf("concurrent unmarshal mismatch: got %+v", p)
			}
		}()
	}

	wg.Wait()
}

// =============================================================================
// TestPooledJSONMarshal_Concurrent
// =============================================================================

func TestPooledJSONMarshal_Concurrent(t *testing.T) {
	t.Parallel()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			p := testPayload{AgentID: "agent-1", Value: float64(id)}
			data, err := PooledJSONMarshal(p)
			if err != nil {
				t.Errorf("PooledJSONMarshal failed: %v", err)
				return
			}

			// Verify we can unmarshal our own output.
			var decoded testPayload
			if err := PooledJSONUnmarshal(data, &decoded); err != nil {
				t.Errorf("PooledJSONUnmarshal failed: %v", err)
				return
			}

			if decoded.AgentID != p.AgentID || decoded.Value != p.Value {
				t.Errorf("concurrent round-trip mismatch: sent %+v, got %+v", p, decoded)
			}
		}(i)
	}

	wg.Wait()
}

// =============================================================================
// TestPooledCompressDecompress_TableDriven
// Table-driven round-trip test with various data sizes and patterns.
// =============================================================================

func TestPooledCompressDecompress_TableDriven(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"single_byte", []byte("x")},
		{"small", []byte("hello world")},
		{"medium", bytesRepeat([]byte("test"), 1000)},
		{"large", bytesRepeat([]byte("data"), 100000)},
		{"json_payload", []byte(`{"agent_id":"agent-1","cpu":{"usage":75.2,"cores":8},"memory":{"used":4294967296,"total":8589934592}}`)},
		{"high_entropy", func() []byte {
			b := make([]byte, 4096)
			for i := range b {
				b[i] = byte(i * 31 % 256)
			}
			return b
		}()},
		{"low_entropy", bytesRepeat([]byte("A"), 10000)},
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

			if len(decompressed) != len(tc.data) {
				t.Fatalf("length mismatch: got %d, want %d", len(decompressed), len(tc.data))
			}
			for i := range tc.data {
				if decompressed[i] != tc.data[i] {
					t.Fatalf("byte mismatch at index %d: got %d, want %d", i, decompressed[i], tc.data[i])
				}
			}
		})
	}
}

// =============================================================================
// TestPooledCompress_CompressionRatio
// Verify that repetitive data achieves meaningful compression.
// =============================================================================

func TestPooledCompress_CompressionRatio(t *testing.T) {
	t.Parallel()

	// Highly repetitive data should compress to <50% of original size.
	data := bytesRepeat([]byte("the quick brown fox jumps over the lazy dog. "), 1000)

	compressed, err := PooledCompress(data)
	if err != nil {
		t.Fatalf("PooledCompress failed: %v", err)
	}

	ratio := float64(len(compressed)) / float64(len(data))
	if ratio > 0.5 {
		t.Errorf("compression ratio %.2f (>0.50) — expected better compression for repetitive data", ratio)
	}

	// Verify round-trip.
	decompressed, err := PooledDecompress(compressed)
	if err != nil {
		t.Fatalf("PooledDecompress failed: %v", err)
	}
	if string(decompressed) != string(data) {
		t.Error("round-trip mismatch after compression")
	}
}

// =============================================================================
// TestPooledJSONMarshal_InvalidInput
// =============================================================================

func TestPooledJSONMarshal_InvalidInput(t *testing.T) {
	t.Parallel()

	// Channels cannot be marshaled to JSON.
	ch := make(chan int)
	_, err := PooledJSONMarshal(ch)
	if err == nil {
		t.Error("expected error marshaling channel type, got nil")
	}
}

// =============================================================================
// TestPooledJSONUnmarshal_InvalidInput
// =============================================================================

func TestPooledJSONUnmarshal_InvalidInput(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{"empty", []byte{}, true},
		{"invalid_json", []byte(`{bad json`), true},
		{"truncated", []byte(`{"key":`), true},
		{"null_literal", []byte(`null`), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var result map[string]interface{}
			err := PooledJSONUnmarshal(tc.data, &result)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkPooledCompress(b *testing.B) {
	data := bytesRepeat([]byte("benchmark payload with moderate repetition. "), 100)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := PooledCompress(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPooledDecompress(b *testing.B) {
	data := bytesRepeat([]byte("benchmark payload with moderate repetition. "), 100)
	compressed, err := PooledCompress(data)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := PooledDecompress(compressed)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPooledCompressDecompress_RoundTrip(b *testing.B) {
	data := bytesRepeat([]byte("benchmark payload with moderate repetition. "), 100)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		compressed, err := PooledCompress(data)
		if err != nil {
			b.Fatal(err)
		}
		_, err = PooledDecompress(compressed)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPooledJSONMarshal(b *testing.B) {
	p := testPayload{AgentID: "agent-bench", Value: 42.5}
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := PooledJSONMarshal(p)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPooledJSONUnmarshal(b *testing.B) {
	data := []byte(`{"agent_id":"agent-bench","value":42.5}`)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var p testPayload
		if err := PooledJSONUnmarshal(data, &p); err != nil {
			b.Fatal(err)
		}
	}
}

// bytesRepeat repeats the given byte slice n times and returns the result.
func bytesRepeat(src []byte, n int) []byte {
	result := make([]byte, len(src)*n)
	for i := 0; i < n; i++ {
		copy(result[i*len(src):], src)
	}
	return result
}
