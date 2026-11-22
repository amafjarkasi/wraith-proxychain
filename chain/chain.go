package chain

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/wraith/proxychain/crypto"
	"github.com/wraith/proxychain/dns"
	"github.com/wraith/proxychain/evade"
	"github.com/wraith/proxychain/hop"
	"github.com/wraith/proxychain/ws"
)

type Option func(*Chain)

type Chain struct {
	Hops        []*hop.Proxy
	dialer      hop.ContextDialer
	Strategy    Rotator
	SessionKey  string
	sessionTTL  time.Duration
	jitterMin   time.Duration
	jitterMax   time.Duration
	timingMin   time.Duration
	timingMax   time.Duration
	cover       evade.CoverConfig
	coverOn     bool
	retries     int
	tunnel      bool
	wsURL       string
	wsCfg       ws.Config
	metrics     Metrics
	sessionFile string
	healthTick  time.Duration
	healthStop  chan struct{}
	poolSize    int
	failClosed  bool
	route       func(string) []*hop.Proxy
	poolMu      sync.Mutex
	pool        map[string][]net.Conn
	rate        RateLimiter
	spares      []*hop.Proxy
	spareMu     sync.Mutex
	stopOnce    sync.Once

	mu       sync.Mutex
	sessions SessionStore
	health   map[*hop.Proxy]*healthState
	rr       *RoundRobin
}

// Metrics is the minimal recorder interface (satisfied by metrics.Metrics).
type Metrics interface {
	RecordDial(latency time.Duration, err error)
}

// HopMetrics is an optional extension for per-proxy telemetry. Metrics that
// implement it receive one record per hop attempt, not just per chain dial.
type HopMetrics interface {
	RecordHop(proxy string, latency time.Duration, err error)
}

var errHopFailed = fmt.Errorf("hop failed")

func New(hops []*hop.Proxy, opts ...Option) *Chain {
	c := &Chain{
		Hops:       append([]*hop.Proxy(nil), hops...),
		Strategy:   &Random{},
		sessionTTL: 5 * time.Minute,
		health:     make(map[*hop.Proxy]*healthState),
	}
	for _, o := range opts {
		o(c)
	}
	if c.dialer != nil {
		for i, h := range c.Hops {
			if h != nil {
				c.Hops[i] = h.WithDialer(c.dialer)
			}
		}
		for i, s := range c.spares {
			if s != nil {
				c.spares[i] = s.WithDialer(c.dialer)
			}
		}
	}
	if c.Strategy == nil {
		c.Strategy = &Random{}
	}
	// inject chain into rotators that want it
	if w, ok := c.Strategy.(*Weighted); ok {
		w.chain = c
	}
	if l, ok := c.Strategy.(*Latency); ok {
		l.chain = c
	}
	if rr, ok := c.Strategy.(*RoundRobin); ok {
		c.rr = rr
	} else {
		// keep a roundrobin instance for fallback? not needed
	}
	if c.sessions == nil {
		c.sessions = newSessionStore(c.sessionTTL, c.sessionFile)
	}
	c.pool = make(map[string][]net.Conn)
	if c.healthTick <= 0 {
		c.healthTick = 30 * time.Second
	}
	c.startHealthProbes()
	return c
}

func WithStrategy(r Rotator) Option {
	return func(c *Chain) { c.Strategy = r }
}

// WithDialer sets the raw TCP dialer used for direct connections and for
// reaching upstream proxies. Pass dns.NewDialer() to resolve via DoH.
func WithDialer(d hop.ContextDialer) Option {
	return func(c *Chain) { c.dialer = d }
}

// WithDoH resolves DNS via DNS-over-HTTPS for direct dials, proxy addresses,
// and health probes. Equivalent to WithDialer(dns.NewDialer()).
func WithDoH() Option {
	return func(c *Chain) { c.dialer = dns.NewDialer() }
}

func WithSessionTTL(d time.Duration) Option {
	return func(c *Chain) { c.sessionTTL = d }
}

func WithSessionFile(path string) Option {
	return func(c *Chain) { c.sessionFile = path }
}

func WithSessionStore(s SessionStore) Option {
	return func(c *Chain) { c.sessions = s }
}

func WithHealthInterval(d time.Duration) Option {
	return func(c *Chain) { c.healthTick = d }
}

