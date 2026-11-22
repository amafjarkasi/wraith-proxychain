package hop

import (
	"context"
	"io"
	"net"
	"testing"
)

func TestHTTPConnect(t *testing.T) {
	// echo backend
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
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

	// connect proxy
	pl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Close()
	go func() {
		for {
			c, err := pl.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				n, _ := c.Read(buf)
				// expect CONNECT
				c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				// bridge
				bc, err := net.Dial("tcp", ln.Addr().String())
				if err != nil {
					return
				}
				defer bc.Close()
				go io.Copy(bc, c)
				io.Copy(c, bc)
				_ = n
			}(c)
		}
	}()

	p, err := New("http://" + pl.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := p.Dial(context.Background(), "tcp", ln.Addr().String())
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
		t.Fatalf("got %q", b)
	}
}
