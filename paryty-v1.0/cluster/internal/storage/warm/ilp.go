// Package warm — ILP (InfluxDB Line Protocol) sender for QuestDB high-throughput ingestion.
//
// ILP is QuestDB's native high-throughput ingestion protocol, significantly faster
// than PG INSERT for bulk metric writes. The sender buffers lines and flushes
// over a persistent TCP connection with automatic reconnect on failure.
package warm

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// defaultBufLimit is the buffer threshold that triggers an automatic flush.
	// Kept small to avoid overwhelming QuestDB's ILP handler with large TCP writes.
	defaultBufLimit = 8 * 1024 // 8 KB

	// defaultDialTimeout is the TCP connection timeout for the ILP socket.
	defaultDialTimeout = 5 * time.Second

	// defaultWriteTimeout is the write deadline for ILP TCP writes.
	defaultWriteTimeout = 10 * time.Second

	// defaultILPPort is the standard QuestDB ILP port.
	defaultILPPort = 9009
)

// ILPSender sends metrics to QuestDB over the InfluxDB Line Protocol (ILP).
// It buffers lines internally and auto-flushes when the buffer exceeds bufLimit.
// Thread-safe: all public methods are safe for concurrent use.
type ILPSender struct {
	addr     string
	conn     net.Conn
	connMu   sync.Mutex // protects conn for writes and reconnects
	mu       sync.Mutex // protects buf
	buf      bytes.Buffer
	bufLimit int
}

// NewILPSender creates a new ILP sender connected to the given QuestDB ILP address.
// The addr should be in "host:port" format. If no port is present, 9009 is used.
func NewILPSender(addr string) (*ILPSender, error) {
	if addr == "" {
		return nil, fmt.Errorf("ILP address must not be empty")
	}

	// Append default port if not specified.
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, strconv.Itoa(defaultILPPort))
	}

	conn, err := net.DialTimeout("tcp", addr, defaultDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial ILP at %s: %w", addr, err)
	}

	slog.Info("ILP sender connected", "addr", addr)

	return &ILPSender{
		addr:     addr,
		conn:     conn,
		bufLimit: defaultBufLimit,
	}, nil
}

// SendMetric formats a single metric as an ILP line, appends it to the internal
// buffer, and auto-flushes when the buffer exceeds the configured limit.
func (s *ILPSender) SendMetric(table string, tags map[string]string, fields map[string]any, ts time.Time) error {
	line := formatILPLine(table, tags, fields, ts)

	s.mu.Lock()
	_, _ = s.buf.WriteString(line)
	needFlush := s.buf.Len() >= s.bufLimit
	var data []byte
	if needFlush {
		data = make([]byte, s.buf.Len())
		copy(data, s.buf.Bytes())
		s.buf.Reset()
	}
	s.mu.Unlock()

	if needFlush {
		return s.writeToConn(data)
	}
	return nil
}

// Flush writes all buffered data to the TCP connection and clears the buffer.
// On write failure, it attempts one reconnect and retries the write.
func (s *ILPSender) Flush() error {
	s.mu.Lock()
	if s.buf.Len() == 0 {
		s.mu.Unlock()
		return nil
	}
	data := make([]byte, s.buf.Len())
	copy(data, s.buf.Bytes())
	s.buf.Reset()
	s.mu.Unlock()

	return s.writeToConn(data)
}

// Close flushes remaining data and closes the TCP connection.
func (s *ILPSender) Close() error {
	if err := s.Flush(); err != nil {
		slog.Error("ILP flush on close failed", "err", err)
	}

	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			return fmt.Errorf("close ILP connection: %w", err)
		}
		s.conn = nil
	}
	return nil
}

// writeToConn writes data to the TCP connection. On failure, it reconnects
// once and retries. The connMu must NOT be held by the caller — this method
// acquires it internally.
func (s *ILPSender) writeToConn(data []byte) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.conn == nil {
		return fmt.Errorf("ILP connection is closed")
	}

	_ = s.conn.SetWriteDeadline(time.Now().Add(defaultWriteTimeout))

	if _, err := s.conn.Write(data); err != nil {
		slog.Warn("ILP write failed, attempting reconnect", "err", err)

		if rerr := s.reconnectLocked(); rerr != nil {
			return fmt.Errorf("write failed (%w), reconnect also failed: %w", err, rerr)
		}

		_ = s.conn.SetWriteDeadline(time.Now().Add(defaultWriteTimeout))
		if _, err := s.conn.Write(data); err != nil {
			return fmt.Errorf("retry ILP write after reconnect: %w", err)
		}
	}

	return nil
}

