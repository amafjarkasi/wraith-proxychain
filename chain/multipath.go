package chain

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// PathDialer is a single dialable path (satisfied by *Chain).
type PathDialer interface {
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
	Stop()
}

// MultipathPolicy selects how multiple paths are used for one dial.
type MultipathPolicy int

const (
	// MultipathFailover tries paths in order until one succeeds.
	MultipathFailover MultipathPolicy = iota
	// MultipathStripe rotates the starting path per dial (round-robin).
	MultipathStripe
	// MultipathFastest picks the path with the fewest failures, then lowest latency.
	MultipathFastest
	// MultipathRace dials every path in parallel and returns the first success.
	MultipathRace
)

type pathStat struct {
	latSum time.Duration
	n      int
	fails  int
}

// MultiChain dials through one of several paths using a MultipathPolicy.
type MultiChain struct {
	paths  []PathDialer
	policy MultipathPolicy

	mu    sync.Mutex
	rr    int
	stats []pathStat
}

// NewMultiChain builds a multipath dialer. paths must be non-empty.
func NewMultiChain(paths []PathDialer, policy MultipathPolicy) *MultiChain {
	return &MultiChain{
		paths:  append([]PathDialer(nil), paths...),
		policy: policy,
		stats:  make([]pathStat, len(paths)),
	}
}

// Dial opens a connection using the configured policy.
func (m *MultiChain) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if len(m.paths) == 0 {
		return nil, fmt.Errorf("multipath: no paths")
	}
	if m.policy == MultipathRace {
		return m.dialRace(ctx, network, addr)
	}
	order := m.order()
	var lastErr error
	for _, i := range order {
		start := time.Now()
		conn, err := m.paths[i].Dial(ctx, network, addr)
		if err != nil {
			m.record(i, time.Since(start), err)
			lastErr = err
			continue
		}
		m.record(i, time.Since(start), nil)
		return conn, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("multipath: all paths failed")
	}
	return nil, lastErr
}

// Stop stops all underlying paths.
func (m *MultiChain) Stop() {
	for _, p := range m.paths {
		p.Stop()
	}
}

func (m *MultiChain) order() []int {
	idx := make([]int, len(m.paths))
	for i := range idx {
		idx[i] = i
	}
	switch m.policy {
	case MultipathStripe:
		m.mu.Lock()
		start := m.rr % len(idx)
		m.rr++
		m.mu.Unlock()
		out := make([]int, len(idx))
		for i := range idx {
			out[i] = idx[(start+i)%len(idx)]
		}
		return out
	case MultipathFastest:
		m.mu.Lock()
		stats := append([]pathStat(nil), m.stats...)
		m.mu.Unlock()
		sort.SliceStable(idx, func(a, b int) bool {
			sa, sb := stats[idx[a]], stats[idx[b]]
			if sa.fails != sb.fails {
				return sa.fails < sb.fails
			}
			return avg(sa) < avg(sb)
		})
	}
	return idx
}

func avg(s pathStat) time.Duration {
	if s.n == 0 {
		return 0
	}
	return s.latSum / time.Duration(s.n)
}

func (m *MultiChain) record(i int, latency time.Duration, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &m.stats[i]
	s.n++
	s.latSum += latency
	if err != nil {
		s.fails++
	}
}

type raceResult struct {
	idx     int
	conn    net.Conn
	err     error
	latency time.Duration
}

func (m *MultiChain) dialRace(ctx context.Context, network, addr string) (net.Conn, error) {
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan raceResult, len(m.paths))
	for i, p := range m.paths {
		go func(i int, p PathDialer) {
			start := time.Now()
			conn, err := p.Dial(rctx, network, addr)
			results <- raceResult{idx: i, conn: conn, err: err, latency: time.Since(start)}
		}(i, p)
	}
	var winner raceResult
	won := false
	var firstErr error
	for range m.paths {
		res := <-results
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			m.record(res.idx, res.latency, res.err)
			continue
		}
		if won {
			res.conn.Close()
			continue
		}
		winner = res
		won = true
		m.record(res.idx, res.latency, nil)
		cancel()
	}
	if !won {
		if firstErr == nil {
			firstErr = fmt.Errorf("multipath: all paths failed")
		}
		return nil, firstErr
	}
	return winner.conn, nil
}
