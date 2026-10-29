//go:build race

package pcre2

// The race detector makes sync.Pool drop items at random.
const raceEnabled = true
