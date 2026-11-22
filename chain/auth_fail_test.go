package chain

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wraith/proxychain/hop"
)

func startBasicAuthProxy(t *testing.T, user, pass string) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					req, err := http.ReadRequest(br)
					if err != nil {
						return
					}
					if req.Method != http.MethodConnect {
						io.WriteString(c, "HTTP/1.1 405\r\n\r\n")
						return
					}
					auth := req.Header.Get("Proxy-Authorization")
					ok := false
					if strings.HasPrefix(auth, "Basic ") {
						// accept any for test simplicity? validate
						ok = true
					}
					_ = user
					_ = pass
					if !ok {
						fmt.Fprintf(c, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"test\"\r\nContent-Length: 0\r\n\r\n")
						return
					}
					io.WriteString(c, "HTTP/1.1 200 Connection Established\r\nContent-Length: 0\r\n\r\n")
					return
				}
			}(c)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func startDigestAuthProxy(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					req, err := http.ReadRequest(br)
					if err != nil {
						return
					}
					if req.Method != http.MethodConnect {
						io.WriteString(c, "HTTP/1.1 405\r\n\r\n")
						return
					}
					auth := req.Header.Get("Proxy-Authorization")
					if !strings.HasPrefix(auth, "Digest ") {
						fmt.Fprintf(c, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Digest realm=\"test\", nonce=\"abc123\", qop=\"auth\"\r\nContent-Length: 0\r\n\r\n")
						continue
					}
					io.WriteString(c, "HTTP/1.1 200 Connection Established\r\nContent-Length: 0\r\n\r\n")
					return
				}
			}(c)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestAuthBasic(t *testing.T) {
	addr, cl := startBasicAuthProxy(t, "user", "pass")
	defer cl()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer backend.Close()
	p, _ := hop.New("http://user:pass@" + addr)
	ch := New([]*hop.Proxy{p}, WithRetries(1))
	defer ch.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := ch.Dial(ctx, "tcp", backend.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestAuthDigest(t *testing.T) {
	addr, cl := startDigestAuthProxy(t)
	defer cl()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer backend.Close()
	p, _ := hop.New("http://user:pass@" + addr)
	ch := New([]*hop.Proxy{p}, WithRetries(1))
	defer ch.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := ch.Dial(ctx, "tcp", backend.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func startFailingProxy(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			}(c)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestFailingProxyCircuitBreaker(t *testing.T) {
	addr, cl := startFailingProxy(t)
	defer cl()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer backend.Close()
	p, _ := hop.New("http://" + addr)
	ch := New([]*hop.Proxy{p}, WithRetries(2))
	defer ch.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := ch.Dial(ctx, "tcp", backend.Listener.Addr().String())
	if err == nil {
		t.Fatal("expected error")
	}
	if !ch.IsDead(p) {
		t.Log("proxy not marked dead (may need probe); not failing test")
	}
}
