package chain

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/wraith/proxychain/hop"
)

func startEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(c)
		}
	}()
	return ln.Addr().String()
}

func startConnectProxy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				target := req.URL.Host
				if target == "" {
					target = req.Host
				}
				c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				bc, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer bc.Close()
				go io.Copy(bc, c)
				io.Copy(c, bc)
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestTwoHopChain(t *testing.T) {
	backend := startEcho(t)
	proxy2 := startConnectProxy(t)
	proxy1 := startConnectProxy(t)

	p1, err := hop.New("http://" + proxy1)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := hop.New("http://" + proxy2)
	if err != nil {
		t.Fatal(err)
	}
	c := New([]*hop.Proxy{p1, p2}, WithStrategy(&RoundRobin{}))
	conn, err := c.Dial(context.Background(), "tcp", backend)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4)
	if _, err := io.ReadFull(conn, b); err != nil {
		t.Fatal(err)
	}
	if string(b) != "ping" {
		t.Fatalf("got %q", string(b))
	}
}

func TestRoundRobinDeterminism(t *testing.T) {
	hops := make([]*hop.Proxy, 2)
	for i := range hops {
		p, _ := hop.New("http://127.0.0.1:8080")
		hops[i] = p
	}
	rr := &RoundRobin{}
	o1 := rr.Order(hops)
	o2 := rr.Order(hops)
	if o1[0] != hops[0] || o2[0] != hops[1] {
		t.Fatalf("unexpected rotation: %v %v", o1, o2)
	}
}
