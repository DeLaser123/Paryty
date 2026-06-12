package auth

import (
	"fmt"
	"testing"
	"time"
)

// =============================================================================
// loginRateLimiter regression tests
//
// These tests pin the brute-force protection contract:
//   - exactly maxLoginFailures attempts are permitted per window
//   - the attempt after the limit is blocked for loginBlockTime
//   - an expired block resets cleanly
//   - successful login clears state
//   - the tracking map is bounded (memory-exhaustion DoS regression)
// =============================================================================

func TestLoginRateLimiter_AllowsExactlyMaxFailures(t *testing.T) {
	l := newLoginRateLimiter()

	for i := 1; i <= maxLoginFailures; i++ {
		if !l.allow("user@example.com") {
			t.Fatalf("attempt %d should be allowed (limit is %d)", i, maxLoginFailures)
		}
	}
	if l.allow("user@example.com") {
		t.Fatalf("attempt %d should be blocked", maxLoginFailures+1)
	}
}

func TestLoginRateLimiter_BlockPersistsWithinBlockWindow(t *testing.T) {
	l := newLoginRateLimiter()

	for i := 0; i < maxLoginFailures; i++ {
		l.allow("user@example.com")
	}
	if l.allow("user@example.com") {
		t.Fatal("should be blocked after exceeding the limit")
	}
	// Still blocked on subsequent attempts.
	if l.allow("user@example.com") {
		t.Fatal("should remain blocked on repeated attempts")
	}
}

func TestLoginRateLimiter_ExpiredBlockResets(t *testing.T) {
	l := newLoginRateLimiter()

	for i := 0; i < maxLoginFailures+1; i++ {
		l.allow("user@example.com")
	}

	// Simulate the block and window expiring.
	l.mu.Lock()
	w := l.attempts["user@example.com"]
	w.blockedUntil = time.Now().Add(-time.Second)
	w.windowEndsAt = time.Now().Add(-time.Second)
	l.mu.Unlock()

	if !l.allow("user@example.com") {
		t.Fatal("attempt after block expiry should be allowed")
	}
}

func TestLoginRateLimiter_SuccessClearsState(t *testing.T) {
	l := newLoginRateLimiter()

	for i := 0; i < maxLoginFailures-1; i++ {
		l.allow("user@example.com")
	}
	l.recordSuccess("user@example.com")

	// Full budget should be available again.
	for i := 1; i <= maxLoginFailures; i++ {
		if !l.allow("user@example.com") {
			t.Fatalf("attempt %d after success should be allowed", i)
		}
	}
}

func TestLoginRateLimiter_IndependentKeys(t *testing.T) {
	l := newLoginRateLimiter()

	for i := 0; i < maxLoginFailures+1; i++ {
		l.allow("attacker@example.com")
	}
	if l.allow("attacker@example.com") {
		t.Fatal("attacker key should be blocked")
	}
	if !l.allow("legit@example.com") {
		t.Fatal("unrelated key must not be affected")
	}
}

func TestLoginRateLimiter_MapIsBounded(t *testing.T) {
	l := newLoginRateLimiter()

	// Insert active (non-expired) entries up to the cap.
	now := time.Now()
	l.mu.Lock()
	for i := 0; i < maxTrackedKeys; i++ {
		l.attempts[fmt.Sprintf("u%d@example.com", i)] = &loginWindow{
			failures:     1,
			windowEndsAt: now.Add(loginWindowSize),
		}
	}
	l.mu.Unlock()

	// A new key must still be allowed (fail-open) but NOT grow the map.
	if !l.allow("overflow@example.com") {
		t.Fatal("untracked key should fail open when map is full")
	}
	l.mu.Lock()
	size := len(l.attempts)
	l.mu.Unlock()
	if size > maxTrackedKeys {
		t.Fatalf("map grew beyond bound: %d > %d", size, maxTrackedKeys)
	}
}

func TestLoginRateLimiter_PruneEvictsExpiredEntries(t *testing.T) {
	l := newLoginRateLimiter()

	// Fill the map with EXPIRED entries.
	past := time.Now().Add(-time.Hour)
	l.mu.Lock()
	for i := 0; i < maxTrackedKeys; i++ {
		l.attempts[fmt.Sprintf("u%d@example.com", i)] = &loginWindow{
			failures:     1,
			windowEndsAt: past,
			blockedUntil: past,
		}
	}
	l.mu.Unlock()

	// New attempt triggers pruning, frees space, and is tracked.
	if !l.allow("fresh@example.com") {
		t.Fatal("attempt should be allowed after pruning expired entries")
	}
	l.mu.Lock()
	_, tracked := l.attempts["fresh@example.com"]
	size := len(l.attempts)
	l.mu.Unlock()
	if !tracked {
		t.Fatal("fresh key should be tracked after pruning made room")
	}
	if size > maxTrackedKeys {
		t.Fatalf("map exceeds bound after pruning: %d", size)
	}
}

// =============================================================================
// timing-equalization hash regression test
// =============================================================================

func TestDummyBcryptHash_IsValidBcrypt(t *testing.T) {
	// The dummy hash must be a well-formed bcrypt hash so VerifyPassword
	// burns full bcrypt cost (a malformed hash returns instantly and
	// silently defeats the user-enumeration timing defense).
	start := time.Now()
	err := VerifyPassword("definitely-not-the-password", dummyBcryptHash)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("dummy hash must never verify successfully")
	}
	// bcrypt cost 12 takes well over 10ms on any hardware; a malformed-hash
	// early return takes microseconds.
	if elapsed < 10*time.Millisecond {
		t.Fatalf("verification returned too fast (%v) — dummy hash is likely malformed", elapsed)
	}
}
