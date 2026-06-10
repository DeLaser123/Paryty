package auth

import (
	"fmt"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const (
	// BcryptCost is the bcrypt hashing cost factor. 12 is the current
	// recommendation for production (approx. 250ms per hash on modern hardware).
	BcryptCost = 12

	// MinPasswordLength is the minimum acceptable password length.
	MinPasswordLength = 8

	// MaxPasswordLength is the maximum password length before bcrypt truncation.
	// bcrypt silently truncates at 72 bytes, so we enforce this limit explicitly.
	MaxPasswordLength = 72
)

// HashPassword securely hashes a password using bcrypt with the configured
// cost factor. Returns the bcrypt hash string (including algorithm, cost, salt).
func HashPassword(password string) (string, error) {
	if len(password) > MaxPasswordLength {
		return "", fmt.Errorf("password exceeds maximum length of %d bytes", MaxPasswordLength)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(hash), nil
}

// VerifyPassword compares a plaintext password against a bcrypt hash using
// constant-time comparison. Returns nil if they match.
func VerifyPassword(password, hashedPassword string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		return fmt.Errorf("password verification failed: %w", err)
	}
	return nil
}

// ValidatePasswordPolicy checks that a password meets the minimum security
// requirements:
//   - At least 8 characters
//   - At least one uppercase letter
//   - At least one lowercase letter
//   - At least one digit
//   - At least one special character
//
// Returns nil if the password passes all checks, or an error describing the
// first failed check.
func ValidatePasswordPolicy(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	if len(password) > MaxPasswordLength {
		return fmt.Errorf("password must be at most %d characters", MaxPasswordLength)
	}

	var (
		hasUpper   bool
		hasLower   bool
		hasDigit   bool
		hasSpecial bool
	)

	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	if !hasUpper {
		return fmt.Errorf("password must contain at least one uppercase letter")
	}
	if !hasLower {
		return fmt.Errorf("password must contain at least one lowercase letter")
	}
	if !hasDigit {
		return fmt.Errorf("password must contain at least one digit")
	}
	if !hasSpecial {
		return fmt.Errorf("password must contain at least one special character")
	}

	return nil
}
