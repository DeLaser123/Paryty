// Package hot implements the hot storage tier using Dragonfly (Redis-compatible).
// This file implements the Alert State Machine for alert lifecycle management
// including deduplication, state transitions, and active alert queries.
package hot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// AlertState represents the current state of an alert in the state machine.
type AlertState string

// Sentinel errors for alert lifecycle operations. Callers (REST handlers)
// use errors.Is to map these to 404/409 responses without importing redis.
var (
	// ErrAlertNotFound indicates the alert does not exist or its state has expired.
	ErrAlertNotFound = errors.New("alert not found")

	// ErrInvalidTransition indicates the requested state change violates the
	// alert state machine (e.g. resolved → acknowledged).
	ErrInvalidTransition = errors.New("invalid alert state transition")
)

const (
	// AlertStateFiring indicates an active, unresolved alert.
	AlertStateFiring AlertState = "firing"

	// AlertStateResolved indicates the condition that triggered the alert has cleared.
	AlertStateResolved AlertState = "resolved"

	// AlertStateAcknowledged indicates a human has acknowledged the alert.
	AlertStateAcknowledged AlertState = "acknowledged"

	// AlertStateSilenced indicates the alert has been temporarily suppressed.
	AlertStateSilenced AlertState = "silenced"
)

// validTransitions defines the allowed state transitions in the alert state machine.
// Terminal state: resolved — no outgoing transitions.
//
//	firing       → resolved, acknowledged, silenced
//	acknowledged → firing, resolved, silenced
//	silenced     → firing, resolved
//	resolved     → (terminal)
var validTransitions = map[AlertState][]AlertState{
	AlertStateFiring:       {AlertStateResolved, AlertStateAcknowledged, AlertStateSilenced},
	AlertStateAcknowledged: {AlertStateFiring, AlertStateResolved, AlertStateSilenced},
	AlertStateSilenced:     {AlertStateFiring, AlertStateResolved},
	AlertStateResolved:     {}, // terminal
}

// AlertTransition records a single state change in the alert lifecycle.
type AlertTransition struct {
	From      AlertState `json:"from"`
	To        AlertState `json:"to"`
	Reason    string     `json:"reason"`
	Timestamp time.Time  `json:"timestamp"`
}

// alertPipeline abstracts the pipeline operations used by AlertOps.
// This avoids exposing the full redis.Pipeliner interface.
type alertPipeline interface {
	RPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
	Exec(ctx context.Context) ([]redis.Cmder, error)
}

// redisCmdable abstracts the subset of redis.Cmdable used by AlertOps.
// Both *redis.Client and *redis.Pipeline satisfy this interface.
type redisCmdable interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd
	Pipeline() alertPipeline
}

// AlertOps provides alert lifecycle management on Dragonfly (hot store).
// All operations are tenant-scoped and idempotent.
type AlertOps struct {
	rdb    redisCmdable
	logger *zap.Logger
}

// NewAlertOps creates a new AlertOps instance backed by the provided redis client.
func NewAlertOps(rdb redisCmdable, logger *zap.Logger) *AlertOps {
	return &AlertOps{
		rdb:    rdb,
		logger: logger,
	}
}

// ---- Key generation ----
//
// Every key is namespaced as paryty:<tenant>:alerts:<domain>:<id> to guarantee
// strict tenant isolation in shared Dragonfly instances.

// alertStateKey returns: paryty:{tenant}:alerts:state:{alertID}
func alertStateKey(tenant, alertID string) string {
	return fmt.Sprintf("paryty:%s:alerts:state:%s", tenant, alertID)
}

// alertMetaKey returns: paryty:{tenant}:alerts:meta:{alertID}
func alertMetaKey(tenant, alertID string) string {
	return fmt.Sprintf("paryty:%s:alerts:meta:%s", tenant, alertID)
}

// alertHistoryKey returns: paryty:{tenant}:alerts:history:{alertID}
func alertHistoryKey(tenant, alertID string) string {
	return fmt.Sprintf("paryty:%s:alerts:history:%s", tenant, alertID)
}

