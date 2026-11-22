package chain

import (
	"sync"
	"time"

	"github.com/wraith/proxychain/hop"
)

type healthState struct {
	mu        sync.Mutex
	failures  int
	deadUntil time.Time
	latency   time.Duration
}

// HealthScore returns stored latency (0 if unknown).
func (c *Chain) HealthScore(h *hop.Proxy) time.Duration {
	s := c.healthStateFor(h)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latency
}

func (c *Chain) FailureCount(h *hop.Proxy) int {
	s := c.healthStateFor(h)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failures
}

// IsDead exported check.
func (c *Chain) IsDead(h *hop.Proxy) bool { return c.isDead(h) }

// HopsSnapshot returns a copy of the current hop list for safe iteration.
func (c *Chain) HopsSnapshot() []*hop.Proxy {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*hop.Proxy(nil), c.Hops...)
}

// HopStat summarizes a hop's health.
type HopStat struct {
	URL     string
	Dead    bool
	Fails   int
	Latency time.Duration
}

// HopStats returns a snapshot of per-hop health.
func (c *Chain) HopStats() []HopStat {
	c.mu.Lock()
	hops := append([]*hop.Proxy(nil), c.Hops...)
	c.mu.Unlock()
	out := make([]HopStat, 0, len(hops))
	for _, h := range hops {
		out = append(out, HopStat{
			URL:     h.URL.String(),
			Dead:    c.isDead(h),
			Fails:   c.FailureCount(h),
			Latency: c.HealthScore(h),
		})
	}
	return out
}
