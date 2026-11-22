package metrics

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetrics(t *testing.T) {
	m := &Metrics{}
	m.RecordDial(0, nil)
	m.AddBytes(10, 20)
	s := m.Snapshot()
	if s["dials"].(int64) != 1 {
		t.Fatal("dials")
	}
	if s["bytes_in"].(int64) != 10 {
		t.Fatal("bytes")
	}
}

func TestPerHopMetrics(t *testing.T) {
	m := &Metrics{}
	m.RecordHop("http://proxy.example:8080", 5*time.Millisecond, nil)
	m.RecordHop("http://proxy.example:8080", 0, errors.New("dead"))
	m.RecordHop("socks5://backup.example:1080", time.Millisecond, nil)
	s := m.Snapshot()
	hops, ok := s["hops"].(map[string]any)
	if !ok {
		t.Fatal("missing hops")
	}
	first := hops["http://proxy.example:8080"].(map[string]any)
	if first["dials"].(int64) != 2 || first["failures"].(int64) != 1 {
		t.Fatalf("first hop: %v", first)
	}
	if first["last_error"] != "dead" {
		t.Fatalf("last_error: %v", first["last_error"])
	}
	if _, ok := hops["socks5://backup.example:1080"]; !ok {
		t.Fatal("second hop missing")
	}
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "hop{http://proxy.example:8080} dials=2 failures=1") {
		t.Fatalf("handler: %s", rec.Body.String())
	}
}
