package chain

import (
	"net"
	"sync"
	"time"
)

// RateLimiter is the interface used to throttle connections; implement it
// externally (e.g. Redis-backed) for distributed limits.
type RateLimiter interface {
	Wait(n int)
}

type rateLimiter struct {
	mu     sync.Mutex
	rate   int64 // bytes per sec
	bucket int64
	last   time.Time
}

func newRateLimiter(bps int64) *rateLimiter {
	return &rateLimiter{rate: bps, bucket: bps, last: time.Now()}
}

func (r *rateLimiter) Wait(n int) {
	if r.rate <= 0 {
		return
	}
	r.mu.Lock()
	now := time.Now()
	elapsed := now.Sub(r.last)
	r.last = now
	r.bucket += int64(elapsed.Seconds() * float64(r.rate))
	if r.bucket > r.rate {
		r.bucket = r.rate
	}
	var sleep time.Duration
	if r.bucket < int64(n) {
		deficit := int64(n) - r.bucket
		sleep = time.Duration(float64(deficit) / float64(r.rate) * float64(time.Second))
		r.bucket = 0
	} else {
		r.bucket -= int64(n)
	}
	r.mu.Unlock()
	if sleep > 0 {
		time.Sleep(sleep)
	}
}

type rateLimitedConn struct {
	net.Conn
	rl RateLimiter
}

func (r *rateLimitedConn) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if n > 0 {
		r.rl.Wait(n)
	}
	return n, err
}

func (r *rateLimitedConn) Write(p []byte) (int, error) {
	r.rl.Wait(len(p))
	return r.Conn.Write(p)
}
