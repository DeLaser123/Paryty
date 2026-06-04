package processing

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// TestGraph_AddNode
// =============================================================================

func TestGraph_AddNode(t *testing.T) {
	t.Parallel()

	g := NewDependencyGraph(nopLogger())
	now := time.Now()

	nodes := []*GraphNode{
		{ID: "svc-a", Name: "Service A", Type: NodeTypeService, AgentID: "agent-1", FirstSeen: now, LastSeen: now, HealthStatus: "healthy"},
		{ID: "svc-b", Name: "Service B", Type: NodeTypeService, AgentID: "agent-2", FirstSeen: now, LastSeen: now, HealthStatus: "healthy"},
		{ID: "proc-c", Name: "Process C", Type: NodeTypeProcess, AgentID: "agent-3", FirstSeen: now, LastSeen: now, HealthStatus: "degraded"},
	}

	for i, node := range nodes {
		isNew := g.AddOrUpdateNode(node)
		if !isNew {
			t.Fatalf("node %d: expected new node, got update", i)
		}
	}

	if g.NodeCount() != 3 {
		t.Errorf("expected 3 nodes, got %d", g.NodeCount())
	}
	if g.EdgeCount() != 0 {
		t.Errorf("expected 0 edges, got %d", g.EdgeCount())
	}

	changes := g.GetChanges()
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}

	for _, c := range changes {
		if c.Type != "node_added" {
			t.Errorf("expected change type 'node_added', got %q", c.Type)
		}
	}
}

// =============================================================================
// TestGraph_AddEdge
// =============================================================================

func TestGraph_AddEdge(t *testing.T) {
	t.Parallel()

	g := NewDependencyGraph(nopLogger())
	now := time.Now()

	g.AddOrUpdateNode(&GraphNode{ID: "svc-a", Name: "Service A", Type: NodeTypeService, AgentID: "agent-1", FirstSeen: now, LastSeen: now})
	g.AddOrUpdateNode(&GraphNode{ID: "svc-b", Name: "Service B", Type: NodeTypeService, AgentID: "agent-1", FirstSeen: now, LastSeen: now})

	isNew := g.AddOrUpdateEdge(&GraphEdge{
		SourceID: "svc-a", TargetID: "svc-b", Protocol: "http",
		Port: 8080, Frequency: 100, LatencyMs: 25.5, ErrorRate: 0.01,
		FirstSeen: now, LastSeen: now,
	})
	if !isNew {
		t.Error("expected new edge, got update")
	}

	if g.NodeCount() != 2 {
		t.Errorf("expected 2 nodes, got %d", g.NodeCount())
	}
	if g.EdgeCount() != 1 {
		t.Errorf("expected 1 edge, got %d", g.EdgeCount())
	}

	// GetNeighbors for svc-a should return svc-b (outgoing).
	neighbors, edges := g.GetNeighbors("svc-a")
	if len(neighbors) != 1 {
		t.Fatalf("expected 1 neighbor for svc-a, got %d", len(neighbors))
	}
	if neighbors[0].ID != "svc-b" {
		t.Errorf("expected neighbor svc-b, got %s", neighbors[0].ID)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge for svc-a, got %d", len(edges))
	}
	if edges[0].SourceID != "svc-a" || edges[0].TargetID != "svc-b" {
		t.Errorf("edge direction mismatch: %s -> %s", edges[0].SourceID, edges[0].TargetID)
	}

	// GetNeighbors for svc-b should return svc-a (incoming).
	neighbors, edges = g.GetNeighbors("svc-b")
	if len(neighbors) != 1 {
		t.Fatalf("expected 1 neighbor for svc-b, got %d", len(neighbors))
	}
	if neighbors[0].ID != "svc-a" {
		t.Errorf("expected neighbor svc-a, got %s", neighbors[0].ID)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge for svc-b, got %d", len(edges))
	}
}

// =============================================================================
// TestGraph_UpdateExistingEdge
// =============================================================================

