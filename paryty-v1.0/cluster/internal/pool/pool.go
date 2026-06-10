// Package pool provides shared object pools for Zstd compression, JSON
// serialization, and bytes buffers. These pools eliminate per-call allocation
// of expensive objects, reducing GC pressure in high-throughput paths.
//
// # Design Principles
//
//   - Every Get must be paired with a Put (typically via defer).
//   - Callers MUST Reset pooled objects before use (enc.Reset, dec.Reset, buf.Reset).
//   - PooledCompress and PooledDecompress handle Reset internally and return
//     fresh byte slices safe for long-term retention.
//   - PooledJSONMarshal and PooledJSONUnmarshal handle Reset internally.
//
// # Memory Impact
//
// Zstd encoder: ~1.2 MB internal state each. Pool holds 2-4 encoders = 2.4-4.8 MB fixed.
// Zstd decoder: ~500 KB internal state each. Pool holds 2-4 decoders = 1-2 MB fixed.
// Bytes buffers: grow to peak size, then shrink naturally via GC pressure.
// JSON encoder/decoder: pooling is limited to bytes.Buffer reuse since
// json.Encoder and json.Decoder do not expose a Reset method.
package pool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// ---- Zstd Encoder Pool ----

// zstdEncoderPool reuses Zstd encoders to avoid ~1.2 MB allocation per call.
// Each encoder is configured with SpeedDefault compression level.
// Callers MUST call enc.Reset(writer) before each use.
var zstdEncoderPool = sync.Pool{
	New: func() any {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if err != nil {
			// NewWriter with a valid compression level never fails.
			// Panic is appropriate here — a broken pool is a fatal startup defect.
			panic("pool: create zstd encoder: " + err.Error())
		}
		return enc
	},
}

// zstdDecoderPool reuses Zstd decoders to avoid per-call allocation.
// Callers MUST call dec.Reset(reader) before each use.
var zstdDecoderPool = sync.Pool{
	New: func() any {
		dec, err := zstd.NewReader(nil)
		if err != nil {
			// NewReader with nil reader never fails.
			panic("pool: create zstd decoder: " + err.Error())
		}
		return dec
	},
}

// bufferPool reuses bytes.Buffer instances. Internal memory grows to peak
// usage and is only freed when the GC collects pooled buffers that have
// shrunk. Callers MUST call buf.Reset() before each use.
var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// ---- Pooled Zstd Compression ----

// PooledCompress compresses data using a pooled Zstd encoder and buffer.
// The returned byte slice is a fresh copy safe for long-term retention.
//
// The encoder is Reset before use and returned to the pool after use.
// The buffer is Reset before use and returned to the pool after the
// compressed bytes are copied out.
func PooledCompress(data []byte) ([]byte, error) {
	enc := zstdEncoderPool.Get().(*zstd.Encoder)
	defer zstdEncoderPool.Put(enc)

	buf := bufferPool.Get().(*bytes.Buffer)
	defer bufferPool.Put(buf)
	buf.Reset()

	enc.Reset(buf)
	if _, err := enc.Write(data); err != nil {
		return nil, fmt.Errorf("pool: zstd write: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("pool: zstd close: %w", err)
	}

	// Copy bytes out of the buffer before returning it to the pool.
	// The buffer's internal memory may be overwritten by the next caller.
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// PooledDecompress decompresses Zstd-compressed data using a pooled decoder.
// The returned byte slice is a fresh allocation safe for long-term retention.
//
// The decoder is Reset before use and returned to the pool after use.
func PooledDecompress(data []byte) ([]byte, error) {
	dec := zstdDecoderPool.Get().(*zstd.Decoder)
	defer zstdDecoderPool.Put(dec)

	dec.Reset(bytes.NewReader(data))
	result, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("pool: zstd read: %w", err)
	}
	return result, nil
}

// ---- Pooled JSON Operations ----

// PooledJSONUnmarshal deserializes JSON data into v using json.Unmarshal.
// The json.Decoder type does not expose a Reset method, so we cannot pool
// decoder instances. This function provides a consistent API alongside
// PooledJSONMarshal and serves as the single call-site for future
// optimization (e.g., switching to a pooled third-party JSON library).
func PooledJSONUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// PooledJSONMarshal serializes v into JSON bytes using a pooled buffer.
// The returned byte slice is a fresh copy safe for long-term retention.
//
// Although json.Encoder does not expose a Reset method (preventing direct
// encoder pooling), we still pool the bytes.Buffer to avoid per-call buffer
// allocation. The encoder is created fresh for each call but writes to the
// reused buffer.
//
// The trailing newline added by json.Encoder.Encode is trimmed to match
// json.Marshal output for wire compatibility.
func PooledJSONMarshal(v any) ([]byte, error) {
	buf := bufferPool.Get().(*bytes.Buffer)
	defer bufferPool.Put(buf)
	buf.Reset()

	// json.Encoder cannot be pooled (no Reset method), but writing to a
	// pooled buffer still saves one heap allocation per call.
	enc := json.NewEncoder(buf)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("pool: json encode: %w", err)
	}

	// Trim the trailing newline added by json.Encoder.Encode to match
	// json.Marshal output. This ensures wire compatibility with existing
	// consumers that expect exact json.Marshal bytes.
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}

	// Copy bytes out of the buffer before returning it to the pool.
	result := make([]byte, len(b))
	copy(result, b)
	return result, nil
}