// alertFingerprintKey returns: paryty:{tenant}:alerts:fingerprint:{hash}
func alertFingerprintKey(tenant, hash string) string {
	return fmt.Sprintf("paryty:%s:alerts:fingerprint:%s", tenant, hash)
}

// alertStatePattern returns the SCAN pattern for all alert state keys of a tenant.
func alertStatePattern(tenant string) string {
	return fmt.Sprintf("paryty:%s:alerts:state:*", tenant)
}

// TTL constants for alert keys.
const (
	// alertActiveTTL is the TTL for alerts in active states (firing, acknowledged, silenced).
	alertActiveTTL = 5 * time.Minute

	// alertResolvedTTL is the TTL for resolved alerts — short-lived for idempotency.
	alertResolvedTTL = 1 * time.Minute

	// historyTTL is the TTL for state history entries.
	historyTTL = 1 * time.Hour
)

// ---- State transition ----

// TransitionAlert moves an alert to a new state after validating the transition.
// Returns an error if the transition is invalid or the current state is unknown.
//
// Valid transitions:
//
//	firing       → resolved, acknowledged, silenced
//	acknowledged → firing, resolved, silenced
//	silenced     → firing, resolved
//	resolved     → (terminal, no outgoing transitions)
//
// Transitioning to the same state is a no-op (idempotent).
//
// The state is stored in paryty:{tenant}:alerts:state:{alertID} with a TTL
// appropriate for the new state. The transition is recorded in the history list.
func (a *AlertOps) TransitionAlert(ctx context.Context, tenant, alertID string, newState AlertState, reason string) error {
	if tenant == "" {
		return fmt.Errorf("tenant is required")
	}
	if alertID == "" {
		return fmt.Errorf("alertID is required")
	}
	if !isValidAlertState(newState) {
		return fmt.Errorf("invalid alert state: %q", newState)
	}

	// Retrieve the current state.
	currentState, err := a.GetAlertState(ctx, tenant, alertID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return fmt.Errorf("%w: %s", ErrAlertNotFound, alertID)
		}
		return fmt.Errorf("get current state: %w", err)
	}

	// Short-circuit if the alert is already in the requested state (idempotent).
	if currentState == newState {
		a.logger.Debug("alert already in requested state",
			zap.String("tenant", tenant),
			zap.String("alertID", alertID),
			zap.String("state", string(newState)),
		)
		return nil
	}

	// Validate the transition.
	if !isTransitionAllowed(currentState, newState) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, currentState, newState)
	}

	// Determine TTL based on new state.
	ttl := alertActiveTTL
	if newState == AlertStateResolved {
		ttl = alertResolvedTTL
	}

	// Store the new state.
	if err := a.setAlertState(ctx, tenant, alertID, newState, ttl); err != nil {
		return fmt.Errorf("set alert state: %w", err)
	}

	// Clear fingerprint mapping when alert reaches terminal state.
	if newState == AlertStateResolved {
		if err := a.clearFingerprintForAlert(ctx, tenant, alertID); err != nil {
			a.logger.Warn("failed to clear fingerprint on resolution",
				zap.String("tenant", tenant),
				zap.String("alertID", alertID),
				zap.Error(err),
			)
			// Non-fatal: fingerprint will expire naturally via TTL.
		}
	}

	// Record the transition in history.
	if err := a.StoreAlertTransition(ctx, tenant, alertID, currentState, newState, reason); err != nil {
		a.logger.Warn("failed to store alert transition",
			zap.String("tenant", tenant),
			zap.String("alertID", alertID),
			zap.Error(err),
		)
		// Non-fatal: state change already committed.
	}

	a.logger.Info("alert state transitioned",
		zap.String("tenant", tenant),
		zap.String("alertID", alertID),
		zap.String("from", string(currentState)),
		zap.String("to", string(newState)),
		zap.String("reason", reason),
	)

	return nil
}

// ---- Deduplication ----