func TestGraph_UpdateExistingEdge(t *testing.T) {
	t.Parallel()

	g := NewDependencyGraph(nopLogger())
	now := time.Now()

	g.AddOrUpdateNode(&GraphNode{ID: "svc-a", Name: "A", Type: NodeTypeService, AgentID: "agent-1", FirstSeen: now, LastSeen: now})
	g.AddOrUpdateNode(&GraphNode{ID: "svc-b", Name: "B", Type: NodeTypeService, AgentID: "agent-1", FirstSeen: now, LastSeen: now})

	// Add edge with frequency=1.
	isNew := g.AddOrUpdateEdge(&GraphEdge{
		SourceID: "svc-a", TargetID: "svc-b", Frequency: 1,
		FirstSeen: now, LastSeen: now,
	})
	if !isNew {
		t.Error("expected new edge")
	}

	// Update same edge with frequency=2.
	isNew = g.AddOrUpdateEdge(&GraphEdge{
		SourceID: "svc-a", TargetID: "svc-b", Frequency: 2,
		FirstSeen: now, LastSeen: now,
	})
	if isNew {
		t.Error("expected update, got new edge")
	}

	if g.EdgeCount() != 1 {
		t.Fatalf("expected 1 edge after update, got %d", g.EdgeCount())
	}

	edges := g.Edges()
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].Frequency != 2 {
		t.Errorf("expected frequency=2, got %d", edges[0].Frequency)
	}

	// Verify change records: 2 nodes + 1 edge_added + 1 edge_updated = 4.
	changes := g.GetChanges()
	if len(changes) != 4 {
		t.Fatalf("expected 4 changes, got %d", len(changes))
	}

	typeCounts := map[string]int{}
	for _, c := range changes {
		typeCounts[c.Type]++
	}
	if typeCounts["edge_added"] != 1 {
		t.Errorf("expected 1 edge_added, got %d", typeCounts["edge_added"])
	}
	if typeCounts["edge_updated"] != 1 {
		t.Errorf("expected 1 edge_updated, got %d", typeCounts["edge_updated"])
	}
}

// =============================================================================
// TestGraph_StaleNodeRemoval
// =============================================================================

func TestGraph_StaleNodeRemoval(t *testing.T) {
	t.Parallel()

	g := NewDependencyGraph(nopLogger())
	now := time.Now()

	// Node 1: seen just now (fresh).
	g.AddOrUpdateNode(&GraphNode{
		ID: "fresh-1", Name: "Fresh 1", Type: NodeTypeService,
		AgentID: "agent-1", FirstSeen: now, LastSeen: now,
	})
	// Node 2: seen 1 minute ago (fresh within 5m window).
	g.AddOrUpdateNode(&GraphNode{
		ID: "fresh-2", Name: "Fresh 2", Type: NodeTypeService,
		AgentID: "agent-2", FirstSeen: now, LastSeen: now.Add(-1 * time.Minute),
	})
	// Node 3: seen 10 minutes ago (stale).
	g.AddOrUpdateNode(&GraphNode{
		ID: "stale-1", Name: "Stale 1", Type: NodeTypeService,
		AgentID: "agent-3", FirstSeen: now, LastSeen: now.Add(-10 * time.Minute),
	})

	removed := g.RemoveStaleNodes(5 * time.Minute)
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(removed))
	}
	if removed[0] != "stale-1" {
		t.Errorf("expected stale-1 removed, got %s", removed[0])
	}

	if g.NodeCount() != 2 {
		t.Errorf("expected 2 nodes remaining, got %d", g.NodeCount())
	}

	remaining := g.Nodes()
	remainingIDs := make(map[string]bool)
	for _, n := range remaining {
		remainingIDs[n.ID] = true
	}
	if !remainingIDs["fresh-1"] {
		t.Error("expected fresh-1 to remain")
	}
	if !remainingIDs["fresh-2"] {
		t.Error("expected fresh-2 to remain")
	}
	if remainingIDs["stale-1"] {
		t.Error("expected stale-1 to be removed")
	}
}

// =============================================================================
// TestGraph_SnapshotRestore
// =============================================================================

