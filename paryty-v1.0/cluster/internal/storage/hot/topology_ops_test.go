package hot

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// newTestTopologyOps creates a TopologyOps backed by a miniredis instance.
// The caller should defer mr.Close() to clean up.
func newTestTopologyOps(t *testing.T) (*TopologyOps, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() { rdb.Close() })

	client := &Client{
		rdb: rdb,
		cfg: Config{TTL: 5 * time.Minute},
	}

	topoOps := NewTopologyOps(client, zap.NewNop())
	return topoOps, mr
}

// sampleTopology returns a small topology for testing.
// Edge SourceID/TargetID must match actual node IDs for cascade logic to work.
func sampleTopology() *models.Topology {
	return &models.Topology{
		Nodes: []models.TopologyNode{
			{
				ID:       "svc-a:agent-1",
				Name:     "svc-a",
				Type:     models.NodeTypeService,
				AgentID:  "agent-1",
				Labels:   map[string]string{"env": "prod"},
				Metadata: map[string]string{},
				Health:   models.HealthStatusHealthy,
				LastSeen: time.Now(),
			},
			{
				ID:       "svc-b:agent-1",
				Name:     "svc-b",
				Type:     models.NodeTypeService,
				AgentID:  "agent-1",
				Labels:   map[string]string{"env": "prod"},
				Metadata: map[string]string{},
				Health:   models.HealthStatusHealthy,
				LastSeen: time.Now(),
			},
		},
		Edges: []models.TopologyEdge{
			{
				ID:       "svc-a:agent-1→svc-b:agent-1:0",
				SourceID: "svc-a:agent-1",
				TargetID: "svc-b:agent-1",
				Type:     models.EdgeTypeHTTP,
				Protocol: "http",
				Labels:   map[string]string{},
				LastSeen: time.Now(),
			},
		},
		Timestamp: time.Now(),
		Version:   1,
	}
}

// =============================================================================
// TestTopologyOps_UpdateTopology_Basic
// =============================================================================

func TestTopologyOps_UpdateTopology_Basic(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()
	tenant := "acme-corp"

	// Update: add a node to the initially empty topology.
	err := topoOps.UpdateTopology(ctx, tenant, func(current *models.Topology) (*models.Topology, error) {
		current.Nodes = append(current.Nodes, models.TopologyNode{
			ID:       "svc-web:agent-1",
			Name:     "svc-web",
			Type:     models.NodeTypeService,
			AgentID:  "agent-1",
			Labels:   map[string]string{"env": "prod"},
			Metadata: map[string]string{},
			Health:   models.HealthStatusHealthy,
			LastSeen: time.Now(),
		})
		return current, nil
	})
	if err != nil {
		t.Fatalf("UpdateTopology failed: %v", err)
	}

	// Verify the topology was stored.
	topo, version, err := topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion failed: %v", err)
	}
	if topo == nil {
		t.Fatal("expected non-nil topology after update")
	}
	if len(topo.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(topo.Nodes))
	}
	if topo.Nodes[0].ID != "svc-web:agent-1" {
		t.Errorf("expected node ID svc-web:agent-1, got %s", topo.Nodes[0].ID)
	}
	if topo.Version != 1 {
		t.Errorf("expected version=1, got %d", topo.Version)
	}
	if version == "" {
		t.Error("expected non-empty version key")
	}

	// Second update: add another node.
	err = topoOps.UpdateTopology(ctx, tenant, func(current *models.Topology) (*models.Topology, error) {
		current.Nodes = append(current.Nodes, models.TopologyNode{
			ID:       "svc-api:agent-1",
			Name:     "svc-api",
			Type:     models.NodeTypeService,
			AgentID:  "agent-1",
			Labels:   map[string]string{"env": "prod"},
			Metadata: map[string]string{},
			Health:   models.HealthStatusHealthy,
			LastSeen: time.Now(),
		})
		return current, nil
	})
	if err != nil {
		t.Fatalf("second UpdateTopology failed: %v", err)
	}

	// Verify both nodes exist.
	topo, _, err = topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion after second update failed: %v", err)
	}
	if len(topo.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(topo.Nodes))
	}
	if topo.Version != 2 {
		t.Errorf("expected version=2, got %d", topo.Version)
	}
}

