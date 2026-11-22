package chain

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"github.com/wraith/proxychain/hop"
)

type countingDialer struct {
	n    int64
	base *net.Dialer
}

func (d *countingDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	atomic.AddInt64(&d.n, 1)
	return d.base.DialContext(ctx, network, addr)
}

func TestWithDialerDirect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
	}()

	d := &countingDialer{base: &net.Dialer{}}
	ch := New(nil, WithDialer(d), WithHealthInterval(0))
	defer ch.Stop()
	conn, err := ch.Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if got := atomic.LoadInt64(&d.n); got < 1 {
		t.Fatal("custom dialer was not used")
	}
}

func TestWithDialerPropagatesToHops(t *testing.T) {
	p, err := hop.New("http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	d := &countingDialer{base: &net.Dialer{}}
	ch := New([]*hop.Proxy{p}, WithDialer(d), WithHealthInterval(0))
	defer ch.Stop()
	if ch.Hops[0].Dialer != d {
		t.Fatal("dialer not propagated to hop")
	}
}