func TestGraph_SnapshotRestore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockDF := newTestDragonfly()
	now := time.Now()

	g1 := NewDependencyGraph(nopLogger())

	// Build a graph with 10 nodes and 15 edges.
	for i := 1; i <= 10; i++ {
		g1.AddOrUpdateNode(&GraphNode{
			ID:        fmt.Sprintf("n%d", i),
			Name:      fmt.Sprintf("Node %d", i),
			Type:      NodeTypeService,
			AgentID:   fmt.Sprintf("agent-%d", i),
			FirstSeen: now,
			LastSeen:  now,
		})
	}

	edgeDefs := []struct{ src, tgt string }{
		{"n1", "n2"}, {"n2", "n3"}, {"n3", "n4"}, {"n4", "n5"},
		{"n5", "n6"}, {"n6", "n7"}, {"n7", "n8"}, {"n8", "n9"},
		{"n9", "n10"},
		{"n1", "n3"}, {"n1", "n4"}, {"n1", "n5"},
		{"n2", "n4"}, {"n2", "n5"}, {"n2", "n6"},
	}

	for _, e := range edgeDefs {
		g1.AddOrUpdateEdge(&GraphEdge{
			SourceID: e.src, TargetID: e.tgt, Protocol: "grpc",
			Frequency: 100, LatencyMs: 10.0, FirstSeen: now, LastSeen: now,
		})
	}

	if g1.NodeCount() != 10 {
		t.Fatalf("expected 10 nodes, got %d", g1.NodeCount())
	}
	if g1.EdgeCount() != 15 {
		t.Fatalf("expected 15 edges, got %d", g1.EdgeCount())
	}

	// Snapshot to Dragonfly.
	if err := g1.Snapshot(ctx, mockDF); err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	// Create a new graph and restore.
	g2 := NewDependencyGraph(nopLogger())
	if err := g2.Restore(ctx, mockDF); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	if g2.NodeCount() != 10 {
		t.Errorf("restored node count: expected 10, got %d", g2.NodeCount())
	}
	if g2.EdgeCount() != 15 {
		t.Errorf("restored edge count: expected 15, got %d", g2.EdgeCount())
	}

	// Verify restored nodes match original.
	originalNodes := g1.Nodes()
	restoredNodes := g2.Nodes()

	sortByID := func(nodes []*GraphNode) {
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	}
	sortByID(originalNodes)
	sortByID(restoredNodes)

	for i, orig := range originalNodes {
		rest := restoredNodes[i]
		if orig.ID != rest.ID {
			t.Errorf("node %d: ID mismatch: %s vs %s", i, orig.ID, rest.ID)
		}
		if orig.Name != rest.Name {
			t.Errorf("node %d: Name mismatch: %s vs %s", i, orig.Name, rest.Name)
		}
		if orig.Type != rest.Type {
			t.Errorf("node %d: Type mismatch: %s vs %s", i, orig.Type, rest.Type)
		}
	}

	// Verify restored edges match original.
	originalEdges := g1.Edges()
	restoredEdges := g2.Edges()

	sortBySrcTgt := func(edges []*GraphEdge) {
		sort.Slice(edges, func(i, j int) bool {
			if edges[i].SourceID != edges[j].SourceID {
				return edges[i].SourceID < edges[j].SourceID
			}
			return edges[i].TargetID < edges[j].TargetID
		})
	}
	sortBySrcTgt(originalEdges)
	sortBySrcTgt(restoredEdges)

	for i, orig := range originalEdges {
		rest := restoredEdges[i]
		if orig.SourceID != rest.SourceID || orig.TargetID != rest.TargetID {
			t.Errorf("edge %d: direction mismatch: %s->%s vs %s->%s",
				i, orig.SourceID, orig.TargetID, rest.SourceID, rest.TargetID)
		}
		if orig.Frequency != rest.Frequency {
			t.Errorf("edge %d: frequency mismatch: %d vs %d",
				i, orig.Frequency, rest.Frequency)
		}
	}
}

// =============================================================================
// TestGraph_ShortestPath
// =============================================================================

