//go:build !race

package processing

// raceEnabled reports whether the race detector is active in this build.
// See race_enabled_test.go for why load tests are skipped under race.
const raceEnabled = false
