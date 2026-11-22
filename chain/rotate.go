package chain

import (
	"context"
	"crypto/rand"
	"math/big"
	"sort"
	"sync"

	"github.com/wraith/proxychain/hop"
)

type Rotator interface {
	Order(hops []*hop.Proxy) []*hop.Proxy
}

type Random struct{}

func NewRandom() Rotator      { return &Random{} }
func RandomStrategy() Rotator { return &Random{} }

func (Random) Order(hops []*hop.Proxy) []*hop.Proxy {
	out := append([]*hop.Proxy(nil), hops...)
	for i := len(out) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			continue
		}
		j := int(n.Int64())
		out[i], out[j] = out[j], out[i]
	}
	return out
}

type RoundRobin struct {
	mu  sync.Mutex
	idx int
}

func NewRoundRobin() Rotator { return &RoundRobin{} }

func (r *RoundRobin) Order(hops []*hop.Proxy) []*hop.Proxy {
	if len(hops) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.idx % len(hops)
	r.idx++
	out := make([]*hop.Proxy, len(hops))
	for i := 0; i < len(hops); i++ {
		out[i] = hops[(start+i)%len(hops)]
	}
	return out
}

type Weighted struct {
	chain *Chain
}

func NewWeighted(c *Chain) Rotator {
	w := &Weighted{chain: c}
	return w
}

func (w *Weighted) Order(hops []*hop.Proxy) []*hop.Proxy {
	out := append([]*hop.Proxy(nil), hops...)
	if w.chain != nil {
		// drop dead
		filtered := make([]*hop.Proxy, 0, len(out))
		for _, h := range out {
			if !w.chain.isDead(h) {
				filtered = append(filtered, h)
			}
		}
		if len(filtered) > 0 {
			out = filtered
		} else {
			out = append([]*hop.Proxy(nil), hops...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if w.chain != nil {
			fi, fj := w.chain.FailureCount(out[i]), w.chain.FailureCount(out[j])
			if fi != fj {
				return fi < fj
			}
			si, sj := w.chain.HealthScore(out[i]), w.chain.HealthScore(out[j])
			if si == 0 {
				si = 1 << 62
			}
			if sj == 0 {
				sj = 1 << 62
			}
			return si < sj
		}
		return false
	})
	return out
}

type Latency struct {
	chain *Chain
}

func NewLatency(c *Chain) Rotator {
	return &Latency{chain: c}
}

func (l *Latency) Order(hops []*hop.Proxy) []*hop.Proxy {
	out := append([]*hop.Proxy(nil), hops...)
	if l.chain == nil {
		return out
	}
	// probe (parallel)
	var wg sync.WaitGroup
	for _, h := range out {
		h := h
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.chain.Probe(context.Background(), h)
		}()
	}
	wg.Wait()
	// drop dead
	filtered := make([]*hop.Proxy, 0, len(out))
	for _, h := range out {
		if !l.chain.isDead(h) {
			filtered = append(filtered, h)
		}
	}
	if len(filtered) > 0 {
		out = filtered
	}
	sort.SliceStable(out, func(i, j int) bool {
		si := l.chain.HealthScore(out[i])
		sj := l.chain.HealthScore(out[j])
		if si == 0 {
			si = 1 << 62
		}
		if sj == 0 {
			sj = 1 << 62
		}
		return si < sj
	})
	return out
}