func TestGraph_ShortestPath(t *testing.T) {
	t.Parallel()

	g := NewDependencyGraph(nopLogger())
	now := time.Now()

	// Build A→B→C→D chain.
	for _, id := range []string{"A", "B", "C", "D"} {
		g.AddOrUpdateNode(&GraphNode{
			ID: id, Name: id, Type: NodeTypeService,
			AgentID: "agent-1", FirstSeen: now, LastSeen: now,
		})
	}

	g.AddOrUpdateEdge(&GraphEdge{SourceID: "A", TargetID: "B", FirstSeen: now, LastSeen: now})
	g.AddOrUpdateEdge(&GraphEdge{SourceID: "B", TargetID: "C", FirstSeen: now, LastSeen: now})
	g.AddOrUpdateEdge(&GraphEdge{SourceID: "C", TargetID: "D", FirstSeen: now, LastSeen: now})

	tests := []struct {
		name     string
		source   string
		target   string
		expected []string
	}{
		{
			name:     "full chain A to D",
			source:   "A",
			target:   "D",
			expected: []string{"A", "B", "C", "D"},
		},
		{
			name:     "direct neighbor A to B",
			source:   "A",
			target:   "B",
			expected: []string{"A", "B"},
		},
		{
			name:     "single node path",
			source:   "A",
			target:   "A",
			expected: []string{"A"},
		},
		{
			name:     "reverse direction no path",
			source:   "D",
			target:   "A",
			expected: nil,
		},
		{
			name:     "non-existent source",
			source:   "Z",
			target:   "A",
			expected: nil,
		},
		{
			name:     "non-existent target",
			source:   "A",
			target:   "Z",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := g.GetPath(tt.source, tt.target)
			if tt.expected == nil {
				if len(path) != 0 {
					t.Errorf("expected empty path, got %v", path)
				}
				return
			}
			if len(path) != len(tt.expected) {
				t.Fatalf("path length mismatch: expected %v, got %v", tt.expected, path)
			}
			for i, id := range path {
				if id != tt.expected[i] {
					t.Errorf("path[%d]: expected %s, got %s", i, tt.expected[i], id)
				}
			}
		})
	}
}

// TestGraph_ShortestPathWithShortcut verifies that adding a shortcut edge
// produces a shorter BFS path. Separate from TestGraph_ShortestPath because
// parallel subtests in Go only execute after the parent function returns.
func TestGraph_ShortestPathWithShortcut(t *testing.T) {
	t.Parallel()

	g := NewDependencyGraph(nopLogger())
	now := time.Now()

	// Build A→B→C→D chain + A→D shortcut.
	for _, id := range []string{"A", "B", "C", "D"} {
		g.AddOrUpdateNode(&GraphNode{
			ID: id, Name: id, Type: NodeTypeService,
			AgentID: "agent-1", FirstSeen: now, LastSeen: now,
		})
	}

	g.AddOrUpdateEdge(&GraphEdge{SourceID: "A", TargetID: "B", FirstSeen: now, LastSeen: now})
	g.AddOrUpdateEdge(&GraphEdge{SourceID: "B", TargetID: "C", FirstSeen: now, LastSeen: now})
	g.AddOrUpdateEdge(&GraphEdge{SourceID: "C", TargetID: "D", FirstSeen: now, LastSeen: now})
	g.AddOrUpdateEdge(&GraphEdge{SourceID: "A", TargetID: "D", FirstSeen: now, LastSeen: now})

	path := g.GetPath("A", "D")
	expected := []string{"A", "D"}
	if len(path) != len(expected) {
		t.Fatalf("path length mismatch: expected %v, got %v", expected, path)
	}
	for i, id := range path {
		if id != expected[i] {
			t.Errorf("path[%d]: expected %s, got %s", i, expected[i], id)
		}
	}
}

// =============================================================================
// TestGraph_GetChanges
// =============================================================================

