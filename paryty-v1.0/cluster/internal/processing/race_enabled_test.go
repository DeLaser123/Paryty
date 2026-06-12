//go:build race

package processing

// raceEnabled reports whether the race detector is active in this build.
// Heavy load tests (100K agents, 1M messages) are skipped under the race
// detector: instrumentation slows execution by 5-20x, which blows past the
// default 10-minute test timeout and turns verification Gate 3 red without
// finding any real defect. Load behavior is verified by the non-race run
// (verify-go Gate 5 / explicit `go test -run Load`).
const raceEnabled = true
