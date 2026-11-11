//go:build race

package testutil

// RaceEnabled reports whether this test binary was built with -race.
func RaceEnabled() bool { return true }