func TestGraph_GetChanges(t *testing.T) {
	t.Parallel()

	g := NewDependencyGraph(nopLogger())
	now := time.Now()

	// Add 5 nodes.
	for i := 0; i < 5; i++ {
		g.AddOrUpdateNode(&GraphNode{
			ID: fmt.Sprintf("n%d", i), Name: fmt.Sprintf("N%d", i),
			Type: NodeTypeService, AgentID: "agent-1",
			FirstSeen: now, LastSeen: now,
		})
	}

	changes := g.GetChanges()
	if len(changes) != 5 {
		t.Fatalf("expected 5 changes, got %d", len(changes))
	}
	for _, c := range changes {
		if c.Type != "node_added" {
			t.Errorf("expected 'node_added', got %q", c.Type)
		}
	}

	// Second call should return nil (already cleared).
	changes = g.GetChanges()
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes on second call, got %d", len(changes))
	}

	// Add 2 more nodes.
	for i := 5; i < 7; i++ {
		g.AddOrUpdateNode(&GraphNode{
			ID: fmt.Sprintf("n%d", i), Name: fmt.Sprintf("N%d", i),
			Type: NodeTypeService, AgentID: "agent-1",
			FirstSeen: now, LastSeen: now,
		})
	}

	changes = g.GetChanges()
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
}

// =============================================================================
// TestGraph_ConcurrentAccess
// =============================================================================

func TestGraph_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	g := NewDependencyGraph(nopLogger())
	now := time.Now()

	ctx := context.Background()

	// Pre-populate with initial nodes.
	for i := 0; i < 20; i++ {
		g.AddOrUpdateNode(&GraphNode{
			ID: fmt.Sprintf("n%d", i), Name: fmt.Sprintf("N%d", i),
			Type: NodeTypeService, AgentID: "agent-1",
			FirstSeen: now, LastSeen: now,
		})
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Writer: add nodes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 20; i < 120; i++ {
			g.AddOrUpdateNode(&GraphNode{
				ID: fmt.Sprintf("n%d", i), Name: fmt.Sprintf("N%d", i),
				Type: NodeTypeService, AgentID: "agent-1",
				FirstSeen: now, LastSeen: now,
			})
		}
	}()

	// Writer: add edges.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 19; i++ {
			g.AddOrUpdateEdge(&GraphEdge{
				SourceID: fmt.Sprintf("n%d", i), TargetID: fmt.Sprintf("n%d", i+1),
				FirstSeen: now, LastSeen: now,
			})
		}
	}()

	// Reader: get nodes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			nodes := g.Nodes()
			_ = len(nodes)
		}
	}()

	// Reader: get edges.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			edges := g.Edges()
			_ = len(edges)
		}
	}()

	// Reader: stats.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = g.Stats()
		}
	}()

	// Reader: path finding.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			path := g.GetPath("n0", "n19")
			_ = len(path)
		}
	}()

	// Reader: get neighbors.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			n, e := g.GetNeighbors("n0")
			_ = len(n)
			_ = len(e)
		}
	}()

	// Writer: remove stale nodes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			_ = g.RemoveStaleNodes(1 * time.Hour)
		}
	}()

	// Snapshot/Restore.
	wg.Add(1)
	go func() {
		defer wg.Done()
		mock := newTestDragonfly()
		for i := 0; i < 5; i++ {
			if err := g.Snapshot(ctx, mock); err != nil {
				errCh <- fmt.Errorf("snapshot: %w", err)
			}
		}
	}()

	// GetChanges.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = g.GetChanges()
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}

// =============================================================================
// TestGraph_RemoveNodeAlsoRemovesEdges
// =============================================================================

