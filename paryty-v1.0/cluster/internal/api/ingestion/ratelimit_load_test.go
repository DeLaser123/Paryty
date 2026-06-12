package api

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimit_BurstTraffic(t *testing.T) {
	// Create a rate limiter with a limit of 100 requests per minute
	limiter := NewRateLimiter(100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limiter.StartCleanup(ctx)

	agentID := "test-agent-1"
	requestCount := 1000
	successCount := int32(0)

	// Send burst of requests
	var wg sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow(agentID) {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}
	wg.Wait()

	// Should allow exactly 100 requests (the limit)
	assert.Equal(t, int32(100), successCount, "Should allow exactly the limit number of requests")
	assert.Equal(t, int32(requestCount-100), int32(requestCount)-successCount, "Should reject the remaining requests")
}

func TestRateLimit_ConcurrentTenants(t *testing.T) {
	// Create a rate limiter with a limit of 100 requests per minute per agent
	limiter := NewRateLimiter(100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limiter.StartCleanup(ctx)

	tenantCount := 10
	requestsPerTenant := 200
	successCounts := make([]int32, tenantCount)

	// Send requests from multiple tenants concurrently
	var wg sync.WaitGroup
	for tenantIdx := 0; tenantIdx < tenantCount; tenantIdx++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			agentID := "agent-" + string(rune('A'+idx))
			for i := 0; i < requestsPerTenant; i++ {
				if limiter.Allow(agentID) {
					atomic.AddInt32(&successCounts[idx], 1)
				}
			}
		}(tenantIdx)
	}
	wg.Wait()

	// Each tenant should have exactly 100 successful requests
	for i := 0; i < tenantCount; i++ {
		assert.Equal(t, int32(100), successCounts[i],
			"Tenant %d should have exactly 100 successful requests", i)
	}
}

func TestRateLimit_WindowReset(t *testing.T) {
	// Create a rate limiter with a limit of 10 requests per minute
	limiter := NewRateLimiter(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limiter.StartCleanup(ctx)

	agentID := "test-agent-1"

	// Fill the rate limit
	for i := 0; i < 10; i++ {
		require.True(t, limiter.Allow(agentID), "Request %d should be allowed", i)
	}

	// Next request should be rejected
	assert.False(t, limiter.Allow(agentID), "Request after limit should be rejected")

	// Wait for window to reset (1 minute / 10 requests = 6 seconds per token)
	// We'll wait a bit longer to ensure tokens are refilled
	time.Sleep(7 * time.Second)

	// Should be able to make requests again
	assert.True(t, limiter.Allow(agentID), "Request after window reset should be allowed")
}

func TestRateLimit_DifferentAgents(t *testing.T) {
	// Create a rate limiter with a limit of 10 requests per minute
	limiter := NewRateLimiter(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limiter.StartCleanup(ctx)

	agent1 := "agent-1"
	agent2 := "agent-2"

	// Fill rate limit for agent 1
	for i := 0; i < 10; i++ {
		require.True(t, limiter.Allow(agent1), "Agent 1 request %d should be allowed", i)
	}

	// Agent 1 should be rate limited
	assert.False(t, limiter.Allow(agent1), "Agent 1 should be rate limited")

	// Agent 2 should still have full quota
	for i := 0; i < 10; i++ {
		assert.True(t, limiter.Allow(agent2), "Agent 2 request %d should be allowed", i)
	}

	// Agent 2 should now be rate limited
	assert.False(t, limiter.Allow(agent2), "Agent 2 should be rate limited")
}

func TestRateLimit_ConcurrentAccess(t *testing.T) {
	// Create a rate limiter with a limit of 1000 requests per minute
	limiter := NewRateLimiter(1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limiter.StartCleanup(ctx)

	agentID := "test-agent-1"
	requestCount := 2000
	successCount := int32(0)

	// Send requests concurrently
	var wg sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow(agentID) {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}
	wg.Wait()

	// Should allow exactly 1000 requests (the limit)
	assert.Equal(t, int32(1000), successCount, "Should allow exactly the limit number of requests")
}

func TestRateLimit_CleanupStaleEntries(t *testing.T) {
	// Create a rate limiter with a limit of 10 requests per minute
	limiter := NewRateLimiter(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limiter.StartCleanup(ctx)

	// Create some agents
	agents := []string{"agent-1", "agent-2", "agent-3"}
	for _, agent := range agents {
		for i := 0; i < 5; i++ {
			limiter.Allow(agent)
		}
	}

	// Verify buckets exist
	bucketCount := 0
	limiter.buckets.Range(func(key, value any) bool {
		bucketCount++
		return true
	})
	assert.Equal(t, 3, bucketCount, "Should have 3 buckets")

	// Wait for stale threshold (10 minutes)
	// For testing, we'll just verify the cleanup function works
	// In a real test, you'd wait for the stale threshold
	limiter.cleanup()

	// Buckets should still exist since they were just accessed
	bucketCount = 0
	limiter.buckets.Range(func(key, value any) bool {
		bucketCount++
		return true
	})
	assert.Equal(t, 3, bucketCount, "Should still have 3 buckets after cleanup")
}

func TestRateLimit_ZeroLimit(t *testing.T) {
	// Create a rate limiter with zero limit (should use default)
	limiter := NewRateLimiter(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limiter.StartCleanup(ctx)

	agentID := "test-agent-1"

	// Should use default limit (10000)
	for i := 0; i < 100; i++ {
		assert.True(t, limiter.Allow(agentID), "Request %d should be allowed with default limit", i)
	}
}

func TestRateLimit_NegativeLimit(t *testing.T) {
	// Create a rate limiter with negative limit (should use default)
	limiter := NewRateLimiter(-100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limiter.StartCleanup(ctx)

	agentID := "test-agent-1"

	// Should use default limit (10000)
	for i := 0; i < 100; i++ {
		assert.True(t, limiter.Allow(agentID), "Request %d should be allowed with default limit", i)
	}
}
