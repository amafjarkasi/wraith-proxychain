// Command proxyproto shows a PROXY v2 header being written by a client and
// consumed by a wrapped listener that overrides the observed peer address.
package main

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/wraith/proxychain/proxyproto"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer ln.Close()

	wrapped := proxyproto.NewListener(ln, 2*time.Second)
	defer wrapped.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, err := wrapped.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		fmt.Println("server sees client:", conn.RemoteAddr())
		fmt.Println("server sees destination:", conn.LocalAddr())
		_, _ = io.WriteString(conn, "ok")
		errCh <- nil
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	src := &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 12345}
	dst := ln.Addr().(*net.TCPAddr)
	if err := proxyproto.WriteHeader(conn, src, dst); err != nil {
		panic(err)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		panic(err)
	}
	fmt.Println("client received:", string(buf))
	if err := <-errCh; err != nil {
		fmt.Println("server error:", err)
	}
}
