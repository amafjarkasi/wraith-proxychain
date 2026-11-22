package chain

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/wraith/proxychain/hop"
)

// mockProxy is a minimal HTTP CONNECT proxy.
func startMockProxy(t *testing.T) (string, func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
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
				if req.Method != http.MethodConnect {
					io.WriteString(c, "HTTP/1.1 405\r\n\r\n")
					return
				}
				up, err := net.Dial("tcp", req.Host)
				if err != nil {
					fmt.Fprintf(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
					return
				}
				defer up.Close()
				io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestStressMixedScenarios(t *testing.T) {
	// backend echo-ish (just accept and close after read)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()
	backendAddr := backend.Listener.Addr().String()

	// pool of mock proxies
	const N = 12
	proxies := make([]string, 0, N)
	cleanups := make([]func(), 0, N)
	for i := 0; i < N; i++ {
		addr, cl := startMockProxy(t)
		proxies = append(proxies, "http://"+addr)
		cleanups = append(cleanups, cl)
	}
	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()

	strategies := []func(*Chain) Rotator{
		func(c *Chain) Rotator { return NewRandom() },
		func(c *Chain) Rotator { return NewRoundRobin() },
		func(c *Chain) Rotator { return NewWeighted(c) },
		func(c *Chain) Rotator { return NewLatency(c) },
	}

	for _, hopsCount := range []int{1, 2, 3, 5, 8} {
		for si, sf := range strategies {
			t.Run(fmt.Sprintf("hops%d_strat%d", hopsCount, si), func(t *testing.T) {
				hops := make([]*hop.Proxy, 0, hopsCount)
				for i := 0; i < hopsCount && i < len(proxies); i++ {
					p, _ := hop.New(proxies[i])
					hops = append(hops, p)
				}
				// only filtering strategies get a dead hop to exercise removal
				if si >= 2 {
					dead, _ := hop.New("http://127.0.0.1:1")
					hops = append(hops, dead)
				}
				spares := []*hop.Proxy{}
				if sp, err := hop.New(proxies[len(proxies)-1]); err == nil {
					spares = append(spares, sp)
				}

				ch := New(hops,
					WithStrategy(sf(nil)),
					WithRetries(2),
					WithSpareProxies(spares),
					WithPoolSize(4),
					WithJitter(0, 5*time.Millisecond),
				)
				// fix strategy with chain ref
				ch.Strategy = sf(ch)
				defer ch.Stop()

				var wg sync.WaitGroup
				errs := make(chan error, 20)
				for i := 0; i < 20; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						conn, err := ch.Dial(ctx, "tcp", backendAddr)
						if err != nil {
							errs <- err
							return
						}
						conn.Close()
					}()
				}
				wg.Wait()
				close(errs)
				fail := 0
				for e := range errs {
					fail++
					if fail <= 3 {
						t.Logf("err: %v", e)
					}
				}
				t.Logf("hops=%d strat=%d failures=%d/20", hopsCount, si, fail)
				if fail > 10 {
					t.Fatalf("too many failures: %d", fail)
				}
			})
		}
	}
}

func TestStressSingleProxy(t *testing.T) {
	addr, cl := startMockProxy(t)
	defer cl()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer backend.Close()
	p, _ := hop.New("http://" + addr)
	ch := New([]*hop.Proxy{p}, WithRetries(3))
	defer ch.Stop()
	for i := 0; i < 200; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, err := ch.Dial(ctx, "tcp", backend.Listener.Addr().String())
		cancel()
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		conn.Close()
	}
}
