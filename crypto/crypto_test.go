package crypto

import (
	"net"
	"testing"
)

func TestTunnelRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	var ta, tb net.Conn
	var errA, errB error
	done := make(chan struct{})
	go func() { ta, errA = Wrap(a, true); done <- struct{}{} }()
	tb, errB = Wrap(b, false)
	<-done
	if errA != nil {
		t.Fatal(errA)
	}
	if errB != nil {
		t.Fatal(errB)
	}
	msg := []byte("hello proxychain")
	go func() { ta.Write(msg) }()
	buf := make([]byte, 64)
	n, err := tb.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != string(msg) {
		t.Fatalf("got %q want %q", buf[:n], msg)
	}
	go func() { tb.Write([]byte("pong")) }()
	buf2 := make([]byte, 64)
	n2, err := ta.Read(buf2)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf2[:n2]) != "pong" {
		t.Fatalf("got %q", buf2[:n2])
	}
}
