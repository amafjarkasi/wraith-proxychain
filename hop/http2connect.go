package hop

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/http2"
)

// http2Conn adapts an HTTP/2 CONNECT stream to net.Conn.
type http2Conn struct {
	conn net.Conn
	r    io.Reader
	w    io.WriteCloser
}

func (c *http2Conn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c *http2Conn) Write(p []byte) (int, error) { return c.w.Write(p) }
func (c *http2Conn) Close() error {
	if c.w != nil {
		_ = c.w.Close()
	}
	if rc, ok := c.r.(io.Closer); ok {
		_ = rc.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
func (c *http2Conn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *http2Conn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *http2Conn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *http2Conn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *http2Conn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

func isHTTP2Scheme(s string) bool {
	switch s {
	case "h2", "https2", "http2", "h2c", "http2c":
		return true
	}
	return false
}

func http2TLS(s string) bool {
	switch s {
	case "h2", "https2", "http2":
		return true
	}
	return false
}

func dialHTTP2Connect(ctx context.Context, p *Proxy, network, addr string) (net.Conn, error) {
	if p == nil || p.URL == nil {
		return nil, fmt.Errorf("hop: nil proxy")
	}
	proxyAddr := p.Address()
	host := p.URL.Hostname()

	rawConn, err := p.dialTCP(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	if http2TLS(p.URL.Scheme) {
		cfg := &tls.Config{NextProtos: []string{"h2"}, ServerName: host}
		tconn := tls.Client(rawConn, cfg)
		if err := tconn.HandshakeContext(ctx); err != nil {
			rawConn.Close()
			return nil, err
		}
		rawConn = tconn
	}

	t := &http2.Transport{}
	h2cc, err := t.NewClientConn(rawConn)
	if err != nil {
		rawConn.Close()
		return nil, err
	}

	req := &http.Request{
		Method:     http.MethodConnect,
		URL:        &url.URL{Opaque: addr},
		Host:       addr,
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		ProtoMinor: 0,
		Header:     make(http.Header),
	}
	if u := p.URL.User; u != nil {
		pass, _ := u.Password()
		cred := u.Username() + ":" + pass
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cred)))
	}
	pr, pw := io.Pipe()
	req.Body = pr
	req = req.WithContext(ctx)

	resp, err := h2cc.RoundTrip(req)
	if err != nil {
		rawConn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		rawConn.Close()
		return nil, fmt.Errorf("hop: proxy connect %s", resp.Status)
	}
	return &http2Conn{conn: rawConn, r: resp.Body, w: pw}, nil
}

func connectOverHTTP2(conn net.Conn, target string, p *Proxy) (net.Conn, error) {
	if conn == nil {
		return nil, fmt.Errorf("hop: nil conn")
	}
	if p == nil || p.URL == nil {
		return nil, fmt.Errorf("hop: nil proxy")
	}
	if target == "" {
		return nil, fmt.Errorf("hop: empty target")
	}
	host := p.URL.Hostname()

	var rawConn net.Conn
	var err error
	if http2TLS(p.URL.Scheme) {
		cfg := &tls.Config{NextProtos: []string{"h2"}, ServerName: host}
		tlsConn := tls.Client(conn, cfg)
		if err := tlsConn.Handshake(); err != nil {
			return nil, err
		}
		rawConn = tlsConn
	} else {
		rawConn = conn
	}

	t := &http2.Transport{}
	h2cc, err := t.NewClientConn(rawConn)
	if err != nil {
		return nil, err
	}

	req := &http.Request{
		Method:     http.MethodConnect,
		URL:        &url.URL{Opaque: target},
		Host:       target,
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		ProtoMinor: 0,
		Header:     make(http.Header),
	}
	if u := p.URL.User; u != nil {
		pass, _ := u.Password()
		cred := u.Username() + ":" + pass
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cred)))
	}
	pr, pw := io.Pipe()
	req.Body = pr

	resp, err := h2cc.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("hop: proxy connect %s", resp.Status)
	}
	return &http2Conn{conn: rawConn, r: resp.Body, w: pw}, nil
}
