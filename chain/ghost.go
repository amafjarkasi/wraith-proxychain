package chain

import "time"

// GhostOptions returns options for maximum evasion.
func GhostOptions() []Option {
	return []Option{
		WithJitter(50*time.Millisecond, 500*time.Millisecond),
		WithRetries(3),
		WithTunnel(true),
		WithRateLimit(50 * 1024),
		WithPoolSize(4),
		WithHealthInterval(15 * time.Second),
	}
}
