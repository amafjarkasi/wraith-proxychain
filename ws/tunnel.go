// Package ws exposes a WebSocket connection as a net.Conn with ping/pong keepalive.
package ws

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Config controls keepalive behaviour. Zero values use defaults.
type Config struct {
	Interval    time.Duration // ping interval (default 30s)
	Timeout     time.Duration // pong timeout (default 10s)
	Missed      int           // consecutive missed pongs before close (default 3)
	NoKeepalive bool          // disable client keepalive
	NoPong      bool          // server: ignore pings instead of replying (tests)
}

type conn struct {
	ws   *websocket.Conn
	cfg  Config
	rbuf []byte

	rmu sync.Mutex
	wmu sync.Mutex

	readDeadline  time.Time
	writeDeadline time.Time

	pongs chan struct{}
	done  chan struct{}
	once  sync.Once
}

func defaults(c Config) Config {
	if c.Interval <= 0 {
		c.Interval = 30 * time.Second
	}
	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Second
	}
	if c.Missed <= 0 {
		c.Missed = 3
	}
	return c
}

// WrapClient upgrades an existing connection to a WebSocket client using url.
// The returned net.Conn carries binary frames and starts a keepalive loop.
func WrapClient(ctx context.Context, rawConn net.Conn, rawURL string, cfg Config) (net.Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, errors.New("ws: URL scheme must be ws or wss")
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = rawConn.SetDeadline(dl)
		defer rawConn.SetDeadline(time.Time{})
	}
	c, resp, err := websocket.NewClient(rawConn, u, nil, 4096, 4096)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	return newConn(c, cfg, true), nil
}

// Upgrade upgrades an HTTP request and returns a keepalive-aware net.Conn.
func Upgrade(w http.ResponseWriter, r *http.Request, cfg Config) (net.Conn, error) {
	up := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     func(*http.Request) bool { return true },
	}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}
	return newConn(c, cfg, false), nil
}

func newConn(c *websocket.Conn, cfg Config, ping bool) net.Conn {
	cfg = defaults(cfg)
	nc := &conn{
		ws:    c,
		cfg:   cfg,
		pongs: make(chan struct{}, 1),
		done:  make(chan struct{}),
	}
	c.SetPongHandler(func(string) error {
		select {
		case nc.pongs <- struct{}{}:
		default:
		}
		return nil
	})
	if !ping && cfg.NoPong {
		c.SetPingHandler(func(string) error { return nil })
	}
	if ping && !cfg.NoKeepalive {
		go nc.keepalive()
	}
	return nc
}

func (c *conn) keepalive() {
	t := time.NewTicker(c.cfg.Interval)
	defer t.Stop()
	missed := 0
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			c.wmu.Lock()
			err := c.ws.WriteControl(websocket.PingMessage, []byte("wraith"), time.Now().Add(c.cfg.Timeout))
			c.wmu.Unlock()
			if err != nil {
				c.Close()
				return
			}
			select {
			case <-c.pongs:
				missed = 0
			case <-time.After(c.cfg.Timeout):
				missed++
				if missed >= c.cfg.Missed {
					c.Close()
					return
				}
			case <-c.done:
				return
			}
		}
	}
}

func (c *conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	if !c.readDeadline.IsZero() {
		_ = c.ws.SetReadDeadline(c.readDeadline)
	}
	for len(c.rbuf) == 0 {
		mt, b, err := c.ws.ReadMessage()
		if err != nil {
			return 0, err
		}
		if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
			continue
		}
		c.rbuf = b
	}
	n := copy(p, c.rbuf)
	c.rbuf = c.rbuf[n:]
	return n, nil
}

func (c *conn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if !c.writeDeadline.IsZero() {
		_ = c.ws.SetWriteDeadline(c.writeDeadline)
	}
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *conn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.ws.Close()
}

func (c *conn) LocalAddr() net.Addr  { return c.ws.UnderlyingConn().LocalAddr() }
func (c *conn) RemoteAddr() net.Addr { return c.ws.UnderlyingConn().RemoteAddr() }

func (c *conn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	return c.SetWriteDeadline(t)
}

func (c *conn) SetReadDeadline(t time.Time) error {
	c.rmu.Lock()
	c.readDeadline = t
	c.rmu.Unlock()
	return c.ws.SetReadDeadline(t)
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	c.wmu.Lock()
	c.writeDeadline = t
	c.wmu.Unlock()
	return c.ws.SetWriteDeadline(t)
}

var _ net.Conn = (*conn)(nil)
