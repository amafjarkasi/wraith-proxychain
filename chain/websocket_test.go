package chain

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/wraith/proxychain/ws"
)

func TestChainWebSocketTunnel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Upgrade(w, r, ws.Config{})
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	ch := New(nil, WithWebSocketTunnel(u.String(), ws.Config{Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond}))
	defer ch.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := ch.Dial(ctx, "tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	msg := []byte("chain-over-websocket")
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
}