func TestGraph_RemoveNodeAlsoRemovesEdges(t *testing.T) {
	t.Parallel()

	g := NewDependencyGraph(nopLogger())
	now := time.Now()

	// Build: A→B, B→C, A→C
	for _, id := range []string{"A", "B", "C"} {
		g.AddOrUpdateNode(&GraphNode{
			ID: id, Name: id, Type: NodeTypeService,
			AgentID: "agent-1", FirstSeen: now, LastSeen: now,
		})
	}

	g.AddOrUpdateEdge(&GraphEdge{SourceID: "A", TargetID: "B", FirstSeen: now, LastSeen: now})
	g.AddOrUpdateEdge(&GraphEdge{SourceID: "B", TargetID: "C", FirstSeen: now, LastSeen: now})
	g.AddOrUpdateEdge(&GraphEdge{SourceID: "A", TargetID: "C", FirstSeen: now, LastSeen: now})

	if g.EdgeCount() != 3 {
		t.Fatalf("expected 3 edges, got %d", g.EdgeCount())
	}

	// Make B stale (seen 10 minutes ago).
	g.AddOrUpdateNode(&GraphNode{
		ID: "B", Name: "B", Type: NodeTypeService,
		AgentID: "agent-1", FirstSeen: now, LastSeen: now.Add(-10 * time.Minute),
	})

	removed := g.RemoveStaleNodes(5 * time.Minute)
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(removed))
	}
	if removed[0] != "B" {
		t.Errorf("expected B removed, got %s", removed[0])
	}

	// Only A→C should remain.
	if g.EdgeCount() != 1 {
		t.Fatalf("expected 1 edge remaining, got %d", g.EdgeCount())
	}

	edges := g.Edges()
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].SourceID != "A" || edges[0].TargetID != "C" {
		t.Errorf("expected remaining edge A→C, got %s→%s", edges[0].SourceID, edges[0].TargetID)
	}

	// Verify A has no neighbors from B.
	neighborsA, _ := g.GetNeighbors("A")
	for _, n := range neighborsA {
		if n.ID == "B" {
			t.Error("B should not appear as neighbor of A after removal")
		}
	}

	// Verify C has no incoming from B.
	neighborsC, _ := g.GetNeighbors("C")
	for _, n := range neighborsC {
		if n.ID == "B" {
			t.Error("B should not appear as neighbor of C after removal")
		}
	}

	// Verify node count.
	if g.NodeCount() != 2 {
		t.Errorf("expected 2 nodes, got %d", g.NodeCount())
	}
}

// =============================================================================
// TestGraph_Stats
// =============================================================================

