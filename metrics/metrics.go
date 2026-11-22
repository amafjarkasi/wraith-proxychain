package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type hopStat struct {
	dials    int64
	failures int64
	latSum   int64
	latN     int64
	lastErr  atomic.Value
}

type Metrics struct {
	Dials    int64
	Failures int64
	BytesIn  int64
	BytesOut int64
	latSum   int64 // nanos
	latN     int64

	hopMu sync.RWMutex
	hops  map[string]*hopStat
}

func (m *Metrics) hop(name string) *hopStat {
	m.hopMu.RLock()
	s := m.hops[name]
	m.hopMu.RUnlock()
	if s != nil {
		return s
	}
	m.hopMu.Lock()
	defer m.hopMu.Unlock()
	if m.hops == nil {
		m.hops = make(map[string]*hopStat)
	}
	if s = m.hops[name]; s == nil {
		s = &hopStat{}
		m.hops[name] = s
	}
	return s
}

func (m *Metrics) RecordDial(lat time.Duration, err error) {
	atomic.AddInt64(&m.Dials, 1)
	if err != nil {
		atomic.AddInt64(&m.Failures, 1)
	}
	atomic.AddInt64(&m.latSum, int64(lat))
	atomic.AddInt64(&m.latN, 1)
}

// RecordHop records a per-proxy attempt. Latency is nanoseconds from the
// chain's health tracker; zero means the attempt failed before timing.
func (m *Metrics) RecordHop(proxy string, lat time.Duration, err error) {
	if proxy == "" {
		return
	}
	s := m.hop(proxy)
	atomic.AddInt64(&s.dials, 1)
	atomic.AddInt64(&s.latSum, int64(lat))
	atomic.AddInt64(&s.latN, 1)
	if err != nil {
		atomic.AddInt64(&s.failures, 1)
		s.lastErr.Store(err.Error())
	}
}

func (m *Metrics) AddBytes(in, out int64) {
	atomic.AddInt64(&m.BytesIn, in)
	atomic.AddInt64(&m.BytesOut, out)
}

func (m *Metrics) Snapshot() map[string]any {
	n := atomic.LoadInt64(&m.latN)
	avg := int64(0)
	if n > 0 {
		avg = atomic.LoadInt64(&m.latSum) / n
	}
	out := map[string]any{
		"dials":      atomic.LoadInt64(&m.Dials),
		"failures":   atomic.LoadInt64(&m.Failures),
		"bytes_in":   atomic.LoadInt64(&m.BytesIn),
		"bytes_out":  atomic.LoadInt64(&m.BytesOut),
		"avg_lat_ns": avg,
	}

	m.hopMu.RLock()
	hops := make(map[string]any, len(m.hops))
	for name, s := range m.hops {
		hn := atomic.LoadInt64(&s.latN)
		havg := int64(0)
		if hn > 0 {
			havg = atomic.LoadInt64(&s.latSum) / hn
		}
		entry := map[string]any{
			"dials":      atomic.LoadInt64(&s.dials),
			"failures":   atomic.LoadInt64(&s.failures),
			"avg_lat_ns": havg,
		}
		if v := s.lastErr.Load(); v != nil {
			entry["last_error"] = v
		}
		hops[name] = entry
	}
	m.hopMu.RUnlock()
	out["hops"] = hops
	return out
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := m.Snapshot()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, k := range []string{"dials", "failures", "bytes_in", "bytes_out", "avg_lat_ns"} {
			fmt.Fprintf(w, "%s %v\n", k, s[k])
		}
		if hops, ok := s["hops"].(map[string]any); ok {
			names := make([]string, 0, len(hops))
			for name := range hops {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				entry, _ := hops[name].(map[string]any)
				fmt.Fprintf(w, "hop{%s} dials=%v failures=%v avg_lat_ns=%v",
					strings.ReplaceAll(name, " ", "_"), entry["dials"], entry["failures"], entry["avg_lat_ns"])
				if e, ok := entry["last_error"]; ok {
					fmt.Fprintf(w, " last_error=%q", e)
				}
				fmt.Fprintln(w)
			}
		}
	})
}