func WithPoolSize(n int) Option {
	return func(c *Chain) { c.poolSize = n }
}

func WithFailClosed(on bool) Option {
	return func(c *Chain) { c.failClosed = on }
}

func WithRoute(fn func(string) []*hop.Proxy) Option {
	return func(c *Chain) { c.route = fn }
}

func WithRateLimit(bytesPerSec int64) Option {
	return func(c *Chain) {
		if bytesPerSec > 0 {
			c.rate = newRateLimiter(bytesPerSec)
		}
	}
}

func WithRateLimiter(rl RateLimiter) Option {
	return func(c *Chain) { c.rate = rl }
}

func WithSpareProxies(spares []*hop.Proxy) Option {
	return func(c *Chain) {
		c.spares = append([]*hop.Proxy(nil), spares...)
	}
}

func WithJitter(min, max time.Duration) Option {
	return func(c *Chain) {
		c.jitterMin = min
		c.jitterMax = max
	}
}

// WithTimingJitter adds crypto-random delay before every write on the dialed
// connection. It changes timing only, not bytes.
func WithTimingJitter(min, max time.Duration) Option {
	return func(c *Chain) { c.timingMin, c.timingMax = min, max }
}

// WithCoverTraffic adds length-prefixed decoy frames on idle connections.
// Both endpoints must wrap with evade.NewCoverConn; cover frames are stripped
// on read. Do not use against a peer that expects raw bytes.
func WithCoverTraffic(cfg evade.CoverConfig) Option {
	return func(c *Chain) { c.cover, c.coverOn = cfg, true }
}

func WithRetries(n int) Option {
	return func(c *Chain) { c.retries = n }
}

func WithTunnel(on bool) Option {
	return func(c *Chain) { c.tunnel = on }
}

// WithWebSocketTunnel upgrades the dialed connection to a WebSocket tunnel.
func WithWebSocketTunnel(rawURL string, cfg ws.Config) Option {
	return func(c *Chain) {
		c.wsURL = rawURL
		c.wsCfg = cfg
	}
}

func WithMetrics(m Metrics) Option {
	return func(c *Chain) { c.metrics = m }
}

func (c *Chain) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if len(c.Hops) == 0 && c.route == nil {
		if c.failClosed {
			return nil, fmt.Errorf("chain: no proxies configured (fail-closed)")
		}
		start := time.Now()
		conn, err := c.rawDial(ctx, network, addr)
		if err != nil {
			if c.metrics != nil {
				c.metrics.RecordDial(time.Since(start), err)
			}
			return nil, err
		}
		return c.decorate(ctx, conn, addr, nil, time.Since(start))
	}

	// self-heal: replace dead hops with spares
	c.heal()

	// pool
	if c.poolSize > 0 {
		if pc := c.getPool(addr); pc != nil {
			return pc, nil
		}
	}

	attempts := c.retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for a := 0; a < attempts; a++ {
		start := time.Now()
		conn, order, err := c.dialOnce(ctx, network, addr)
		elapsed := time.Since(start)
		if err == nil {
			return c.decorate(ctx, conn, addr, order, elapsed)
		}
		lastErr = err
		if c.metrics != nil {
			c.metrics.RecordDial(elapsed, err)
		}
	}
	return nil, lastErr
}

func (c *Chain) rawDial(ctx context.Context, network, addr string) (net.Conn, error) {
	if c.dialer != nil {
		return c.dialer.DialContext(ctx, network, addr)
	}
	d := &net.Dialer{}
	return d.DialContext(ctx, network, addr)
}

func (c *Chain) decorate(ctx context.Context, conn net.Conn, addr string, order []*hop.Proxy, elapsed time.Duration) (net.Conn, error) {
	if c.SessionKey != "" {
		c.sessions.Set(c.SessionKey, order, c.sessionTTL)
	}
	if c.wsURL != "" {
		wc, err := ws.WrapClient(ctx, conn, c.wsURL, c.wsCfg)
		if err != nil {
			conn.Close()
			return nil, err
		}
		conn = wc
	}
	if c.tunnel {
		tun, err := crypto.WrapContext(ctx, conn, true)
		if err != nil {
			conn.Close()
			return nil, err
		}
		conn = tun
	}
	if c.timingMin > 0 || c.timingMax > 0 {
		conn = evade.NewTimingConn(conn, c.timingMin, c.timingMax)
	}
	if c.coverOn {
		conn = evade.NewCoverConn(conn, c.cover)
	}
	if c.poolSize > 0 {
		conn = c.wrapPooled(conn, addr)
	}
	if c.rate != nil {
		conn = &rateLimitedConn{Conn: conn, rl: c.rate}
	}
	if c.metrics != nil {
		c.metrics.RecordDial(elapsed, nil)
	}
	return conn, nil
}