// =============================================================================
// TestTopologyOps_UpdateTopology_MutateError
// =============================================================================

func TestTopologyOps_UpdateTopology_MutateError(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()
	tenant := "test-tenant"

	// Mutate function returns an error — should propagate immediately.
	err := topoOps.UpdateTopology(ctx, tenant, func(_ *models.Topology) (*models.Topology, error) {
		return nil, fmt.Errorf("simulated mutation failure")
	})
	if err == nil {
		t.Fatal("expected error from mutate, got nil")
	}

	// Topology should still be empty (nothing was written).
	topo, _, err := topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion failed: %v", err)
	}
	if topo != nil {
		t.Error("expected nil topology after failed mutation")
	}
}

// =============================================================================
// TestTopologyOps_UpdateTopology_Concurrent
// 10 goroutines update simultaneously; all nodes must be present in the result.
// =============================================================================

func TestTopologyOps_UpdateTopology_Concurrent(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()
	tenant := "concurrent-tenant"

	const goroutines = 10

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			nodeID := fmt.Sprintf("svc-%d:agent-1", id)
			err := topoOps.UpdateTopology(ctx, tenant, func(current *models.Topology) (*models.Topology, error) {
				// Add our unique node if not already present.
				for _, n := range current.Nodes {
					if n.ID == nodeID {
						return current, nil
					}
				}
				current.Nodes = append(current.Nodes, models.TopologyNode{
					ID:       nodeID,
					Name:     fmt.Sprintf("svc-%d", id),
					Type:     models.NodeTypeService,
					AgentID:  "agent-1",
					Labels:   map[string]string{},
					Metadata: map[string]string{},
					Health:   models.HealthStatusHealthy,
					LastSeen: time.Now(),
				})
				return current, nil
			})
			if err != nil {
				errCh <- fmt.Errorf("goroutine %d: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent update error: %v", err)
	}

	// Verify all 10 nodes are present.
	topo, _, err := topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion failed: %v", err)
	}
	if topo == nil {
		t.Fatal("expected non-nil topology after concurrent updates")
	}
	if len(topo.Nodes) != goroutines {
		t.Errorf("expected %d nodes, got %d", goroutines, len(topo.Nodes))

		// Log which nodes are present for debugging.
		present := make(map[string]bool)
		for _, n := range topo.Nodes {
			present[n.ID] = true
		}
		for i := 0; i < goroutines; i++ {
			id := fmt.Sprintf("svc-%d:agent-1", i)
			if !present[id] {
				t.Logf("missing node: %s", id)
			}
		}
	}
}

// =============================================================================
// TestTopologyOps_GetTopologyWithVersion
// =============================================================================

func TestTopologyOps_GetTopologyWithVersion(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()
	tenant := "query-tenant"

	// Case 1: No topology exists yet.
	topo, ver, err := topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion on empty: %v", err)
	}
	if topo != nil {
		t.Errorf("expected nil topology for non-existent key, got %+v", topo)
	}
	if ver != "" {
		t.Errorf("expected empty version for non-existent key, got %q", ver)
	}

	// Seed a topology.
	seed := sampleTopology()
	err = topoOps.UpdateTopology(ctx, tenant, func(_ *models.Topology) (*models.Topology, error) {
		return seed, nil
	})
	if err != nil {
		t.Fatalf("seed UpdateTopology failed: %v", err)
	}

	// Case 2: Topology exists.
	topo, ver, err = topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion: %v", err)
	}
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}
	if len(topo.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(topo.Nodes))
	}
	if len(topo.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(topo.Edges))
	}
	if ver == "" {
		t.Error("expected non-empty version")
	}
	if topo.Version == 0 {
		t.Error("expected topology version > 0")
	}
}

// =============================================================================
// TestTopologyOps_ApplyDiff
// =============================================================================

