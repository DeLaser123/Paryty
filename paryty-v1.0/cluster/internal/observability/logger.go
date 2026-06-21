// Package observability provides unified logging configuration for the Paryty
// cluster. All Go components MUST use NewStandardLogger to produce JSON logs
// with consistent field names, ensuring cross-language log correlation.
//
// Log format (JSON):
//
//	{
//	  "level": "info",
//	  "timestamp": "2026-06-20T22:25:00.000Z",
//	  "message": "query processed",
//	  "component": "paryty-query",
//	  "version": "1.0.0",
//	  "trace_id": "abc-123"
//	}
//
// Field conventions (shared across Rust, Go, TypeScript, Python):
//   - "timestamp" — ISO 8601 with millisecond precision
//   - "level" — debug | info | warn | error
//   - "message" — human-readable event description
//   - "component" — service name (e.g. paryty-query, paryty-pipeline)
//   - "version" — semver string
//   - "trace_id" — OpenTelemetry trace ID when available
package observability

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ParytyVersion is the current cluster version injected at build time.
// Override via -ldflags "-X github.com/paryty/paryty-v1.0/cluster/internal/observability.ParytyVersion=1.2.3".
var ParytyVersion = "1.0.0"

// NewStandardLogger creates a JSON logger with Paryty standard fields.
// The service parameter identifies the component (e.g. "paryty-query",
// "paryty-pipeline", "paryty-ingestion").
//
// Output format matches the cross-language log contract documented in the
// observability-golden-signals rule. All Paryty Go services MUST use this
// constructor instead of zap.NewProduction() directly.
func NewStandardLogger(service string) (*zap.Logger, error) {
	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.MessageKey = "message"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	config.InitialFields = map[string]interface{}{
		"component": service,
		"version":   ParytyVersion,
	}
	return config.Build()
}
