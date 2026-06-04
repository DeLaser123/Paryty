// Live test: Agent offline detection against running Dragonfly.
//
// This test proves that ConnectionOps.DetectDisconnectedAgents() works
// against the live Dragonfly instance, not just miniredis.
//
// Run: go run ./cmd/offline-test/
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/storage/hot"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Connect to live Dragonfly.
	client := hot.New(hot.Config{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		PoolSize: 5,
	})
	defer client.Close()

	co := hot.NewConnectionOps(client, logger)
	ctx := context.Background()
	tenant := "offline-test"

	fmt.Println("=== Agent Offline Detection Live Test ===")
	fmt.Println()

	// Register 3 agents.
	now := time.Now().UTC()
	agents := []*hot.ConnectionInfo{
		{
			AgentID:       "agent-fresh-001",
			SessionID:     "s-fresh-001",
			ConnectedAt:   now.Add(-2 * time.Minute),
			LastHeartbeat: now.Add(-10 * time.Second), // FRESH
			Capabilities:  []string{"metrics"},
		},
		{
			AgentID:       "agent-stale-002",
			SessionID:     "s-stale-002",
			ConnectedAt:   now.Add(-10 * time.Minute),
			LastHeartbeat: now.Add(-3 * time.Minute), // STALE
			Capabilities:  []string{"traces"},
		},
		{
			AgentID:       "agent-stale-003",
			SessionID:     "s-stale-003",
			ConnectedAt:   now.Add(-8 * time.Minute),
			LastHeartbeat: now.Add(-5 * time.Minute), // VERY STALE
			Capabilities:  []string{"logs"},
		},
	}

	fmt.Println("[1] Registering 3 agents in Dragonfly...")
	for _, a := range agents {
		if err := co.TrackConnection(ctx, tenant, a); err != nil {
			fmt.Printf("  FAIL: TrackConnection(%s): %v\n", a.AgentID, err)
			os.Exit(1)
		}
		fmt.Printf("  OK: Registered %s (last heartbeat: %s ago)\n",
			a.AgentID, now.Sub(a.LastHeartbeat).Round(time.Second))
	}

	fmt.Println()
	fmt.Println("[2] Detecting disconnected agents (threshold: 60s)...")
	disconnected, err := co.DetectDisconnectedAgents(ctx, tenant, 60*time.Second)
	if err != nil {
		fmt.Printf("  FAIL: DetectDisconnectedAgents: %v\n", err)
		os.Exit(1)
	}

	disconnectedIDs := make(map[string]bool)
	for _, d := range disconnected {
		disconnectedIDs[d.AgentID] = true
	}

	// Verify results.
	fmt.Println()
	fmt.Println("[3] Results:")
	allCorrect := true

	// agent-fresh-001 should NOT be disconnected.
	if disconnectedIDs["agent-fresh-001"] {
		fmt.Println("  FAIL: agent-fresh-001 should NOT be disconnected (heartbeat 10s ago)")
		allCorrect = false
	} else {
		fmt.Println("  OK: agent-fresh-001 is ONLINE (heartbeat 10s ago)")
	}

	// agent-stale-002 SHOULD be disconnected.
	if disconnectedIDs["agent-stale-002"] {
		fmt.Println("  OK: agent-stale-002 is OFFLINE (heartbeat 3min ago)")
	} else {
		fmt.Println("  FAIL: agent-stale-002 should be disconnected (heartbeat 3min ago)")
		allCorrect = false
	}

	// agent-stale-003 SHOULD be disconnected.
	if disconnectedIDs["agent-stale-003"] {
		fmt.Println("  OK: agent-stale-003 is OFFLINE (heartbeat 5min ago)")
	} else {
		fmt.Println("  FAIL: agent-stale-003 should be disconnected (heartbeat 5min ago)")
		allCorrect = false
	}

	// Cleanup.
	fmt.Println()
	fmt.Println("[4] Cleaning up test data...")
	connections, _ := co.GetActiveConnections(ctx, tenant)
	fmt.Printf("  Active connections for tenant '%s': %d\n", tenant, len(connections))

	fmt.Println()
	if allCorrect {
		fmt.Println("=== ALL CHECKS PASSED ===")
		fmt.Println("Agent offline detection works against live Dragonfly.")
	} else {
		fmt.Println("=== SOME CHECKS FAILED ===")
		os.Exit(1)
	}
}