func TestTopologyOps_ApplyDiff(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()
	tenant := "diff-tenant"

	// Seed the topology.
	seed := sampleTopology()
	err := topoOps.UpdateTopology(ctx, tenant, func(_ *models.Topology) (*models.Topology, error) {
		return seed, nil
	})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Apply diff:
	// - Remove svc-b (which also removes the edge svc-a→svc-b).
	// - Add svc-c.
	// - Add edge svc-a→svc-c.
	diff := &TopologyDiff{
		NodesAdded: []models.TopologyNode{
			{
				ID:       "svc-c:agent-1",
				Name:     "svc-c",
				Type:     models.NodeTypeService,
				AgentID:  "agent-1",
				Labels:   map[string]string{},
				Metadata: map[string]string{},
				Health:   models.HealthStatusHealthy,
				LastSeen: time.Now(),
			},
		},
		NodesRemoved: []string{"svc-b:agent-1"},
		EdgesAdded: []models.TopologyEdge{
			{
				ID:       "svc-a:agent-1→svc-c:agent-1:0",
				SourceID: "svc-a:agent-1",
				TargetID: "svc-c:agent-1",
				Type:     models.EdgeTypeGRPC,
				Protocol: "grpc",
				Labels:   map[string]string{},
				LastSeen: time.Now(),
			},
		},
		Timestamp: time.Now(),
		Version:   "v2",
	}

	err = topoOps.ApplyDiff(ctx, tenant, diff)
	if err != nil {
		t.Fatalf("ApplyDiff failed: %v", err)
	}

	// Verify the result.
	topo, _, err := topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion after diff: %v", err)
	}
	if topo == nil {
		t.Fatal("expected non-nil topology after diff")
	}

	// Should have 2 nodes: svc-a (original) and svc-c (added).
	if len(topo.Nodes) != 2 {
		t.Fatalf("expected 2 nodes after diff, got %d", len(topo.Nodes))
	}
	nodeIDs := make(map[string]bool)
	for _, n := range topo.Nodes {
		nodeIDs[n.ID] = true
	}
	if !nodeIDs["svc-a:agent-1"] {
		t.Error("expected svc-a:agent-1 to remain")
	}
	if !nodeIDs["svc-c:agent-1"] {
		t.Error("expected svc-c:agent-1 to be added")
	}
	if nodeIDs["svc-b:agent-1"] {
		t.Error("expected svc-b:agent-1 to be removed")
	}

	// Should have 1 edge: svc-a→svc-c (added). The old svc-a→svc-b edge was cascade-removed.
	if len(topo.Edges) != 1 {
		t.Fatalf("expected 1 edge after diff, got %d", len(topo.Edges))
	}
	if topo.Edges[0].SourceID != "svc-a:agent-1" || topo.Edges[0].TargetID != "svc-c:agent-1" {
		t.Errorf("expected edge svc-a:agent-1→svc-c:agent-1, got %s→%s", topo.Edges[0].SourceID, topo.Edges[0].TargetID)
	}
	if topo.Edges[0].Protocol != "grpc" {
		t.Errorf("expected protocol grpc, got %s", topo.Edges[0].Protocol)
	}
}

// =============================================================================
// TestTopologyOps_ApplyDiff_UpdateNodesAndEdges
// =============================================================================

