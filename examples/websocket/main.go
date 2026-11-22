// Command websocket demonstrates a WebSocket tunnel with ping/pong keepalive.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/ws"
)

func main() {
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
	ch := chain.New(nil, chain.WithWebSocketTunnel(u.String(), ws.Config{
		Interval: 5 * time.Second,
		Timeout:  2 * time.Second,
		Missed:   3,
	}))
	defer ch.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := ch.Dial(ctx, "tcp", u.Host)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	msg := []byte("hello over websocket")
	if _, err := conn.Write(msg); err != nil {
		panic(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		panic(err)
	}
	fmt.Println(string(buf))
}