// DeduplicateAlert checks whether an alert with the same fingerprint is already
// active (firing/acknowledged/silenced). Returns true if the alert is a duplicate.
//
// The fingerprint is computed from rule_id + sorted labels to ensure idempotent
// deduplication. The fingerprint key is:
//
//	paryty:{tenant}:alerts:fingerprint:{hash}
//
// Returns:
//   - (true, nil)  — duplicate; existing alert is active
//   - (false, nil) — new alert; state stored as firing
//   - (false, err) — storage error
func (a *AlertOps) DeduplicateAlert(ctx context.Context, tenant string, alert *models.Alert) (bool, error) {
	if tenant == "" {
		return false, fmt.Errorf("tenant is required")
	}
	if alert == nil {
		return false, fmt.Errorf("alert is required")
	}

	hash := Fingerprint(alert)
	fpKey := alertFingerprintKey(tenant, hash)

	// Check if an alert with this fingerprint already exists.
	existingAlertID, err := a.rdb.Get(ctx, fpKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, fmt.Errorf("get fingerprint: %w", err)
	}

	if existingAlertID != "" {
		// Fingerprint exists — check the current state of the existing alert.
		existingState, err := a.GetAlertState(ctx, tenant, existingAlertID)
		if err != nil && !errors.Is(err, redis.Nil) {
			return false, fmt.Errorf("get existing alert state: %w", err)
		}

		switch existingState {
		case AlertStateFiring, AlertStateAcknowledged, AlertStateSilenced:
			// Alert is active — this is a duplicate.
			a.logger.Debug("duplicate alert detected",
				zap.String("tenant", tenant),
				zap.String("fingerprint", hash),
				zap.String("existingAlertID", existingAlertID),
				zap.String("existingState", string(existingState)),
			)
			return true, nil

		case AlertStateResolved, "":
			// Existing alert is resolved or missing — allow the new alert.
			a.logger.Debug("previous alert resolved, allowing new firing",
				zap.String("tenant", tenant),
				zap.String("fingerprint", hash),
				zap.String("previousAlertID", existingAlertID),
			)
		}
	}

	// No active duplicate — store the new alert as firing.
	now := time.Now().UTC()
	alert.Status = models.AlertStatusFiring
	alert.UpdatedAt = now
	if alert.StartsAt.IsZero() {
		alert.StartsAt = now
	}

	// Atomically bind the fingerprint to this alert (SetNX ensures only one wins).
	set, err := a.rdb.SetNX(ctx, fpKey, alert.ID, alertActiveTTL).Result()
	if err != nil {
		return false, fmt.Errorf("set fingerprint: %w", err)
	}

	if !set {
		// Another goroutine bound this fingerprint between our GET and SETNX.
		a.logger.Debug("fingerprint already bound by concurrent request",
			zap.String("tenant", tenant),
			zap.String("fingerprint", hash),
		)
		return true, nil
	}

	// Store the alert metadata and state.
	if err := a.storeAlertMeta(ctx, tenant, alert); err != nil {
		return false, fmt.Errorf("store alert meta: %w", err)
	}
	if err := a.setAlertState(ctx, tenant, alert.ID, AlertStateFiring, alertActiveTTL); err != nil {
		return false, fmt.Errorf("set alert state: %w", err)
	}

	a.logger.Info("new alert stored",
		zap.String("tenant", tenant),
		zap.String("alertID", alert.ID),
		zap.String("fingerprint", hash),
		zap.String("severity", string(alert.Severity)),
	)

	return false, nil
}

// ---- Active alert queries ----

