package security

import (
	"fmt"
	"os"
)

// SecretsManager provides a unified interface for accessing secrets.
// Currently backed by environment variables, designed to be swapped for
// HashiCorp Vault, AWS Secrets Manager, or similar in production without
// changing the calling code.
type SecretsManager struct {
	prefix string // environment variable prefix, e.g. "PARYTY_"
}

// NewSecretsManager creates a SecretsManager with the given environment
// variable prefix (e.g., "PARYTY_").
func NewSecretsManager(prefix string) *SecretsManager {
	if prefix == "" {
		prefix = "PARYTY_"
	}
	return &SecretsManager{prefix: prefix}
}

// Get returns a secret value by key. The key is prefixed with the configured
// prefix and uppercased for environment variable lookup.
// Returns an error if the secret is not set (fail-fast on missing critical secrets).
func (sm *SecretsManager) Get(key string) (string, error) {
	envKey := sm.prefix + key
	value := os.Getenv(envKey)
	if value == "" {
		return "", fmt.Errorf("secret %q is not set (env: %s)", key, envKey)
	}
	return value, nil
}

// GetOptional returns a secret value by key, or the fallback if not set.
func (sm *SecretsManager) GetOptional(key, fallback string) string {
	envKey := sm.prefix + key
	value := os.Getenv(envKey)
	if value == "" {
		return fallback
	}
	return value
}

// MustGet panics if the secret is not set. Use only at program initialization.
func (sm *SecretsManager) MustGet(key string) string {
	value, err := sm.Get(key)
	if err != nil {
		panic(fmt.Sprintf("required secret not set: %s", key))
	}
	return value
}

// =============================================================================
// Well-known secret keys
// =============================================================================

const (
	// SecretJWTSecret is the HMAC-SHA256 signing key for JWT tokens.
	SecretJWTSecret = "JWT_SECRET"

	// SecretDBPassword is the PostgreSQL connection password.
	SecretDBPassword = "DB_PASSWORD"

	// SecretAPIKeyEncryptionKey is used for API key hashing (pepper).
	SecretAPIKeyEncryptionKey = "API_KEY_ENCRYPTION_KEY"

	// SecretRedpandaPassword is the Redpanda/Kafka SASL password.
	SecretRedpandaPassword = "REDPANDA_PASSWORD"
)