func TestGraph_Stats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func(g *DependencyGraph)
		wantNodes     int
		wantEdges     int
		wantMaxFanOut int
		wantIsolated  int
	}{
		{
			name:          "empty graph",
			setup:         func(_ *DependencyGraph) {},
			wantNodes:     0,
			wantEdges:     0,
			wantMaxFanOut: 0,
			wantIsolated:  0,
		},
		{
			name: "single node no edges",
			setup: func(g *DependencyGraph) {
				now := time.Now()
				g.AddOrUpdateNode(&GraphNode{ID: "A", Name: "A", Type: NodeTypeService, AgentID: "a1", FirstSeen: now, LastSeen: now})
			},
			wantNodes:     1,
			wantEdges:     0,
			wantMaxFanOut: 0,
			wantIsolated:  1,
		},
		{
			name: "linear chain A→B→C",
			setup: func(g *DependencyGraph) {
				now := time.Now()
				for _, id := range []string{"A", "B", "C"} {
					g.AddOrUpdateNode(&GraphNode{ID: id, Name: id, Type: NodeTypeService, AgentID: "a1", FirstSeen: now, LastSeen: now})
				}
				g.AddOrUpdateEdge(&GraphEdge{SourceID: "A", TargetID: "B", FirstSeen: now, LastSeen: now})
				g.AddOrUpdateEdge(&GraphEdge{SourceID: "B", TargetID: "C", FirstSeen: now, LastSeen: now})
			},
			wantNodes:     3,
			wantEdges:     2,
			wantMaxFanOut: 1,
			wantIsolated:  0,
		},
		{
			name: "hub with spokes",
			setup: func(g *DependencyGraph) {
				now := time.Now()
				for _, id := range []string{"hub", "s1", "s2", "s3", "s4"} {
					g.AddOrUpdateNode(&GraphNode{ID: id, Name: id, Type: NodeTypeService, AgentID: "a1", FirstSeen: now, LastSeen: now})
				}
				for _, tgt := range []string{"s1", "s2", "s3", "s4"} {
					g.AddOrUpdateEdge(&GraphEdge{SourceID: "hub", TargetID: tgt, FirstSeen: now, LastSeen: now})
				}
			},
			wantNodes:     5,
			wantEdges:     4,
			wantMaxFanOut: 4,
			wantIsolated:  0,
		},
		{
			name: "mixed isolated and connected",
			setup: func(g *DependencyGraph) {
				now := time.Now()
				for _, id := range []string{"A", "B", "C", "D", "E"} {
					g.AddOrUpdateNode(&GraphNode{ID: id, Name: id, Type: NodeTypeService, AgentID: "a1", FirstSeen: now, LastSeen: now})
				}
				g.AddOrUpdateEdge(&GraphEdge{SourceID: "A", TargetID: "B", FirstSeen: now, LastSeen: now})
				g.AddOrUpdateEdge(&GraphEdge{SourceID: "B", TargetID: "C", FirstSeen: now, LastSeen: now})
				// D and E are isolated.
			},
			wantNodes:     5,
			wantEdges:     2,
			wantMaxFanOut: 1,
			wantIsolated:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewDependencyGraph(nopLogger())
			tt.setup(g)

			stats := g.Stats()

			if stats.NodeCount != tt.wantNodes {
				t.Errorf("NodeCount: expected %d, got %d", tt.wantNodes, stats.NodeCount)
			}
			if stats.EdgeCount != tt.wantEdges {
				t.Errorf("EdgeCount: expected %d, got %d", tt.wantEdges, stats.EdgeCount)
			}
			if stats.MaxFanOut != tt.wantMaxFanOut {
				t.Errorf("MaxFanOut: expected %d, got %d", tt.wantMaxFanOut, stats.MaxFanOut)
			}
			if stats.IsolatedNodes != tt.wantIsolated {
				t.Errorf("IsolatedNodes: expected %d, got %d", tt.wantIsolated, stats.IsolatedNodes)
			}

			// Verify individual accessors match Stats.
			if g.NodeCount() != stats.NodeCount {
				t.Errorf("NodeCount()=%d != Stats.NodeCount=%d", g.NodeCount(), stats.NodeCount)
			}
			if g.EdgeCount() != stats.EdgeCount {
				t.Errorf("EdgeCount()=%d != Stats.EdgeCount=%d", g.EdgeCount(), stats.EdgeCount)
			}
			if g.MaxFanOut() != stats.MaxFanOut {
				t.Errorf("MaxFanOut()=%d != Stats.MaxFanOut=%d", g.MaxFanOut(), stats.MaxFanOut)
			}
			if g.IsolatedNodeCount() != stats.IsolatedNodes {
				t.Errorf("IsolatedNodeCount()=%d != Stats.IsolatedNodes=%d",
					g.IsolatedNodeCount(), stats.IsolatedNodes)
			}
		})
	}

	// AvgFanOut test with specific expected value.
	t.Run("avg fan out", func(t *testing.T) {
		t.Parallel()
		g := NewDependencyGraph(nopLogger())
		now := time.Now()

		// 3 nodes, 2 edges: A→B, A→C. Fan-outs: A=2, B=0, C=0. Avg = 2/3.
		for _, id := range []string{"A", "B", "C"} {
			g.AddOrUpdateNode(&GraphNode{ID: id, Name: id, Type: NodeTypeService, AgentID: "a1", FirstSeen: now, LastSeen: now})
		}
		g.AddOrUpdateEdge(&GraphEdge{SourceID: "A", TargetID: "B", FirstSeen: now, LastSeen: now})
		g.AddOrUpdateEdge(&GraphEdge{SourceID: "A", TargetID: "C", FirstSeen: now, LastSeen: now})

		avg := g.AvgFanOut()
		expected := 2.0 / 3.0
		diff := avg - expected
		if diff < -0.001 || diff > 0.001 {
			t.Errorf("AvgFanOut: expected ~%.4f, got %.4f", expected, avg)
		}

		stats := g.Stats()
		if stats.AvgFanOut != avg {
			t.Errorf("Stats.AvgFanOut=%f != AvgFanOut()=%f", stats.AvgFanOut, avg)
		}
	})

	// Verify empty graph AvgFanOut returns 0.
	t.Run("empty avg fan out", func(t *testing.T) {
		t.Parallel()
		g := NewDependencyGraph(nopLogger())
		if g.AvgFanOut() != 0 {
			t.Errorf("expected AvgFanOut=0 for empty graph, got %f", g.AvgFanOut())
		}
	})
}