func (c *Chain) startHealthProbes() {
	if c.healthTick <= 0 {
		return
	}
	stop := make(chan struct{})
	c.healthStop = stop
	go func() {
		t := time.NewTicker(c.healthTick)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				c.mu.Lock()
				hops := append([]*hop.Proxy(nil), c.Hops...)
				c.mu.Unlock()
				for _, h := range hops {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					c.Probe(ctx, h)
					cancel()
				}
			}
		}
	}()
}

func (c *Chain) Stop() {
	c.stopOnce.Do(func() {
		if c.healthStop != nil {
			close(c.healthStop)
			c.healthStop = nil
		}
	})
	// drain pool
	c.poolMu.Lock()
	defer c.poolMu.Unlock()
	for k, conns := range c.pool {
		for _, conn := range conns {
			conn.Close()
		}
		delete(c.pool, k)
	}
}

type pooledConn struct {
	net.Conn
	c   *Chain
	key string
}

func (p *pooledConn) Close() error {
	if p.c == nil {
		return p.Conn.Close()
	}
	p.c.returnConn(p.key, p.Conn)
	p.c = nil
	return nil
}

func (c *Chain) getPool(key string) net.Conn {
	c.poolMu.Lock()
	defer c.poolMu.Unlock()
	if len(c.pool[key]) == 0 {
		return nil
	}
	pc := c.pool[key][len(c.pool[key])-1]
	c.pool[key] = c.pool[key][:len(c.pool[key])-1]
	return pc
}

func (c *Chain) wrapPooled(conn net.Conn, key string) net.Conn {
	return &pooledConn{Conn: conn, c: c, key: key}
}

func (c *Chain) returnConn(key string, conn net.Conn) {
	c.poolMu.Lock()
	defer c.poolMu.Unlock()
	if len(c.pool[key]) >= c.poolSize {
		conn.Close()
		return
	}
	c.pool[key] = append(c.pool[key], conn)
}

func (c *Chain) heal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spareMu.Lock()
	defer c.spareMu.Unlock()
	for i, h := range c.Hops {
		if c.isDeadLocked(h) && len(c.spares) > 0 {
			c.Hops[i] = c.spares[0]
			c.spares = c.spares[1:]
		}
	}
}