// reconnectLocked closes the current connection and dials a new one.
// Caller must hold connMu.
func (s *ILPSender) reconnectLocked() error {
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}

	conn, err := net.DialTimeout("tcp", s.addr, defaultDialTimeout)
	if err != nil {
		return fmt.Errorf("reconnect ILP at %s: %w", s.addr, err)
	}

	s.conn = conn
	slog.Info("ILP sender reconnected", "addr", s.addr)
	return nil
}

// ---- ILP Line Formatting ----

// formatILPLine builds a single ILP line:
//
//	table,tag1=val1,tag2=val2 field1=v1,field2=v2 timestamp_ns\n
func formatILPLine(table string, tags map[string]string, fields map[string]any, ts time.Time) string {
	var b strings.Builder
	b.Grow(256) // pre-allocate for typical metric lines

	// Measurement name.
	b.WriteString(escapeILPToken(table))

	// Tags (sorted for deterministic output — idempotency).
	if len(tags) > 0 {
		writeSortedTags(&b, tags)
	}

	b.WriteByte(' ')

	// Fields (sorted for deterministic output — idempotency).
	writeSortedFields(&b, fields)

	// Timestamp in nanoseconds.
	b.WriteByte(' ')
	b.WriteString(strconv.FormatInt(ts.UnixNano(), 10))
	b.WriteByte('\n')

	return b.String()
}

// writeSortedTags writes tags in sorted key order for deterministic ILP output.
// BUGFIX: Empty tag values produce invalid ILP lines (e.g. "container_id=,").
// Tags with empty values are silently skipped to prevent QuestDB ILP rejection.
func writeSortedTags(b *strings.Builder, tags map[string]string) {
	keys := make([]string, 0, len(tags))
	for k := range tags {
		if tags[k] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, k := range keys {
		b.WriteByte(',')
		b.WriteString(escapeILPToken(k))
		b.WriteByte('=')
		b.WriteString(escapeILPToken(tags[k]))
	}
}

// writeSortedFields writes fields in sorted key order for deterministic ILP output.
func writeSortedFields(b *strings.Builder, fields map[string]any) {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(escapeILPToken(k))
		b.WriteByte('=')
		writeFieldValue(b, fields[k])
	}
}

// writeFieldValue serializes a field value with the correct ILP type suffix.
func writeFieldValue(b *strings.Builder, v any) {
	switch val := v.(type) {
	case float64:
		b.WriteString(strconv.FormatFloat(val, 'f', -1, 64))
	case float32:
		b.WriteString(strconv.FormatFloat(float64(val), 'f', -1, 32))
	case int64:
		b.WriteString(strconv.FormatInt(val, 10))
		b.WriteByte('i')
	case int32:
		b.WriteString(strconv.FormatInt(int64(val), 10))
		b.WriteByte('i')
	case int:
		b.WriteString(strconv.Itoa(val))
		b.WriteByte('i')
	case uint64:
		b.WriteString(strconv.FormatUint(val, 10))
		b.WriteByte('i')
	case uint32:
		b.WriteString(strconv.FormatUint(uint64(val), 10))
		b.WriteByte('i')
	case string:
		b.WriteByte('"')
		b.WriteString(escapeILPString(val))
		b.WriteByte('"')
	case bool:
		if val {
			b.WriteByte('t')
		} else {
			b.WriteByte('f')
		}
	case time.Time:
		if val.IsZero() {
			b.WriteString("null")
		} else {
			b.WriteByte('\'')
			b.WriteString(val.UTC().Format("2006-01-02T15:04:05.000000Z"))
			b.WriteByte('\'')
		}
	default:
		// Fallback: format as string.
		b.WriteByte('"')
		b.WriteString(escapeILPString(fmt.Sprintf("%v", val)))
		b.WriteByte('"')
	}
}

// escapeILPToken escapes special characters in an ILP tag key/value or field key.
// Characters to escape: space, comma, equals, backslash.
func escapeILPToken(s string) string {
	if !needsILPTokenEscape(s) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ', ',', '=', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// needsILPTokenEscape returns true if the string contains characters that
// require escaping in ILP tag keys/values or field keys.
func needsILPTokenEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == ',' || c == '=' || c == '\\' {
			return true
		}
	}
	return false
}

// escapeILPString escapes special characters inside a double-quoted ILP string
// field value. Only backslash and double-quote need escaping.
func escapeILPString(s string) string {
	if !needsILPStringEscape(s) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// needsILPStringEscape returns true if the string contains characters that
// require escaping inside an ILP double-quoted string value.
func needsILPStringEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '"' {
			return true
		}
	}
	return false
}
