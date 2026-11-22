package proxyproto

import (
	"bytes"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestRoundTripIPv4(t *testing.T) {
	src := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 12345}
	dst := &net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: 443}
	var buf bytes.Buffer
	if err := WriteHeader(&buf, src, dst); err != nil {
		t.Fatal(err)
	}
	gotSrc, gotDst, err := ReadHeader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotSrc.String(), src.String()) || !reflect.DeepEqual(gotDst.String(), dst.String()) {
		t.Fatalf("got %v -> %v", gotSrc, gotDst)
	}
}

func TestRoundTripIPv6(t *testing.T) {
	src := &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 1000}
	dst := &net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 2000}
	var buf bytes.Buffer
	if err := WriteHeader(&buf, src, dst); err != nil {
		t.Fatal(err)
	}
	gotSrc, gotDst, err := ReadHeader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if gotSrc.String() != src.String() || gotDst.String() != dst.String() {
		t.Fatalf("got %v -> %v", gotSrc, gotDst)
	}
}

func TestUnknownHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHeader(&buf, &net.UnixAddr{Name: "x", Net: "unix"}, nil); err != nil {
		t.Fatal(err)
	}
	src, dst, err := ReadHeader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if src != nil || dst != nil {
		t.Fatal("expected no addresses")
	}
}

func TestBadSignature(t *testing.T) {
	_, _, err := ReadHeader(bytes.NewReader([]byte("GET / HTTP/1.1\r\n\r\n")))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListenerOverridesAddr(t *testing.T) {
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		src := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 9999}
		dst := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
		if err := WriteHeader(client, src, dst); err != nil {
			t.Error(err)
		}
	}()

	l := &pipeListener{conn: server}
	wrapped := NewListener(l, time.Second)
	conn, err := wrapped.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := conn.RemoteAddr().String(); got != "203.0.113.7:9999" {
		t.Fatalf("remote = %s", got)
	}
	<-done
	client.Close()
}

type pipeListener struct {
	conn net.Conn
	used bool
}

func (l *pipeListener) Accept() (net.Conn, error) {
	if l.used {
		return nil, net.ErrClosed
	}
	l.used = true
	return l.conn, nil
}

func (l *pipeListener) Close() error   { return nil }
func (l *pipeListener) Addr() net.Addr { return l.conn.LocalAddr() }