func (c *Chain) isDeadLocked(h *hop.Proxy) bool {
	s := c.health[h]
	if s == nil {
		s = &healthState{}
		c.health[h] = s
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().Before(s.deadUntil)
}

func (c *Chain) dialOnce(ctx context.Context, network, addr string) (net.Conn, []*hop.Proxy, error) {
	order := c.resolveOrder(addr)
	if len(order) == 0 {
		if c.failClosed {
			return nil, nil, fmt.Errorf("chain: no proxies configured (fail-closed)")
		}
		return nil, nil, fmt.Errorf("chain: no hops")
	}
	alive := make([]*hop.Proxy, 0, len(order))
	for _, h := range order {
		if !c.isDead(h) {
			alive = append(alive, h)
		}
	}
	if len(alive) > 0 {
		order = alive
	}

	var conn net.Conn
	var err error
	if len(order) == 1 {
		conn, err = order[0].Dial(ctx, network, addr)
		if err != nil {
			c.recordFailure(order[0])
			return nil, nil, err
		}
		c.recordSuccess(order[0], 0)
	} else {
		conn, err = order[0].Dial(ctx, network, order[1].Address())
		if err != nil {
			c.recordFirstHopFailure(order[0], order[1], err)
			return nil, nil, err
		}
		c.recordSuccess(order[0], 0)
		for i := 1; i < len(order)-1; i++ {
			c.jitter(ctx)
			conn, err = hop.ConnectOver(conn, order[i+1].Address(), order[i])
			if err != nil {
				if conn != nil {
					conn.Close()
				}
				c.recordFailure(order[i+1])
				return nil, nil, err
			}
			c.recordSuccess(order[i], 0)
		}
		c.jitter(ctx)
		conn, err = hop.ConnectOver(conn, addr, order[len(order)-1])
		if err != nil {
			if conn != nil {
				conn.Close()
			}
			c.recordFailure(order[len(order)-1])
			return nil, nil, err
		}
		c.recordSuccess(order[len(order)-1], 0)
	}
	return conn, order, nil
}

func (c *Chain) resolveOrder(addr string) []*hop.Proxy {
	c.mu.Lock()
	hops := append([]*hop.Proxy(nil), c.Hops...)
	c.mu.Unlock()
	if c.route != nil {
		host := addr
		if h, _, err := net.SplitHostPort(addr); err == nil {
			host = h
		}
		if rh := c.route(host); rh != nil {
			hops = append([]*hop.Proxy(nil), rh...)
		}
	}
	if c.SessionKey != "" {
		if o, ok := c.sessions.Get(c.SessionKey); ok {
			return o
		}
	}
	order := c.Strategy.Order(hops)
	// inject chain into rotators on first use
	if w, ok := c.Strategy.(*Weighted); ok && w.chain == nil {
		w.chain = c
	}
	if l, ok := c.Strategy.(*Latency); ok && l.chain == nil {
		l.chain = c
	}
	return order
}

func (c *Chain) jitter(ctx context.Context) {
	if c.jitterMax <= 0 {
		return
	}
	min := c.jitterMin
	max := c.jitterMax
	if max < min {
		max = min
	}
	var d time.Duration
	if max == min {
		d = min
	} else {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
		if err != nil {
			d = min
		} else {
			d = min + time.Duration(n.Int64())
		}
	}
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (c *Chain) healthStateFor(h *hop.Proxy) *healthState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.health[h]; ok {
		return s
	}
	s := &healthState{}
	c.health[h] = s
	return s
}

func (c *Chain) isDead(h *hop.Proxy) bool {
	s := c.healthStateFor(h)
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().Before(s.deadUntil)
}

// recordFirstHopFailure attributes a failed first-hop dial to the proxy itself
// when the TCP connect to it failed, otherwise to the downstream hop it was
// being asked to reach.
func (c *Chain) recordFirstHopFailure(first, next *hop.Proxy, err error) {
	if strings.Contains(err.Error(), first.Address()) {
		c.recordFailure(first)
		return
	}
	c.recordFailure(next)
}

func (c *Chain) recordFailure(h *hop.Proxy) {
	if hm, ok := c.metrics.(HopMetrics); ok && h != nil && h.URL != nil {
		hm.RecordHop(h.URL.String(), 0, errHopFailed)
	}
	s := c.healthStateFor(h)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures++
	if s.failures > 30 {
		s.failures = 30
	}
	backoff := time.Second << (s.failures - 1)
	if backoff > 5*time.Minute || backoff < 0 {
		backoff = 5 * time.Minute
	}
	jitter := time.Duration(0)
	if backoff > 0 {
		r, _ := rand.Int(rand.Reader, big.NewInt(int64(backoff/4+1)))
		jitter = time.Duration(r.Int64())
	}
	s.deadUntil = time.Now().Add(backoff + jitter)
}

func (c *Chain) recordSuccess(h *hop.Proxy, latency time.Duration) {
	if hm, ok := c.metrics.(HopMetrics); ok && h != nil && h.URL != nil {
		hm.RecordHop(h.URL.String(), latency, nil)
	}
	s := c.healthStateFor(h)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = 0
	if latency > 0 {
		s.latency = latency
	}
}

// Probe measures dial latency to a hop's proxy address.
func (c *Chain) Probe(ctx context.Context, h *hop.Proxy) time.Duration {
	start := time.Now()
	addr := h.Address()
	conn, err := c.rawDial(ctx, "tcp", addr)
	elapsed := time.Since(start)
	if err != nil {
		c.recordFailure(h)
		return 0
	}
	conn.Close()
	c.recordSuccess(h, elapsed)
	return elapsed
}