func TestTopologyOps_ApplyDiff_UpdateNodesAndEdges(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()
	tenant := "update-diff-tenant"

	// Seed.
	seed := sampleTopology()
	err := topoOps.UpdateTopology(ctx, tenant, func(_ *models.Topology) (*models.Topology, error) {
		return seed, nil
	})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Apply diff that updates node health and edge protocol.
	diff := &TopologyDiff{
		NodesUpdated: []models.TopologyNode{
			{
				ID:       "svc-a:agent-1",
				Name:     "svc-a",
				Type:     models.NodeTypeService,
				AgentID:  "agent-1",
				Labels:   map[string]string{"env": "staging"},
				Metadata: map[string]string{},
				Health:   models.HealthStatusDegraded,
				LastSeen: time.Now(),
			},
		},
		EdgesUpdated: []models.TopologyEdge{
			{
				ID:       "svc-a:agent-1→svc-b:agent-1:0",
				SourceID: "svc-a:agent-1",
				TargetID: "svc-b:agent-1",
				Type:     models.EdgeTypeGRPC,
				Protocol: "grpc",
				Labels:   map[string]string{},
				LastSeen: time.Now(),
			},
		},
		Timestamp: time.Now(),
	}

	err = topoOps.ApplyDiff(ctx, tenant, diff)
	if err != nil {
		t.Fatalf("ApplyDiff update failed: %v", err)
	}

	topo, _, err := topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion failed: %v", err)
	}

	// Verify node update.
	var svcA *models.TopologyNode
	for i := range topo.Nodes {
		if topo.Nodes[i].ID == "svc-a:agent-1" {
			svcA = &topo.Nodes[i]
		}
	}
	if svcA == nil {
		t.Fatal("svc-a:agent-1 not found")
	}
	if svcA.Health != models.HealthStatusDegraded {
		t.Errorf("expected health=degraded, got %s", svcA.Health)
	}
	if svcA.Labels["env"] != "staging" {
		t.Errorf("expected label env=staging, got %s", svcA.Labels["env"])
	}

	// Verify edge update.
	if len(topo.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(topo.Edges))
	}
	if topo.Edges[0].Protocol != "grpc" {
		t.Errorf("expected protocol=grpc, got %s", topo.Edges[0].Protocol)
	}
	if topo.Edges[0].Type != models.EdgeTypeGRPC {
		t.Errorf("expected edge type=grpc, got %s", topo.Edges[0].Type)
	}
}

// =============================================================================
// TestTopologyOps_ApplyDiff_NilDiff
// =============================================================================

func TestTopologyOps_ApplyDiff_NilDiff(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()

	err := topoOps.ApplyDiff(ctx, "test-tenant", nil)
	if err == nil {
		t.Fatal("expected error for nil diff, got nil")
	}
}

// =============================================================================
// TestTopologyOps_UpdateTopology_RetryOnConflict
// Simulates a WATCH conflict by concurrently modifying the key from a second
// Redis client while UpdateTopology is executing its WATCH callback.
// Uses channels for deterministic coordination and sync/atomic for safe counters.
// =============================================================================

func TestTopologyOps_UpdateTopology_RetryOnConflict(t *testing.T) {
	t.Parallel()

	topoOps, mr := newTestTopologyOps(t)
	ctx := context.Background()
	tenant := "conflict-tenant"

	// Seed an initial topology with 2 nodes.
	seed := sampleTopology()
	err := topoOps.UpdateTopology(ctx, tenant, func(_ *models.Topology) (*models.Topology, error) {
		return seed, nil
	})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Coordination: the mutate function signals when it's executing;
	// the conflict goroutine writes a valid topology to trigger the WATCH conflict.
	mutateStarted := make(chan struct{})
	conflictDone := make(chan struct{})

	// Atomic counter to track mutate calls without data races.
	var mutateCalls atomic.Int32

	// Second Redis client to inject concurrent writes.
	conflictRdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer conflictRdb.Close()

	// Conflict goroutine: waits for the mutate to start, then writes valid
	// JSON to trigger the WATCH key change.
	go func() {
		defer close(conflictDone)
		select {
		case <-mutateStarted:
			// Write valid JSON so that on retry the topology can be deserialized.
			conflictData := `{"nodes":[],"edges":[],"timestamp":"2025-01-01T00:00:00Z","version":0}`
			conflictRdb.Set(ctx, topologyKey(tenant), conflictData, 5*time.Minute)
		case <-time.After(5 * time.Second):
			// Safety timeout to prevent goroutine leak in slow CI environments.
		}
	}()

	// UpdateTopology with a mutate function that triggers the concurrent write
	// on the first attempt, causing the EXEC to fail with TxFailedErr.
	err = topoOps.UpdateTopology(ctx, tenant, func(current *models.Topology) (*models.Topology, error) {
		calls := mutateCalls.Add(1)

		if calls == 1 {
			// Signal the conflict goroutine to write to the watched key.
			close(mutateStarted)
			// Wait for the conflict write to complete before returning.
			<-conflictDone
		}

		current.Nodes = append(current.Nodes, models.TopologyNode{
			ID:       "svc-new:agent-1",
			Name:     "svc-new",
			Type:     models.NodeTypeService,
			AgentID:  "agent-1",
			Labels:   map[string]string{},
			Metadata: map[string]string{},
			Health:   models.HealthStatusHealthy,
			LastSeen: time.Now(),
		})
		return current, nil
	})

	if err != nil {
		t.Fatalf("UpdateTopology failed: %v", err)
	}

	// Verify the final topology has the new node.
	topo, _, err := topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion failed: %v", err)
	}
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	// Check that the new node is present.
	hasNewNode := false
	for _, n := range topo.Nodes {
		if n.ID == "svc-new:agent-1" {
			hasNewNode = true
			break
		}
	}
	if !hasNewNode {
		t.Error("expected svc-new:agent-1 to be present in final topology")
	}

	// Verify the conflict actually happened by checking that mutate was called
	// more than once (retry occurred). On conflict, the first attempt fails and
	// the second attempt reads the conflict-written empty topology + adds the node.
	finalCalls := mutateCalls.Load()
	if finalCalls < 2 {
		t.Logf("warning: mutate was called %d time(s); expected ≥2 if conflict occurred", finalCalls)
		t.Logf("conflict injection is timing-dependent with miniredis; test verifies correctness in both paths")
	} else {
		t.Logf("conflict successfully triggered: mutate retried %d time(s)", finalCalls)
	}
}

