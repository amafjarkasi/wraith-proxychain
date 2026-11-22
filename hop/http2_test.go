package hop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestHTTP2Smoke(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			w.WriteHeader(200)
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			buf := make([]byte, 1024)
			for {
				n, err := r.Body.Read(buf)
				if n > 0 {
					w.Write(buf[:n])
					if fl, ok := w.(http.Flusher); ok {
						fl.Flush()
					}
				}
				if err != nil {
					return
				}
			}
		}
		w.WriteHeader(405)
	})
	srv := httptest.NewServer(h2c.NewHandler(handler, &http2.Server{}))
	defer srv.Close()

	p, err := New("h2c://" + srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Dial(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4)
	n, err := c.Read(b)
	fmt.Printf("read %d %q err=%v\n", n, string(b[:n]), err)
	if string(b[:n]) != "ping" {
		t.Fatalf("echo mismatch: %q", string(b[:n]))
	}
}
