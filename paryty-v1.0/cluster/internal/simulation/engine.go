package simulation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DefaultSimulationEngine is the production implementation of SimulationEngine.
// It manages scenario lifecycle with in-memory state and delegates execution
// to the WhatIfRunner.
//
// V2.0 Migration: Replaces the Python SimulationService gRPC server. The
// engine runs entirely in-process, eliminating the gRPC round-trip overhead
// that the Python version required for every scenario operation.
type DefaultSimulationEngine struct {
	scenarios map[string]*scenarioState
	mu        sync.RWMutex
	runner    *WhatIfRunner
	logger    *zap.Logger
}

// scenarioState tracks the lifecycle state of a single scenario.
type scenarioState struct {
	scenario WhatIfScenario
	result   *SimulationResult
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewDefaultSimulationEngine creates a new simulation engine.
func NewDefaultSimulationEngine(store StorageReader, logger *zap.Logger) *DefaultSimulationEngine {
	return &DefaultSimulationEngine{
		scenarios: make(map[string]*scenarioState),
		runner:    NewWhatIfRunner(store, logger),
		logger:    logger,
	}
}

// RunScenario starts executing a scenario and returns a channel that receives
// the final result. If the scenario has no ID, one is generated automatically.
func (e *DefaultSimulationEngine) RunScenario(ctx context.Context, scenario WhatIfScenario) (<-chan *SimulationResult, error) {
	if scenario.ID == "" {
		scenario.ID = uuid.New().String()
	}
	if scenario.CreatedAt.IsZero() {
		scenario.CreatedAt = time.Now()
	}
	if scenario.TenantID == "" {
		scenario.TenantID = "default"
	}

	runCtx, cancel := context.WithCancel(ctx)
	state := &scenarioState{
		scenario: scenario,
		result: &SimulationResult{
			ScenarioID: scenario.ID,
			Status:     ScenarioStatusPending,
			StartTime:  time.Now(),
			Metrics:    make(map[string][]MetricPoint),
		},
		cancel: cancel,
		done:   make(chan struct{}),
	}

	e.mu.Lock()
	if _, exists := e.scenarios[scenario.ID]; exists {
		e.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("scenario %s already exists", scenario.ID)
	}
	e.scenarios[scenario.ID] = state
	e.mu.Unlock()

	resultCh := make(chan *SimulationResult, 1)
	go e.executeScenario(runCtx, state, resultCh)

	return resultCh, nil
}

// executeScenario runs the what-if analysis and delivers the result.
func (e *DefaultSimulationEngine) executeScenario(ctx context.Context, state *scenarioState, resultCh chan<- *SimulationResult) {
	defer close(state.done)
	defer func() {
		state.result.EndTime = time.Now()
		resultCh <- state.result
	}()

	state.result.Status = ScenarioStatusRunning
	e.logger.Info("Scenario started",
		zap.String("scenario_id", state.scenario.ID),
		zap.String("name", state.scenario.Name),
		zap.Int("changes", len(state.scenario.Changes)),
	)

	result, err := e.runner.RunScenario(ctx, state.scenario)
	if err != nil {
		state.result.Status = ScenarioStatusFailed
		state.result.Error = err.Error()
		e.logger.Error("Scenario failed",
			zap.String("scenario_id", state.scenario.ID),
			zap.Error(err),
		)
		return
	}

	if ctx.Err() != nil {
		state.result.Status = ScenarioStatusCanceled
		e.logger.Info("Scenario canceled", zap.String("scenario_id", state.scenario.ID))
		return
	}

	state.result = result
	state.result.EndTime = time.Now()
	e.logger.Info("Scenario completed",
		zap.String("scenario_id", state.scenario.ID),
		zap.String("status", string(state.result.Status)),
	)
}

// ListScenarios returns all known scenarios for a tenant.
func (e *DefaultSimulationEngine) ListScenarios(_ context.Context, tenantID string) ([]WhatIfScenario, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var results []WhatIfScenario
	for _, state := range e.scenarios {
		if state.scenario.TenantID == tenantID {
			results = append(results, state.scenario)
		}
	}
	return results, nil
}

// GetScenarioStatus returns the current status and results for a scenario.
func (e *DefaultSimulationEngine) GetScenarioStatus(_ context.Context, scenarioID string) (*SimulationResult, error) {
	e.mu.RLock()
	state, ok := e.scenarios[scenarioID]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("scenario %s not found", scenarioID)
	}
	return state.result, nil
}

// CancelScenario stops a running scenario. Returns an error if the scenario
// does not exist or is not in a cancellable state.
func (e *DefaultSimulationEngine) CancelScenario(_ context.Context, scenarioID string) error {
	e.mu.RLock()
	state, ok := e.scenarios[scenarioID]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("scenario %s not found", scenarioID)
	}

	if state.result.Status != ScenarioStatusPending && state.result.Status != ScenarioStatusRunning {
		return fmt.Errorf("scenario %s is in state %s, cannot cancel", scenarioID, state.result.Status)
	}

	state.cancel()

	// Wait for the scenario goroutine to finish.
	select {
	case <-state.done:
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for scenario %s to stop", scenarioID)
	}

	return nil
}
