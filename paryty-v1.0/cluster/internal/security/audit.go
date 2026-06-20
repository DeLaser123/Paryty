package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditEvent represents a single auditable action.
type AuditEvent struct {
	ID           string
	TenantID     string
	UserID       string
	Action       string
	ResourceType string
	ResourceID   string
	Details      map[string]interface{}
	IPAddress    string
	UserAgent    string
	CreatedAt    time.Time
	// Hash is a SHA-256 hash of the previous event's hash + this event's data.
	// This creates a tamper-evident chain: modifying any event breaks the chain.
	Hash string
}

// AuditLogger provides asynchronous, buffered audit logging to PostgreSQL.
// Events are written through a buffered channel to avoid blocking the
// request path during audit writes.
type AuditLogger struct {
	db       *pgxpool.Pool
	events   chan *AuditEvent
	done     chan struct{}
	shutdown sync.Once
	// lastHash tracks the hash of the last persisted event for chain integrity.
	lastHash string
	mu       sync.Mutex
}

// AuditLoggerConfig configures the audit logger.
type AuditLoggerConfig struct {
	// BufferSize is the capacity of the internal event channel.
	// Default: 4096.
	BufferSize int
}

// NewAuditLogger creates and starts an AuditLogger. The logger runs a
// background goroutine that drains the event channel and writes to PostgreSQL.
// Call Shutdown() to gracefully stop the logger.
func NewAuditLogger(db *pgxpool.Pool, cfg AuditLoggerConfig) *AuditLogger {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 4096
	}

	al := &AuditLogger{
		db:     db,
		events: make(chan *AuditEvent, cfg.BufferSize),
		done:   make(chan struct{}),
	}

	go al.worker()
	return al
}

// Log enqueues an audit event for asynchronous persistence.
// If the event channel is full, the event is dropped (audit is best-effort).
func (al *AuditLogger) Log(event *AuditEvent) {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	select {
	case al.events <- event:
	default:
		// Channel full — drop event rather than blocking the request path.
		// In production, this should be monitored and alerted on.
	}
}

// LogAction is a convenience method that constructs and enqueues an AuditEvent.
func (al *AuditLogger) LogAction(tenantID, userID, action, resourceType, resourceID string, details map[string]interface{}) {
	al.Log(&AuditEvent{
		TenantID:     tenantID,
		UserID:       userID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
	})
}

// Shutdown gracefully stops the audit logger, draining remaining events.
// Safe to call multiple times (idempotent).
func (al *AuditLogger) Shutdown(ctx context.Context) error {
	al.shutdown.Do(func() {
		close(al.events)
	})

	select {
	case <-al.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// worker drains the event channel and writes to PostgreSQL.
func (al *AuditLogger) worker() {
	defer close(al.done)

	for event := range al.events {
		al.persist(event)
	}
}

// persist writes a single audit event to PostgreSQL.
// If no database pool is configured, the event is silently dropped.
func (al *AuditLogger) persist(event *AuditEvent) {
	if al.db == nil {
		return // No database configured; drop event gracefully.
	}

	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		detailsJSON = []byte("{}")
	}

	// Compute hash chain: SHA-256(previous_hash + this_event_data)
	// Single critical section prevents TOCTOU race on lastHash.
	al.mu.Lock()
	previousHash := al.lastHash
	hashInput := previousHash + event.ID + event.TenantID + event.UserID + event.Action +
		event.ResourceType + event.ResourceID + string(detailsJSON) + event.IPAddress + event.UserAgent
	hash := sha256.Sum256([]byte(hashInput))
	event.Hash = hex.EncodeToString(hash[:])
	al.lastHash = event.Hash
	al.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = al.db.Exec(ctx, `
		INSERT INTO audit_log (id, tenant_id, user_id, action, resource_type, resource_id, details, ip_address, user_agent, hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, event.ID, event.TenantID, event.UserID, event.Action, event.ResourceType,
		event.ResourceID, detailsJSON, event.IPAddress, event.UserAgent, event.Hash)
	if err != nil {
		// In production, this should log to stderr or a monitoring system.
		// We intentionally swallow the error here to avoid cascading failures.
		_ = err
	}
}

// QueryAuditLog retrieves audit events for a tenant, ordered by creation time
// descending (most recent first).
func (al *AuditLogger) QueryAuditLog(ctx context.Context, tenantID string, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	rows, err := al.db.Query(ctx, `
		SELECT id, tenant_id, user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at, hash
		FROM audit_log
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("query audit log: %w", err)
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var detailsJSON []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.Action, &e.ResourceType,
			&e.ResourceID, &detailsJSON, &e.IPAddress, &e.UserAgent, &e.CreatedAt, &e.Hash); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		_ = json.Unmarshal(detailsJSON, &e.Details)
		events = append(events, e)
	}

	return events, nil
}

// PurgeOldEvents deletes audit events older than the specified duration.
// This implements the audit log retention policy.
// Returns the number of deleted rows.
func (al *AuditLogger) PurgeOldEvents(ctx context.Context, olderThan time.Duration) (int64, error) {
	if al.db == nil {
		return 0, nil
	}

	cutoff := time.Now().UTC().Add(-olderThan)
	tag, err := al.db.Exec(ctx, `
		DELETE FROM audit_log WHERE created_at < $1
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge old audit events: %w", err)
	}

	return tag.RowsAffected(), nil
}

// PurgeOldEventsForTenant deletes audit events for a specific tenant older than the specified duration.
// Returns the number of deleted rows.
func (al *AuditLogger) PurgeOldEventsForTenant(ctx context.Context, tenantID string, olderThan time.Duration) (int64, error) {
	if al.db == nil {
		return 0, nil
	}

	cutoff := time.Now().UTC().Add(-olderThan)
	tag, err := al.db.Exec(ctx, `
		DELETE FROM audit_log WHERE tenant_id = $1 AND created_at < $2
	`, tenantID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge old audit events for tenant: %w", err)
	}

	return tag.RowsAffected(), nil
}
