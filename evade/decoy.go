package evade

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TimingConn jitters each Write by a crypto-random delay in [min,max].
// It changes packet timing only, never bytes, so it is safe on any stream.
type TimingConn struct {
	net.Conn
	min, max time.Duration
}

// NewTimingConn wraps c with write timing shaping.
func NewTimingConn(c net.Conn, min, max time.Duration) *TimingConn {
	if max < min {
		max = min
	}
	return &TimingConn{Conn: c, min: min, max: max}
}

func (c *TimingConn) Write(p []byte) (int, error) {
	Jitter(c.min, c.max)
	return c.Conn.Write(p)
}

// DNSResolver is the subset of dns.Resolver needed by DecoyDNS.
type DNSResolver interface {
	ResolveContext(ctx context.Context, host string) (string, error)
}

// DecoyDNS resolves shuffled benign domains through a resolver (e.g. DoH) so
// resolver traffic blends with normal browsing. Resolve errors are ignored.
func DecoyDNS(ctx context.Context, r DNSResolver, domains []string) error {
	for _, d := range RandomOrder(domains) {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, _ = r.ResolveContext(ctx, d)
		Jitter(10*time.Millisecond, 200*time.Millisecond)
	}
	return nil
}

// ConnectionChurn opens short-lived connections to benign hosts, optionally
// completing a TLS handshake with SNI. Hosts must include a port.
func ConnectionChurn(ctx context.Context, hosts []string, count int, withTLS bool) {
	d := net.Dialer{Timeout: 3 * time.Second}
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			return
		}
		h := RandomSNI(hosts)
		if h == "" {
			return
		}
		conn, err := d.DialContext(ctx, "tcp", h)
		if err == nil {
			if withTLS {
				server, _, _ := net.SplitHostPort(h)
				_ = tlsHandshake(conn, server)
			}
			conn.Close()
		}
		Jitter(10*time.Millisecond, 100*time.Millisecond)
	}
}

func tlsHandshake(conn net.Conn, server string) error {
	c := tls.Client(conn, &tls.Config{ServerName: server, InsecureSkipVerify: true})
	return c.Handshake()
}

// BenignRequests sends GETs to shuffled harmless URLs through client. Use a
// client whose transport speaks HTTP/2 when sequencing before an h2 target.
func BenignRequests(ctx context.Context, client *http.Client, urls []string) error {
	if client == nil {
		client = http.DefaultClient
	}
	for _, raw := range RandomOrder(urls) {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
		}
		Jitter(20*time.Millisecond, 150*time.Millisecond)
	}
	return nil
}

// BrowserHeaders returns canonical header pairs in a consistent browser
// order. Use with a writer that preserves order; net/http.Header does not.
func BrowserHeaders(profile string) [][2]string {
	ua := userAgents[0]
	order := []string{"User-Agent", "Accept", "Accept-Language"}
	switch strings.ToLower(profile) {
	case "firefox":
		ua = userAgents[2]
		order = []string{"User-Agent", "Accept", "Accept-Language", "Accept-Encoding"}
	case "safari":
		ua = userAgents[1]
	}
	values := map[string]string{
		"User-Agent":      ua,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9",
		"Accept-Encoding": "gzip, deflate, br",
	}
	out := make([][2]string, 0, len(order))
	for _, k := range order {
		out = append(out, [2]string{k, values[k]})
	}
	return out
}

// CoverConfig controls framed cover traffic.
type CoverConfig struct {
	Interval time.Duration // between cover frames (default 2s)
	MinSize  int           // min cover payload (default 32)
	MaxSize  int           // max cover payload (default 512)
}

// CoverConn wraps a stream with length-prefixed data/cover frames and emits
// random cover frames on idle intervals. Both ends must use CoverConn: cover
// frames are stripped on read, never exposed to the application.
type CoverConn struct {
	net.Conn
	cfg  CoverConfig
	wmu  sync.Mutex
	rmu  sync.Mutex
	rbuf []byte
	done chan struct{}
	once sync.Once
}

// NewCoverConn wraps c and starts the cover frame sender.
func NewCoverConn(c net.Conn, cfg CoverConfig) *CoverConn {
	if cfg.Interval <= 0 {
		cfg.Interval = 2 * time.Second
	}
	if cfg.MinSize < 1 {
		cfg.MinSize = 32
	}
	if cfg.MaxSize < cfg.MinSize {
		cfg.MaxSize = 512
	}
	cc := &CoverConn{Conn: c, cfg: cfg, done: make(chan struct{})}
	go cc.coverLoop()
	return cc
}

const (
	frameData byte = iota
	frameCover
)

func (c *CoverConn) Write(p []byte) (int, error) {
	if err := c.writeFrame(frameData, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *CoverConn) writeFrame(kind byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if len(payload) > 0xffff {
		return errors.New("evade: cover frame too large")
	}
	var head [3]byte
	head[0] = kind
	binary.BigEndian.PutUint16(head[1:], uint16(len(payload)))
	if _, err := c.Conn.Write(head[:]); err != nil {
		return err
	}
	_, err := c.Conn.Write(payload)
	return err
}

func (c *CoverConn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for len(c.rbuf) == 0 {
		var head [3]byte
		if _, err := io.ReadFull(c.Conn, head[:]); err != nil {
			return 0, err
		}
		n := int(binary.BigEndian.Uint16(head[1:]))
		payload := make([]byte, n)
		if _, err := io.ReadFull(c.Conn, payload); err != nil {
			return 0, err
		}
		if head[0] == frameCover {
			continue
		}
		if head[0] != frameData {
			return 0, errors.New("evade: bad cover frame type")
		}
		c.rbuf = payload
	}
	n := copy(p, c.rbuf)
	c.rbuf = c.rbuf[n:]
	return n, nil
}

func (c *CoverConn) coverLoop() {
	t := time.NewTicker(c.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			size := c.cfg.MinSize
			if c.cfg.MaxSize > c.cfg.MinSize {
				r, _ := rand.Int(rand.Reader, big.NewInt(int64(c.cfg.MaxSize-c.cfg.MinSize+1)))
				size += int(r.Int64())
			}
			payload := make([]byte, size)
			_, _ = rand.Read(payload)
			if err := c.writeFrame(frameCover, payload); err != nil {
				return
			}
		}
	}
}

func (c *CoverConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.Conn.Close()
}

// RegionForHost guesses a broad region from a hostname's ccTLD. It is a hint
// only: it says nothing about where a proxy exit actually terminates.
func RegionForHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	parts := strings.Split(strings.ToLower(strings.Trim(strings.TrimSpace(host), ".")), ".")
	if len(parts) < 2 {
		return "unknown"
	}
	switch parts[len(parts)-1] {
	case "us", "ca":
		return "na"
	case "gb", "de", "fr", "nl", "es", "it", "pl", "se", "no", "fi", "dk", "ch", "at", "be", "ie", "pt":
		return "eu"
	case "jp", "kr", "cn", "sg", "hk", "tw", "in", "au", "nz":
		return "apac"
	case "br", "mx", "ar", "cl", "co":
		return "latam"
	case "za", "eg", "ng", "ke":
		return "africa"
	default:
		return "unknown"
	}
}

// FilterByRegion keeps URLs whose hostname ccTLD matches region.
func FilterByRegion(urls []string, region string) []string {
	var out []string
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if RegionForHost(u.Hostname()) == region {
			out = append(out, raw)
		}
	}
	return out
}
