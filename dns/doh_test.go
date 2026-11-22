package dns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func dohServer(t *testing.T, ip string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "example.test" {
			http.Error(w, "bad name", 400)
			return
		}
		w.Header().Set("Content-Type", "application/dns-json")
		fmt.Fprintf(w, `{"Answer":[{"data":%q}]}`, ip)
	}))
}

func TestResolverFallbackAndCache(t *testing.T) {
	bad := dohServer(t, "192.0.2.1")
	defer bad.Close()
	good := dohServer(t, "93.184.216.34")
	defer good.Close()

	calls := 0
	counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"Answer":[{"data":"93.184.216.34"}]}`)
	}))
	defer counting.Close()

	r := NewResolver()
	r.Endpoints = []string{bad.URL + "/dns-query?broken=1", good.URL + "/dns-query"}
	// First endpoint returns a reserved IP that is still a valid answer; make it fail instead.
	r.Endpoints[0] = "http://127.0.0.1:1/dns-query"
	got, err := r.ResolveContext(context.Background(), "example.test")
	if err != nil {
		t.Fatal(err)
	}
	if got != "93.184.216.34" {
		t.Fatalf("got %s", got)
	}

	r.Endpoints = []string{counting.URL + "/dns-query"}
	got, err = r.ResolveContext(context.Background(), "cache.test")
	if err != nil || got != "93.184.216.34" {
		t.Fatalf("resolve: %s %v", got, err)
	}
	_, _ = r.ResolveContext(context.Background(), "cache.test")
	if calls != 1 {
		t.Fatalf("cache missed, calls=%d", calls)
	}
}

func TestResolveIPPassthrough(t *testing.T) {
	r := NewResolver()
	got, err := r.ResolveContext(context.Background(), "8.8.8.8")
	if err != nil || got != "8.8.8.8" {
		t.Fatalf("got %s %v", got, err)
	}
}