// GetActiveAlertsByGroup returns all active alerts grouped by the specified field.
// Supported groupBy values: "severity", "source", "agent_id".
//
// Active alerts are those in firing, acknowledged, or silenced state.
// Resolved alerts are excluded.
func (a *AlertOps) GetActiveAlertsByGroup(ctx context.Context, tenant, groupBy string) (map[string][]models.Alert, error) {
	if tenant == "" {
		return nil, fmt.Errorf("tenant is required")
	}

	// Validate groupBy parameter.
	switch groupBy {
	case "severity", "source", "agent_id":
		// valid
	default:
		return nil, fmt.Errorf("unsupported groupBy: %q (use: severity, source, agent_id)", groupBy)
	}

	result := make(map[string][]models.Alert)
	pattern := alertStatePattern(tenant)

	var cursor uint64
	for {
		keys, nextCursor, err := a.rdb.Scan(ctx, cursor, pattern, int64(scanCount)).Result()
		if err != nil {
			return nil, fmt.Errorf("scan alert states: %w", err)
		}

		for _, key := range keys {
			// Read the alert state.
			stateStr, err := a.rdb.Get(ctx, key).Result()
			if err != nil {
				if errors.Is(err, redis.Nil) {
					continue // expired between SCAN and GET
				}
				a.logger.Warn("failed to get alert state",
					zap.String("key", key),
					zap.Error(err),
				)
				continue
			}

			// Skip non-active alerts.
			state := AlertState(stateStr)
			if !isActiveState(state) {
				continue
			}

			// Extract alert ID from the key: paryty:{tenant}:alerts:state:{alertID}
			alertID := extractAlertIDFromKey(key, tenant)
			if alertID == "" {
				continue
			}

			// Fetch the full alert metadata.
			alert, err := a.getAlertMeta(ctx, tenant, alertID)
			if err != nil {
				if errors.Is(err, redis.Nil) {
					continue // metadata expired
				}
				a.logger.Warn("failed to get alert meta",
					zap.String("alertID", alertID),
					zap.Error(err),
				)
				continue
			}

			// Group by the requested field.
			groupKey := alertGroupKey(alert, groupBy)
			result[groupKey] = append(result[groupKey], *alert)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return result, nil
}

// ---- State and history accessors ----

// GetAlertState returns the current state of an alert.
// Returns redis.Nil wrapped error if the alert does not exist or has expired.
func (a *AlertOps) GetAlertState(ctx context.Context, tenant, alertID string) (AlertState, error) {
	if tenant == "" || alertID == "" {
		return "", fmt.Errorf("tenant and alertID are required")
	}

	val, err := a.rdb.Get(ctx, alertStateKey(tenant, alertID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", err
		}
		return "", fmt.Errorf("get alert state: %w", err)
	}

	return AlertState(val), nil
}

// StoreAlertTransition records a state transition in the alert's history list.
// The history is stored as a Redis list at:
//
//	paryty:{tenant}:alerts:history:{alertID}
//
// Each entry is a JSON-encoded AlertTransition.
func (a *AlertOps) StoreAlertTransition(ctx context.Context, tenant, alertID string, from, to AlertState, reason string) error {
	if tenant == "" || alertID == "" {
		return fmt.Errorf("tenant and alertID are required")
	}

	transition := AlertTransition{
		From:      from,
		To:        to,
		Reason:    reason,
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(transition)
	if err != nil {
		return fmt.Errorf("marshal transition: %w", err)
	}

	hKey := alertHistoryKey(tenant, alertID)

	// Use pipeline to atomically RPUSH + EXPIRE.
	pipe := a.rdb.Pipeline()
	pipe.RPush(ctx, hKey, string(data))
	pipe.Expire(ctx, hKey, historyTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("store transition: %w", err)
	}

	return nil
}

// ---- Fingerprinting ----

// Fingerprint computes a SHA-256 hash of the alert's name, sorted labels, and severity.
// This produces a deterministic identifier for deduplication purposes.
func Fingerprint(alert *models.Alert) string {
	h := sha256.New()

	// Write rule ID (alert name as proxy since Alert doesn't have RuleID).
	h.Write([]byte(alert.Name))

	// Write sorted labels for deterministic hashing.
	if len(alert.Labels) > 0 {
		keys := make([]string, 0, len(alert.Labels))
		for k := range alert.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			h.Write([]byte(k))
			h.Write([]byte("="))
			h.Write([]byte(alert.Labels[k]))
			h.Write([]byte(","))
		}
	}

	// Also include severity to distinguish same-name alerts at different levels.
	h.Write([]byte(string(alert.Severity)))

	return hex.EncodeToString(h.Sum(nil))
}

// ---- Internal helpers ----

// setAlertState stores the alert's current state with the given TTL.
func (a *AlertOps) setAlertState(ctx context.Context, tenant, alertID string, state AlertState, ttl time.Duration) error {
	if err := a.rdb.Set(ctx, alertStateKey(tenant, alertID), string(state), ttl).Err(); err != nil {
		return fmt.Errorf("set state key: %w", err)
	}
	return nil
}

// storeAlertMeta stores the full alert metadata as JSON.
func (a *AlertOps) storeAlertMeta(ctx context.Context, tenant string, alert *models.Alert) error {
	data, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("marshal alert: %w", err)
	}
	if err := a.rdb.Set(ctx, alertMetaKey(tenant, alert.ID), string(data), alertActiveTTL).Err(); err != nil {
		return fmt.Errorf("set meta key: %w", err)
	}
	return nil
}

// getAlertMeta retrieves the full alert metadata.
func (a *AlertOps) getAlertMeta(ctx context.Context, tenant, alertID string) (*models.Alert, error) {
	data, err := a.rdb.Get(ctx, alertMetaKey(tenant, alertID)).Result()
	if err != nil {
		return nil, err
	}
	var alert models.Alert
	if err := json.Unmarshal([]byte(data), &alert); err != nil {
		return nil, fmt.Errorf("unmarshal alert meta: %w", err)
	}
	return &alert, nil
}

// clearFingerprintForAlert removes the fingerprint binding for an alert.
// Called when an alert transitions to the resolved (terminal) state.
func (a *AlertOps) clearFingerprintForAlert(ctx context.Context, tenant, alertID string) error {
	// Read the alert metadata to compute its fingerprint.
	alert, err := a.getAlertMeta(ctx, tenant, alertID)
	if err != nil {
		return fmt.Errorf("get alert for fingerprint clear: %w", err)
	}

	hash := Fingerprint(alert)
	fpKey := alertFingerprintKey(tenant, hash)

	// Only delete if the fingerprint still points to this alert (not a newer one).
	existingID, err := a.rdb.Get(ctx, fpKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil // already cleared
		}
		return fmt.Errorf("get fingerprint: %w", err)
	}

	if existingID == alertID {
		if err := a.rdb.Del(ctx, fpKey).Err(); err != nil {
			return fmt.Errorf("delete fingerprint: %w", err)
		}
	}

	return nil
}

