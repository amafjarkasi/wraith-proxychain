package evade

import (
	"net"
	"testing"
	"time"
)

func TestHoneypotChecker(t *testing.T) {
	c := NewHoneypotChecker()
	if c.Len() < 10 {
		t.Fatalf("too few ranges: %d", c.Len())
	}
	for _, ip := range []string{"192.0.2.1", "198.51.100.7", "203.0.113.9", "2001:db8::1", "127.0.0.1"} {
		if !c.Contains(ip) {
			t.Fatalf("expected %s to be flagged", ip)
		}
	}
	if c.Contains("8.8.8.8") {
		t.Fatal("public IP flagged")
	}
	if err := HoneypotCheckerDefault().AddCIDR("45.13.0.0/16"); err != nil {
		t.Fatal(err)
	}
	if !HoneypotCheckerDefault().Contains("45.13.4.2") {
		t.Fatal("custom range not applied to default checker")
	}
	if err := c.AddCIDR("not-a-cidr"); err == nil {
		t.Fatal("bad CIDR accepted")
	}
}

type deadlineConn struct {
	net.Conn
	deadline time.Time
}

func (d *deadlineConn) SetReadDeadline(t time.Time) error {
	d.deadline = t
	return d.Conn.SetReadDeadline(t)
}

func TestHoneypotProbeTimeout(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	wrapped := &deadlineConn{Conn: c1}
	go func() {
		buf := make([]byte, 256)
		_, _ = c2.Read(buf)
	}()
	if !HoneypotProbeTimeout(wrapped, "TOKEN", 100*time.Millisecond) {
		t.Fatal("expected timeout to flag probe")
	}
	if wrapped.deadline.IsZero() {
		t.Fatal("deadline not set")
	}
}
