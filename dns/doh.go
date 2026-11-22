package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"sync"
	"time"
)

// defaultEndpoints are tried in order; the first successful answer wins.
var defaultEndpoints = []string{
	"https://cloudflare-dns.com/dns-query",
	"https://dns.google/dns-query",
}

type cacheEntry struct {
	ip  string
	exp time.Time
}

// Resolver resolves A records over DNS-over-HTTPS with endpoint fallback and
// a short positive cache.
type Resolver struct {
	Endpoints []string
	Client    *http.Client

	mu    sync.RWMutex
	cache map[string]cacheEntry
	ttl   time.Duration
}

// NewResolver returns a resolver using the default public DoH endpoints.
func NewResolver() *Resolver {
	return &Resolver{
		Endpoints: append([]string(nil), defaultEndpoints...),
		Client:    &http.Client{Timeout: 5 * time.Second},
		cache:     make(map[string]cacheEntry),
		ttl:       time.Minute,
	}
}

func (r *Resolver) endpoints() []string {
	if len(r.Endpoints) > 0 {
		return r.Endpoints
	}
	return defaultEndpoints
}

func (r *Resolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 5 * time.Second}
}

// ResolveContext resolves host, trying each configured endpoint in order.
func (r *Resolver) ResolveContext(ctx context.Context, host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return host, nil
	}
	r.mu.RLock()
	e, ok := r.cache[host]
	r.mu.RUnlock()
	if ok && time.Now().Before(e.exp) {
		return e.ip, nil
	}

	var lastErr error
	for _, endpoint := range r.endpoints() {
		ip, err := r.query(ctx, endpoint, host)
		if err != nil {
			lastErr = err
			continue
		}
		r.mu.Lock()
		if r.cache == nil {
			r.cache = make(map[string]cacheEntry)
		}
		ttl := r.ttl
		if ttl <= 0 {
			ttl = time.Minute
		}
		r.cache[host] = cacheEntry{ip: ip, exp: time.Now().Add(ttl)}
		r.mu.Unlock()
		return ip, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("dns: no endpoints")
	}
	return "", lastErr
}

func (r *Resolver) query(ctx context.Context, endpoint, host string) (string, error) {
	u := endpoint + "?name=" + neturl.QueryEscape(host) + "&type=A"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := r.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dns: status %s", resp.Status)
	}
	var out struct {
		Answer []struct {
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	for _, a := range out.Answer {
		if net.ParseIP(a.Data) != nil {
			return a.Data, nil
		}
	}
	return "", fmt.Errorf("dns: no answer from %s", endpoint)
}

var defaultResolver = NewResolver()

// Resolve resolves host via the default resolver.
func Resolve(host string) (string, error) {
	return ResolveContext(context.Background(), host)
}

// ResolveContext resolves host via the default resolver and respects ctx.
func ResolveContext(ctx context.Context, host string) (string, error) {
	return defaultResolver.ResolveContext(ctx, host)
}

// Dialer is a ContextDialer that resolves via DoH before dialing.
type Dialer struct {
	Resolver *Resolver
}

// NewDialer returns a dialer that resolves via DoH.
func NewDialer() *Dialer { return &Dialer{Resolver: defaultResolver} }

// DialContext resolves via DoH then dials.
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, "80"
	}
	r := d.Resolver
	if r == nil {
		r = defaultResolver
	}
	ip, err := r.ResolveContext(ctx, host)
	if err != nil {
		ip = host
	}
	nd := &net.Dialer{}
	return nd.DialContext(ctx, network, net.JoinHostPort(ip, port))
}

// DialContext resolves via DoH then dials, using the default resolver.
func DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return NewDialer().DialContext(ctx, network, addr)
}