// ---- Validation helpers ----

// isValidAlertState returns true if s is a recognized AlertState.
func isValidAlertState(s AlertState) bool {
	switch s {
	case AlertStateFiring, AlertStateResolved, AlertStateAcknowledged, AlertStateSilenced:
		return true
	default:
		return false
	}
}

// isTransitionAllowed returns true if the transition from → to is allowed
// by the state machine rules.
func isTransitionAllowed(from, to AlertState) bool {
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// isActiveState returns true if the alert is in a non-terminal active state.
func isActiveState(s AlertState) bool {
	switch s {
	case AlertStateFiring, AlertStateAcknowledged, AlertStateSilenced:
		return true
	default:
		return false
	}
}

// extractAlertIDFromKey extracts the alert ID from a key of the form:
//
//	paryty:{tenant}:alerts:state:{alertID}
//
// Returns empty string if the key does not match the expected pattern.
func extractAlertIDFromKey(key, tenant string) string {
	prefix := fmt.Sprintf("paryty:%s:alerts:state:", tenant)
	if len(key) <= len(prefix) {
		return ""
	}
	return key[len(prefix):]
}

// alertGroupKey returns the group key for an alert based on the groupBy field.
func alertGroupKey(alert *models.Alert, groupBy string) string {
	switch groupBy {
	case "severity":
		return string(alert.Severity)
	case "source":
		if alert.Source == "" {
			return "unknown"
		}
		return alert.Source
	case "agent_id":
		if alert.AgentID == "" {
			return "unknown"
		}
		return alert.AgentID
	default:
		return "unknown"
	}
}
