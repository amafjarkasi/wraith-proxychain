package hop

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
)

type countingDialer struct {
	n    int64
	base *net.Dialer
}

func (d *countingDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	atomic.AddInt64(&d.n, 1)
	return d.base.DialContext(ctx, network, addr)
}

func TestProxyWithDialer(t *testing.T) {
	pl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Close()
	go func() {
		c, err := pl.Accept()
		if err == nil {
			defer c.Close()
			buf := make([]byte, 256)
			c.Read(buf)
			c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		}
	}()

	p, err := New("http://" + pl.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	d := &countingDialer{base: &net.Dialer{}}
	p = p.WithDialer(d)
	conn, err := p.Dial(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if got := atomic.LoadInt64(&d.n); got != 1 {
		t.Fatalf("custom dialer called %d times, want 1", got)
	}
}
