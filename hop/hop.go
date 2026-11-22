package hop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// Dialer is implemented by anything that can open a connection to addr.
type Dialer interface {
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
}

// ContextDialer opens raw TCP connections to proxies and direct targets.
type ContextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// Proxy is a single upstream proxy.
type Proxy struct {
	URL    *url.URL
	Dialer ContextDialer
}

// WithDialer returns a copy of p that uses d for its raw TCP connections.
func (p *Proxy) WithDialer(d ContextDialer) *Proxy {
	q := *p
	q.Dialer = d
	return &q
}

// dialTCP opens a raw connection to the proxy server.
func (p *Proxy) dialTCP(ctx context.Context, network, addr string) (net.Conn, error) {
	d := p.Dialer
	if d == nil {
		d = &net.Dialer{}
	}
	return d.DialContext(ctx, network, addr)
}

// New parses a proxy URL (http, https, socks5, socks5h).
func New(raw string) (*Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		return nil, errors.New("hop: missing scheme")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h", "h2", "h2c", "https2", "http2", "http2c":
	default:
		return nil, fmt.Errorf("hop: unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("hop: missing host")
	}
	return &Proxy{URL: u}, nil
}

// Dial opens a connection to addr through this proxy.
func (p *Proxy) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if p == nil || p.URL == nil {
		return nil, errors.New("hop: nil proxy")
	}
	if addr == "" {
		return nil, errors.New("hop: empty address")
	}
	switch p.URL.Scheme {
	case "http", "https":
		return dialHTTPConnect(ctx, p, network, addr)
	case "socks5", "socks5h":
		return dialSocks5(ctx, p, network, addr)
	case "h2", "h2c", "https2", "http2", "http2c":
		return dialHTTP2Connect(ctx, p, network, addr)
	default:
		return nil, fmt.Errorf("hop: unsupported scheme %q", p.URL.Scheme)
	}
}