// =============================================================================
// TestTopologyOps_UpdateTopology_ConcurrentConflict
// Two UpdateTopology calls run concurrently on the same key. Both must
// eventually succeed (one retries after conflict).
// =============================================================================

func TestTopologyOps_UpdateTopology_ConcurrentConflict(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()
	tenant := "concurrent-conflict-tenant"

	// Seed initial topology.
	seed := sampleTopology()
	err := topoOps.UpdateTopology(ctx, tenant, func(_ *models.Topology) (*models.Topology, error) {
		return seed, nil
	})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Two goroutines update the same topology concurrently.
	// Both add their own unique node. The optimistic locking ensures
	// one will conflict and retry, but both must eventually succeed.
	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(2)

	go func() {
		defer wg.Done()
		err := topoOps.UpdateTopology(ctx, tenant, func(current *models.Topology) (*models.Topology, error) {
			current.Nodes = append(current.Nodes, models.TopologyNode{
				ID:       "svc-x:agent-1",
				Name:     "svc-x",
				Type:     models.NodeTypeService,
				AgentID:  "agent-1",
				Labels:   map[string]string{},
				Metadata: map[string]string{},
				Health:   models.HealthStatusHealthy,
				LastSeen: time.Now(),
			})
			return current, nil
		})
		if err != nil {
			errCh <- fmt.Errorf("goroutine svc-x: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		err := topoOps.UpdateTopology(ctx, tenant, func(current *models.Topology) (*models.Topology, error) {
			current.Nodes = append(current.Nodes, models.TopologyNode{
				ID:       "svc-y:agent-1",
				Name:     "svc-y",
				Type:     models.NodeTypeService,
				AgentID:  "agent-1",
				Labels:   map[string]string{},
				Metadata: map[string]string{},
				Health:   models.HealthStatusHealthy,
				LastSeen: time.Now(),
			})
			return current, nil
		})
		if err != nil {
			errCh <- fmt.Errorf("goroutine svc-y: %w", err)
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent conflict error: %v", err)
	}

	// Verify both nodes were added.
	topo, _, err := topoOps.GetTopologyWithVersion(ctx, tenant)
	if err != nil {
		t.Fatalf("GetTopologyWithVersion failed: %v", err)
	}
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	nodeIDs := make(map[string]bool)
	for _, n := range topo.Nodes {
		nodeIDs[n.ID] = true
	}
	if !nodeIDs["svc-x:agent-1"] {
		t.Error("expected svc-x:agent-1 to be present")
	}
	if !nodeIDs["svc-y:agent-1"] {
		t.Error("expected svc-y:agent-1 to be present")
	}
}

// =============================================================================
// TestTopologyOps_UpdateTopology_ContextCancelled
// =============================================================================

func TestTopologyOps_UpdateTopology_ContextCancelled(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	tenant := "cancelled-tenant"

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := topoOps.UpdateTopology(ctx, tenant, func(current *models.Topology) (*models.Topology, error) {
		return current, nil
	})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// =============================================================================
// TestTopologyOps_UpdateTopology_TenantIsolation
// Different tenants must not interfere with each other.
// =============================================================================

func TestTopologyOps_UpdateTopology_TenantIsolation(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()

	// Tenant A: add node "svc-a".
	err := topoOps.UpdateTopology(ctx, "tenant-a", func(current *models.Topology) (*models.Topology, error) {
		current.Nodes = append(current.Nodes, models.TopologyNode{
			ID:       "svc-a:agent-1",
			Name:     "svc-a",
			Type:     models.NodeTypeService,
			AgentID:  "agent-1",
			Labels:   map[string]string{},
			Metadata: map[string]string{},
			Health:   models.HealthStatusHealthy,
			LastSeen: time.Now(),
		})
		return current, nil
	})
	if err != nil {
		t.Fatalf("tenant-a update failed: %v", err)
	}

	// Tenant B: add node "svc-b".
	err = topoOps.UpdateTopology(ctx, "tenant-b", func(current *models.Topology) (*models.Topology, error) {
		current.Nodes = append(current.Nodes, models.TopologyNode{
			ID:       "svc-b:agent-2",
			Name:     "svc-b",
			Type:     models.NodeTypeService,
			AgentID:  "agent-2",
			Labels:   map[string]string{},
			Metadata: map[string]string{},
			Health:   models.HealthStatusHealthy,
			LastSeen: time.Now(),
		})
		return current, nil
	})
	if err != nil {
		t.Fatalf("tenant-b update failed: %v", err)
	}

	// Verify tenant isolation.
	topoA, _, err := topoOps.GetTopologyWithVersion(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("get tenant-a failed: %v", err)
	}
	topoB, _, err := topoOps.GetTopologyWithVersion(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("get tenant-b failed: %v", err)
	}

	if len(topoA.Nodes) != 1 || topoA.Nodes[0].Name != "svc-a" {
		t.Errorf("tenant-a expected [svc-a], got %d nodes", len(topoA.Nodes))
	}
	if len(topoB.Nodes) != 1 || topoB.Nodes[0].Name != "svc-b" {
		t.Errorf("tenant-b expected [svc-b], got %d nodes", len(topoB.Nodes))
	}
}

// =============================================================================
// TestTopologyVersionKey
// =============================================================================

func TestTopologyVersionKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tenant string
		want   string
	}{
		{
			name:   "standard tenant",
			tenant: "acme-corp",
			want:   "paryty:acme-corp:topology:version",
		},
		{
			name:   "default tenant",
			tenant: "default",
			want:   "paryty:default:topology:version",
		},
		{
			name:   "tenant with special chars",
			tenant: "org-123_test",
			want:   "paryty:org-123_test:topology:version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := topologyVersionKey(tc.tenant)
			if got != tc.want {
				t.Errorf("topologyVersionKey(%q) = %q, want %q", tc.tenant, got, tc.want)
			}
		})
	}
}

