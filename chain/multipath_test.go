package chain

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

type fakePath struct {
	id           string
	delay        time.Duration
	err          error
	ignoreCancel bool
	calls        int
	stop         int
	lastConn     *idConn
	mu           sync.Mutex
}

func (f *fakePath) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.delay > 0 {
		if f.ignoreCancel {
			time.Sleep(f.delay)
		} else {
			select {
			case <-time.After(f.delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	c := &idConn{id: f.id}
	f.mu.Lock()
	f.lastConn = c
	f.mu.Unlock()
	return c, nil
}

func (f *fakePath) Stop() {
	f.mu.Lock()
	f.stop++
	f.mu.Unlock()
}

func (f *fakePath) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type idConn struct {
	id     string
	mu     sync.Mutex
	closed bool
}

func (c *idConn) Read([]byte) (int, error)    { return 0, errors.New("unused") }
func (c *idConn) Write(b []byte) (int, error) { return len(b), nil }
func (c *idConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}
func (c *idConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}
func (c *idConn) LocalAddr() net.Addr              { return nil }
func (c *idConn) RemoteAddr() net.Addr             { return nil }
func (c *idConn) SetDeadline(time.Time) error      { return nil }
func (c *idConn) SetReadDeadline(time.Time) error  { return nil }
func (c *idConn) SetWriteDeadline(time.Time) error { return nil }

func TestMultipathFailover(t *testing.T) {
	bad := &fakePath{id: "bad", err: errors.New("down")}
	good := &fakePath{id: "good"}
	m := NewMultiChain([]PathDialer{bad, good}, MultipathFailover)
	conn, err := m.Dial(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatal(err)
	}
	if conn.(*idConn).id != "good" {
		t.Fatalf("got path %q, want good", conn.(*idConn).id)
	}
	if bad.callCount() != 1 || good.callCount() != 1 {
		t.Fatalf("calls %d/%d", bad.callCount(), good.callCount())
	}
}

func TestMultipathStripe(t *testing.T) {
	a := &fakePath{id: "a"}
	b := &fakePath{id: "b"}
	m := NewMultiChain([]PathDialer{a, b}, MultipathStripe)
	want := []string{"a", "b", "a"}
	for i, w := range want {
		conn, err := m.Dial(context.Background(), "tcp", "example.com:80")
		if err != nil {
			t.Fatal(err)
		}
		if got := conn.(*idConn).id; got != w {
			t.Fatalf("dial %d: got %q, want %q", i, got, w)
		}
	}
}

func TestMultipathFastest(t *testing.T) {
	slow := &fakePath{id: "slow", delay: 30 * time.Millisecond}
	fast := &fakePath{id: "fast", delay: time.Millisecond}
	m := NewMultiChain([]PathDialer{slow, fast}, MultipathFastest)
	m.Dial(context.Background(), "tcp", "example.com:80") // train stats
	conn, err := m.Dial(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatal(err)
	}
	if conn.(*idConn).id != "fast" {
		t.Fatalf("got %q, want fast", conn.(*idConn).id)
	}
}

func TestMultipathRace(t *testing.T) {
	slow := &fakePath{id: "slow", delay: 30 * time.Millisecond, ignoreCancel: true}
	fast := &fakePath{id: "fast", delay: 5 * time.Millisecond}
	m := NewMultiChain([]PathDialer{slow, fast}, MultipathRace)
	conn, err := m.Dial(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatal(err)
	}
	got := conn.(*idConn)
	if got.id != "fast" {
		t.Fatalf("got %q, want fast", got.id)
	}
	// Dial waits for all results, so the losing conn must be closed.
	slow.mu.Lock()
	loser := slow.lastConn
	slow.mu.Unlock()
	if loser == nil || !loser.isClosed() {
		t.Fatalf("losing connection was not closed")
	}
}

func TestMultipathStop(t *testing.T) {
	a := &fakePath{id: "a"}
	b := &fakePath{id: "b"}
	m := NewMultiChain([]PathDialer{a, b}, MultipathFailover)
	m.Stop()
	if a.stop != 1 || b.stop != 1 {
		t.Fatalf("stop counts %d/%d", a.stop, b.stop)
	}
}
