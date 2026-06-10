package monitoring

import (
	"sync"
	"sync/atomic"
	"time"
)

// =============================================================================
// SLO Tracker
// =============================================================================

// SLODefinition defines a Service Level Objective.
type SLODefinition struct {
	// Name identifies this SLO (e.g., "query-api-availability").
	Name string
	// TargetPercentage is the SLO target as a percentage (e.g., 99.9).
	TargetPercentage float64
	// WindowDuration is the rolling window over which the SLO is calculated.
	WindowDuration time.Duration
}

// SLOTracker tracks uptime and error budgets for a set of SLOs.
// It records successes and failures and computes remaining error budget.
// All methods are safe for concurrent use.
type SLOTracker struct {
	slos map[string]*sloState
	mu   sync.RWMutex
}

// sloState holds the internal tracking state for a single SLO.
type sloState struct {
	definition  SLODefinition
	successes   atomic.Int64
	failures    atomic.Int64
	windowStart time.Time
	mu          sync.Mutex // protects windowStart
}

// NewSLOTracker creates a new SLOTracker.
func NewSLOTracker() *SLOTracker {
	return &SLOTracker{
		slos: make(map[string]*sloState),
	}
}

// RegisterSLO registers a new SLO definition. If an SLO with the same name
// already exists, it is replaced with the new definition and counters reset.
func (t *SLOTracker) RegisterSLO(def SLODefinition) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.slos[def.Name] = &sloState{
		definition:  def,
		windowStart: time.Now().UTC(),
	}
}

// UnregisterSLO removes an SLO from tracking.
func (t *SLOTracker) UnregisterSLO(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.slos, name)
}

// RecordSuccess records a successful operation for the named SLO.
// If the SLO is not registered, this is a no-op.
func (t *SLOTracker) RecordSuccess(name string) {
	t.mu.RLock()
	state, ok := t.slos[name]
	t.mu.RUnlock()
	if !ok {
		return
	}
	state.successes.Add(1)
	t.maybeResetWindow(state)
}

// RecordFailure records a failed operation for the named SLO.
// If the SLO is not registered, this is a no-op.
func (t *SLOTracker) RecordFailure(name string) {
	t.mu.RLock()
	state, ok := t.slos[name]
	t.mu.RUnlock()
	if !ok {
		return
	}
	state.failures.Add(1)
	t.maybeResetWindow(state)
}

// maybeResetWindow checks if the sliding window has elapsed and resets counters.
func (t *SLOTracker) maybeResetWindow(state *sloState) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if time.Since(state.windowStart) >= state.definition.WindowDuration {
		state.successes.Store(0)
		state.failures.Store(0)
		state.windowStart = time.Now().UTC()
	}
}

// ErrorBudgetRemaining returns the remaining error budget as a percentage of
// total operations. For example, if the target is 99.9% and the current error
// rate is 0.05%, the remaining budget is 0.05% (50% of the allocated 0.1%).
// Returns -1 if the SLO is unknown.
func (t *SLOTracker) ErrorBudgetRemaining(name string) float64 {
	t.mu.RLock()
	state, ok := t.slos[name]
	t.mu.RUnlock()
	if !ok {
		return -1
	}

	successes := float64(state.successes.Load())
	failures := float64(state.failures.Load())
	total := successes + failures

	if total == 0 {
		// No data yet — full budget available.
		return 100.0
	}

	errorRate := failures / total
	allowedErrorRate := (100.0 - state.definition.TargetPercentage) / 100.0

	if errorRate >= allowedErrorRate {
		return 0.0
	}

	// Remaining budget as percentage of allowed errors.
	remaining := (1.0 - errorRate/allowedErrorRate) * 100.0
	return remaining
}

// CurrentErrorRate returns the current error rate as a percentage (0-100).
// Returns -1 if the SLO is unknown.
func (t *SLOTracker) CurrentErrorRate(name string) float64 {
	t.mu.RLock()
	state, ok := t.slos[name]
	t.mu.RUnlock()
	if !ok {
		return -1
	}

	successes := float64(state.successes.Load())
	failures := float64(state.failures.Load())
	total := successes + failures

	if total == 0 {
		return 0.0
	}

	return (failures / total) * 100.0
}

// BurnRateAlert returns true if the error budget is depleting at a rate that
// would exhaust it before the window ends. It checks for a burn rate > 1x
// (i.e., consuming budget faster than linearly).
//
// A burn rate of 1x means the budget is being consumed at a rate that would
// exactly exhaust it at the end of the window. > 1x means it will run out
// before the window ends.
func (t *SLOTracker) BurnRateAlert(name string) bool {
	t.mu.RLock()
	state, ok := t.slos[name]
	t.mu.RUnlock()
	if !ok {
		return false
	}

	successes := float64(state.successes.Load())
	failures := float64(state.failures.Load())
	total := successes + failures

	if total == 0 {
		return false
	}

	// Calculate the fraction of window elapsed.
	state.mu.Lock()
	elapsed := time.Since(state.windowStart)
	state.mu.Unlock()

	windowFraction := elapsed.Seconds() / state.definition.WindowDuration.Seconds()
	if windowFraction <= 0 || windowFraction >= 1.0 {
		// Window just started or already over (should reset).
		return false
	}

	// Current error budget consumption fraction.
	errorRate := failures / total
	allowedErrorRate := (100.0 - state.definition.TargetPercentage) / 100.0

	if allowedErrorRate == 0 {
		return errorRate > 0
	}

	budgetConsumedFraction := errorRate / allowedErrorRate

	// Burn rate = fraction of budget consumed / fraction of window elapsed.
	burnRate := budgetConsumedFraction / windowFraction

	// Alert if burn rate > 1.0 (depleting faster than linear).
	return burnRate > 1.0
}

// GetBurnRate returns the current burn rate (> 0). A value > 1 means the
// error budget will be exhausted before the window ends.
func (t *SLOTracker) GetBurnRate(name string) float64 {
	t.mu.RLock()
	state, ok := t.slos[name]
	t.mu.RUnlock()
	if !ok {
		return -1
	}

	successes := float64(state.successes.Load())
	failures := float64(state.failures.Load())
	total := successes + failures

	if total == 0 {
		return 0
	}

	state.mu.Lock()
	elapsed := time.Since(state.windowStart)
	state.mu.Unlock()

	windowFraction := elapsed.Seconds() / state.definition.WindowDuration.Seconds()
	if windowFraction <= 0 {
		return 0
	}

	errorRate := failures / total
	allowedErrorRate := (100.0 - state.definition.TargetPercentage) / 100.0

	if allowedErrorRate == 0 {
		if errorRate > 0 {
			return 999.0 // effectively infinite burn rate
		}
		return 0
	}

	budgetConsumedFraction := errorRate / allowedErrorRate
	return budgetConsumedFraction / windowFraction
}

// Reset resets all counters for the named SLO. No-op if unknown.
func (t *SLOTracker) Reset(name string) {
	t.mu.RLock()
	state, ok := t.slos[name]
	t.mu.RUnlock()
	if !ok {
		return
	}
	state.successes.Store(0)
	state.failures.Store(0)
	state.mu.Lock()
	state.windowStart = time.Now().UTC()
	state.mu.Unlock()
}

// ListSLOs returns the names of all registered SLOs.
func (t *SLOTracker) ListSLOs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	names := make([]string, 0, len(t.slos))
	for name := range t.slos {
		names = append(names, name)
	}
	return names
}