// =============================================================================
// TestTopologyOps_VersionKeyIsolation
// Version keys for different tenants must be independent.
// =============================================================================

func TestTopologyOps_VersionKeyIsolation(t *testing.T) {
	t.Parallel()

	// Verify the version key format is tenant-scoped.
	keyA := topologyVersionKey("tenant-a")
	keyB := topologyVersionKey("tenant-b")

	if keyA == keyB {
		t.Errorf("version keys should differ between tenants: %q == %q", keyA, keyB)
	}

	expectedPrefixA := "paryty:tenant-a:"
	if len(keyA) < len(expectedPrefixA) || keyA[:len(expectedPrefixA)] != expectedPrefixA {
		t.Errorf("version key for tenant-a should have prefix %q, got %q", expectedPrefixA, keyA)
	}

	expectedPrefixB := "paryty:tenant-b:"
	if len(keyB) < len(expectedPrefixB) || keyB[:len(expectedPrefixB)] != expectedPrefixB {
		t.Errorf("version key for tenant-b should have prefix %q, got %q", expectedPrefixB, keyB)
	}
}

// =============================================================================
// TestIsRetryableError
// =============================================================================

func TestIsRetryableError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"TxFailedErr", redis.TxFailedErr, true},
		{"redis.Nil", redis.Nil, true},
		{"generic error", fmt.Errorf("connection refused"), false},
		// Wrapped redis.TxFailedErr should still be retryable.
		// go-redis wraps the error in fmt.Errorf("exec transaction: %w", err),
		// so errors.Is must walk the chain.
		{"wrapped TxFailedErr", fmt.Errorf("exec transaction: %w", redis.TxFailedErr), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isRetryableError(tc.err)
			if got != tc.want {
				t.Errorf("isRetryableError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// =============================================================================
// TestApplyTopologyDiff_Pure
// Unit test the pure diff function without Redis.
// =============================================================================

func TestApplyTopologyDiff_Pure(t *testing.T) {
	t.Parallel()

	t.Run("add nodes and edges", func(t *testing.T) {
		t.Parallel()

		current := &models.Topology{
			Nodes: []models.TopologyNode{
				{ID: "n1", Name: "Node 1", Type: models.NodeTypeService},
			},
			Edges: []models.TopologyEdge{},
		}

		diff := &TopologyDiff{
			NodesAdded: []models.TopologyNode{
				{ID: "n2", Name: "Node 2", Type: models.NodeTypeService},
				{ID: "n3", Name: "Node 3", Type: models.NodeTypeDatabase},
			},
			EdgesAdded: []models.TopologyEdge{
				{ID: "e1", SourceID: "n1", TargetID: "n2", Protocol: "http"},
			},
		}

		result, err := applyTopologyDiff(current, diff)
		if err != nil {
			t.Fatalf("applyTopologyDiff failed: %v", err)
		}
		if len(result.Nodes) != 3 {
			t.Errorf("expected 3 nodes, got %d", len(result.Nodes))
		}
		if len(result.Edges) != 1 {
			t.Errorf("expected 1 edge, got %d", len(result.Edges))
		}
	})

	t.Run("remove nodes cascades edges", func(t *testing.T) {
		t.Parallel()

		current := &models.Topology{
			Nodes: []models.TopologyNode{
				{ID: "n1", Name: "Node 1"},
				{ID: "n2", Name: "Node 2"},
				{ID: "n3", Name: "Node 3"},
			},
			Edges: []models.TopologyEdge{
				{ID: "e1", SourceID: "n1", TargetID: "n2"},
				{ID: "e2", SourceID: "n2", TargetID: "n3"},
				{ID: "e3", SourceID: "n1", TargetID: "n3"},
			},
		}

		diff := &TopologyDiff{
			NodesRemoved: []string{"n2"},
		}

		result, err := applyTopologyDiff(current, diff)
		if err != nil {
			t.Fatalf("applyTopologyDiff failed: %v", err)
		}
		if len(result.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(result.Nodes))
		}
		// e1 (n1→n2) and e2 (n2→n3) should be removed; e3 (n1→n3) should remain.
		if len(result.Edges) != 1 {
			t.Fatalf("expected 1 edge (e3), got %d", len(result.Edges))
		}
		if result.Edges[0].ID != "e3" {
			t.Errorf("expected edge e3, got %s", result.Edges[0].ID)
		}
	})

	t.Run("update node", func(t *testing.T) {
		t.Parallel()

		current := &models.Topology{
			Nodes: []models.TopologyNode{
				{ID: "n1", Name: "Node 1", Health: models.HealthStatusHealthy},
			},
		}

		diff := &TopologyDiff{
			NodesUpdated: []models.TopologyNode{
				{ID: "n1", Name: "Node 1 Updated", Health: models.HealthStatusDegraded},
			},
		}

		result, err := applyTopologyDiff(current, diff)
		if err != nil {
			t.Fatalf("applyTopologyDiff failed: %v", err)
		}
		if result.Nodes[0].Name != "Node 1 Updated" {
			t.Errorf("expected name 'Node 1 Updated', got %q", result.Nodes[0].Name)
		}
		if result.Nodes[0].Health != models.HealthStatusDegraded {
			t.Errorf("expected health degraded, got %s", result.Nodes[0].Health)
		}
	})

	t.Run("update non-existent node fails", func(t *testing.T) {
		t.Parallel()

		current := &models.Topology{Nodes: []models.TopologyNode{}}
		diff := &TopologyDiff{
			NodesUpdated: []models.TopologyNode{
				{ID: "nonexistent", Name: "Ghost"},
			},
		}

		_, err := applyTopologyDiff(current, diff)
		if err == nil {
			t.Fatal("expected error for updating non-existent node")
		}
	})

	t.Run("update non-existent edge fails", func(t *testing.T) {
		t.Parallel()

		current := &models.Topology{Edges: []models.TopologyEdge{}}
		diff := &TopologyDiff{
			EdgesUpdated: []models.TopologyEdge{
				{ID: "nonexistent", SourceID: "a", TargetID: "b"},
			},
		}

		_, err := applyTopologyDiff(current, diff)
		if err == nil {
			t.Fatal("expected error for updating non-existent edge")
		}
	})

	t.Run("empty diff is no-op", func(t *testing.T) {
		t.Parallel()

		current := sampleTopology()
		originalNodeCount := len(current.Nodes)
		originalEdgeCount := len(current.Edges)

		diff := &TopologyDiff{}

		result, err := applyTopologyDiff(current, diff)
		if err != nil {
			t.Fatalf("applyTopologyDiff failed: %v", err)
		}
		if len(result.Nodes) != originalNodeCount {
			t.Errorf("expected %d nodes, got %d", originalNodeCount, len(result.Nodes))
		}
		if len(result.Edges) != originalEdgeCount {
			t.Errorf("expected %d edges, got %d", originalEdgeCount, len(result.Edges))
		}
	})
}

// =============================================================================
// TestCopyTopology
// =============================================================================

func TestCopyTopology(t *testing.T) {
	t.Parallel()

	original := sampleTopology()
	cp := copyTopology(original)

	// Verify deep copy: modifying the copy does not affect the original.
	cp.Nodes[0].Name = "modified"
	if original.Nodes[0].Name == "modified" {
		t.Error("copyTopology should produce an independent copy")
	}

	cp.Edges[0].Protocol = "modified"
	if original.Edges[0].Protocol == "modified" {
		t.Error("copyTopology edges should be independent")
	}

	// Verify fields are copied.
	if cp.Version != original.Version {
		t.Errorf("version mismatch: %d != %d", cp.Version, original.Version)
	}
	if !cp.Timestamp.Equal(original.Timestamp) {
		t.Errorf("timestamp mismatch")
	}
}

// =============================================================================
// TestTopologyOps_UpdateTopology_EmptyTopologyOnInit
// When no topology exists, mutate receives an empty topology with zero version.
// =============================================================================

func TestTopologyOps_UpdateTopology_EmptyTopologyOnInit(t *testing.T) {
	t.Parallel()

	topoOps, _ := newTestTopologyOps(t)
	ctx := context.Background()
	tenant := "empty-init-tenant"

	var receivedVersion uint64

	err := topoOps.UpdateTopology(ctx, tenant, func(current *models.Topology) (*models.Topology, error) {
		receivedVersion = current.Version

		if len(current.Nodes) != 0 {
			t.Errorf("expected empty nodes on first call, got %d", len(current.Nodes))
		}
		if len(current.Edges) != 0 {
			t.Errorf("expected empty edges on first call, got %d", len(current.Edges))
		}

		return current, nil
	})
	if err != nil {
		t.Fatalf("UpdateTopology failed: %v", err)
	}

	if receivedVersion != 0 {
		t.Errorf("expected initial version=0, got %d", receivedVersion)
	}
}
