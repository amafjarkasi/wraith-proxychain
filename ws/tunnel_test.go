package ws

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func echoServer(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Upgrade(w, r, cfg)
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}))
}

func dialWS(t *testing.T, srv *httptest.Server, cfg Config) net.Conn {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	u.Path = "/echo"
	rc, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	c, err := WrapClient(context.Background(), rc, u.String(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTunnelRoundTrip(t *testing.T) {
	srv := echoServer(t, Config{})
	defer srv.Close()
	c := dialWS(t, srv, Config{Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond, Missed: 3})
	defer c.Close()

	msg := []byte("hello websocket")
	if _, err := c.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("got %q", buf)
	}
	time.Sleep(150 * time.Millisecond) // keepalive ticks must not break the conn
	if _, err := c.Write(msg); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
}

func TestKeepaliveClosesOnMissedPong(t *testing.T) {
	srv := echoServer(t, Config{NoPong: true})
	defer srv.Close()
	c := dialWS(t, srv, Config{Interval: 20 * time.Millisecond, Timeout: 50 * time.Millisecond, Missed: 2})
	defer c.Close()

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Fatal("expected keepalive close")
	}
}
