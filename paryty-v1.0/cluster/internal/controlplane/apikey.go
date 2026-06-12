package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidAPIKey is returned when an API key fails validation.
var ErrInvalidAPIKey = errors.New("invalid api key")

// rawKeyPrefix is the standard prefix for all Paryty live API keys.
const rawKeyPrefix = "pk_live_"

// APIKeyManager provides CRUD and validation operations for API keys.
// All methods are safe for concurrent use (pgxpool handles connection pooling).
type APIKeyManager struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

// NewAPIKeyManager creates an APIKeyManager wired to the given pool and logger.
// Panics if logger is nil.
func NewAPIKeyManager(pool *pgxpool.Pool, logger *zap.Logger) *APIKeyManager {
	if logger == nil {
		panic("controlplane.APIKeyManager: logger must not be nil")
	}
	return &APIKeyManager{
		pool:   pool,
		logger: logger.Named("apikey-manager"),
	}
}

// GenerateKey creates a new API key for the given tenant.
//
// The raw key is returned exactly once ("pk_live_" + 64 hex chars). Only a
// bcrypt hash and the first 16 characters (prefix) are persisted. The caller
// must relay the raw key to the tenant -- it cannot be recovered later.
func (m *APIKeyManager) GenerateKey(ctx context.Context, tenantID, name string) (rawKey string, keyID string, err error) {
	if tenantID == "" {
		return "", "", fmt.Errorf("generate key: tenant_id must not be empty")
	}

	// Generate 32 cryptographically random bytes -> 64 hex chars.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate random bytes: %w", err)
	}
	rawKey = rawKeyPrefix + hex.EncodeToString(b)

	// bcrypt hash for storage (cost = DefaultCost = 10).
	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("bcrypt hash: %w", err)
	}

	// First 16 chars used for O(1) lookup and display.
	prefix := rawKey[:16]

	keyID = uuid.New().String()

	_, err = m.pool.Exec(ctx,
		`INSERT INTO api_keys (key_id, tenant_id, name, key_hash, key_prefix)
		 VALUES ($1, $2, $3, $4, $5)`,
		keyID, tenantID, name, string(hash), prefix,
	)
	if err != nil {
		return "", "", fmt.Errorf("store api key: %w", err)
	}

	m.logger.Info("API key generated",
		zap.String("key_id", keyID),
		zap.String("tenant_id", tenantID),
		zap.String("name", name),
		zap.String("key_prefix", prefix),
	)

	return rawKey, keyID, nil
}

// ValidateKey checks the supplied raw API key against stored bcrypt hashes.
//
// It uses the key prefix (first 16 chars) for an indexed lookup, then verifies
// the full key with bcrypt.CompareHashAndPassword. Returns the owning tenant
// ID on success or ErrInvalidAPIKey on failure.
func (m *APIKeyManager) ValidateKey(ctx context.Context, apiKey string) (string, error) {
	if len(apiKey) < 16 {
		return "", ErrInvalidAPIKey
	}

	prefix := apiKey[:16]

	rows, err := m.pool.Query(ctx,
		`SELECT key_hash, tenant_id
		 FROM api_keys
		 WHERE key_prefix = $1 AND revoked_at IS NULL`,
		prefix,
	)
	if err != nil {
		return "", fmt.Errorf("query api keys by prefix: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var keyHash, tenantID string
		if err := rows.Scan(&keyHash, &tenantID); err != nil {
			return "", fmt.Errorf("scan api key row: %w", err)
		}

		if err := bcrypt.CompareHashAndPassword([]byte(keyHash), []byte(apiKey)); err == nil {
			return tenantID, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate api key rows: %w", err)
	}

	return "", ErrInvalidAPIKey
}

// RevokeKey soft-deletes an API key by setting its revoked_at timestamp.
// Returns an error if the key does not exist or is already revoked.
func (m *APIKeyManager) RevokeKey(ctx context.Context, keyID string) error {
	if keyID == "" {
		return fmt.Errorf("revoke key: key_id must not be empty")
	}

	tag, err := m.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = now()
		 WHERE key_id = $1 AND revoked_at IS NULL`,
		keyID,
	)
	if err != nil {
		return fmt.Errorf("revoke key %s: %w", keyID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("revoke key %s: not found or already revoked", keyID)
	}

	m.logger.Info("API key revoked",
		zap.String("key_id", keyID),
	)
	return nil
}

// ListKeys returns all API keys for the given tenant, ordered by creation
// time (newest first). The key hash is never included in the result.
func (m *APIKeyManager) ListKeys(ctx context.Context, tenantID string) ([]APIKey, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("list keys: tenant_id must not be empty")
	}

	rows, err := m.pool.Query(ctx,
		`SELECT key_id, tenant_id, name, key_prefix, created_at, revoked_at
		 FROM api_keys
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list keys for tenant %s: %w", tenantID, err)
	}
	defer rows.Close()

	keys := make([]APIKey, 0)
	for rows.Next() {
		var k APIKey
		var createdAt time.Time
		var revokedAt *time.Time
		if err := rows.Scan(&k.KeyID, &k.TenantID, &k.Name, &k.KeyPrefix, &createdAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		k.CreatedAt = createdAt.Format(time.RFC3339)
		if revokedAt != nil {
			s := revokedAt.Format(time.RFC3339)
			k.RevokedAt = &s
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}
	return keys, nil
}

// RotateKey generates a new raw key for an existing API key ID.
// The old key hash is replaced in-place with the new hash and prefix.
// Returns the new raw key (shown once) or an error if the key doesn't exist.
func (m *APIKeyManager) RotateKey(ctx context.Context, keyID string) (rawKey string, err error) {
	if keyID == "" {
		return "", fmt.Errorf("rotate key: key_id must not be empty")
	}

	// Generate new 32 random bytes -> 64 hex chars.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	rawKey = rawKeyPrefix + hex.EncodeToString(b)

	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt hash: %w", err)
	}

	prefix := rawKey[:16]

	tag, err := m.pool.Exec(ctx,
		`UPDATE api_keys SET key_hash = $1, key_prefix = $2
		 WHERE key_id = $3 AND revoked_at IS NULL`,
		string(hash), prefix, keyID,
	)
	if err != nil {
		return "", fmt.Errorf("rotate key %s: %w", keyID, err)
	}
	if tag.RowsAffected() == 0 {
		return "", fmt.Errorf("rotate key %s: not found or revoked", keyID)
	}

	m.logger.Info("API key rotated",
		zap.String("key_id", keyID),
		zap.String("new_prefix", prefix),
	)
	return rawKey, nil
}
