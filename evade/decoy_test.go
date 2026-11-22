package evade

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeResolver struct {
	mu    sync.Mutex
	hosts []string
}

func (r *fakeResolver) ResolveContext(_ context.Context, host string) (string, error) {
	r.mu.Lock()
	r.hosts = append(r.hosts, host)
	r.mu.Unlock()
	return "", context.DeadlineExceeded
}

func TestDecoyDNS(t *testing.T) {
	r := &fakeResolver{}
	domains := []string{"a.example", "b.example", "c.example"}
	if err := DecoyDNS(context.Background(), r, domains); err != nil {
		t.Fatal(err)
	}
	if len(r.hosts) != len(domains) {
		t.Fatalf("resolved %d domains, want %d", len(r.hosts), len(domains))
	}
	for _, d := range domains {
		found := false
		for _, got := range r.hosts {
			if got == d {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s", d)
		}
	}
}

func TestBrowserHeadersOrder(t *testing.T) {
	h := BrowserHeaders("firefox")
	if len(h) != 4 || h[0][0] != "User-Agent" || h[3][0] != "Accept-Encoding" {
		t.Fatalf("bad profile: %v", h)
	}
}

func TestRegion(t *testing.T) {
	if got := RegionForHost("proxy.example.de:8080"); got != "eu" {
		t.Fatalf("region = %q", got)
	}
	urls := []string{"http://a.example.de", "http://a.example.com", "http://a.example.jp"}
	got := FilterByRegion(urls, "eu")
	if !reflect.DeepEqual(got, []string{"http://a.example.de"}) {
		t.Fatalf("filtered = %v", got)
	}
}

func TestConnectionChurn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for i := 0; i < 3; i++ {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	ConnectionChurn(context.Background(), []string{ln.Addr().String()}, 3, false)
}

func TestBenignRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	if err := BenignRequests(context.Background(), srv.Client(), []string{srv.URL + "/a", srv.URL + "/b"}); err != nil {
		t.Fatal(err)
	}
}

func TestTimingConn(t *testing.T) {
	a, b := net.Pipe()
	tc := NewTimingConn(a, 5*time.Millisecond, 10*time.Millisecond)
	defer tc.Close()
	defer b.Close()
	go io.Copy(io.Discard, b)
	start := time.Now()
	if _, err := tc.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 5*time.Millisecond {
		t.Fatalf("write returned too early: %s", d)
	}
}

func TestCoverConn(t *testing.T) {
	client, peer := net.Pipe()
	cc := NewCoverConn(client, CoverConfig{Interval: 10 * time.Millisecond, MinSize: 4, MaxSize: 8})
	sc := NewCoverConn(peer, CoverConfig{Interval: 15 * time.Millisecond, MinSize: 4, MaxSize: 8})
	defer cc.Close()
	defer sc.Close()
	go io.Copy(sc, sc)

	if err := cc.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	msg := []byte("ping")
	if _, err := cc.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(cc, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("got %q", buf)
	}
}
