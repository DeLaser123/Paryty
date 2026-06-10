package security

import (
	"context"
	"testing"
	"time"
)

func TestNewAuditLogger_NoDB(t *testing.T) {
	logger := NewAuditLogger(nil, AuditLoggerConfig{BufferSize: 16})
	if logger == nil {
		t.Fatal("expected non-nil logger even without DB")
	}
	// Should shut down cleanly
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := logger.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestNewAuditLogger_ZeroBufferSize(t *testing.T) {
	logger := NewAuditLogger(nil, AuditLoggerConfig{BufferSize: 0})
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	logger.Shutdown(ctx)
}

func TestAuditLogger_LogWithoutDB(t *testing.T) {
	logger := NewAuditLogger(nil, AuditLoggerConfig{BufferSize: 64})

	// Logging without DB should not block or panic
	logger.Log(&AuditEvent{
		Action:       "user.login",
		TenantID:     "tenant-1",
		UserID:       "user-1",
		ResourceType: "session",
		ResourceID:   "sess-123",
		Details:      map[string]interface{}{"ip": "127.0.0.1"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := logger.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestAuditLogger_MultipleEvents(t *testing.T) {
	logger := NewAuditLogger(nil, AuditLoggerConfig{BufferSize: 64})

	for i := 0; i < 100; i++ {
		logger.Log(&AuditEvent{
			Action:       "test.action",
			TenantID:     "tenant-1",
			UserID:       "user-1",
			ResourceType: "test",
			ResourceID:   "res-1",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := logger.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestAuditLogger_ChannelFullDrop(t *testing.T) {
	// Use a very small buffer to force drops
	logger := NewAuditLogger(nil, AuditLoggerConfig{BufferSize: 2})

	// Fill and overflow the channel — should not block
	for i := 0; i < 10; i++ {
		logger.Log(&AuditEvent{
			Action:       "test.drop",
			TenantID:     "t",
			UserID:       "u",
			ResourceType: "r",
			ResourceID:   "r",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	logger.Shutdown(ctx)
}

func TestAuditEvent_ZeroValues(t *testing.T) {
	logger := NewAuditLogger(nil, AuditLoggerConfig{BufferSize: 8})

	// Log an event with all zero values
	logger.Log(&AuditEvent{})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	logger.Shutdown(ctx)
}

func TestAuditLogger_DoubleShutdown(t *testing.T) {
	logger := NewAuditLogger(nil, AuditLoggerConfig{BufferSize: 8})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// First shutdown
	if err := logger.Shutdown(ctx); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}

	// Second shutdown should not panic
	if err := logger.Shutdown(ctx); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
}
