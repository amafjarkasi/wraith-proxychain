package chain

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wraith/proxychain/hop"
)

type benchEnv struct {
	chain       *Chain
	backendAddr string
	cleanup     func()
}

func newBenchEnv(b *testing.B, hopsCount int, strat func(*Chain) Rotator) benchEnv {
	b.Helper()
	proxies := make([]string, 0, hopsCount+2)
	cleanups := make([]func(), 0, hopsCount+2)
	for i := 0; i < hopsCount+2; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			b.Fatal(err)
		}
		go func(ln net.Listener) {
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
		}(ln)
		proxies = append(proxies, "http://"+ln.Addr().String())
		cleanups = append(cleanups, func() { ln.Close() })
	}
	hops := make([]*hop.Proxy, 0, hopsCount)
	for i := 0; i < hopsCount; i++ {
		p, _ := hop.New(proxies[i])
		hops = append(hops, p)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	backendAddr := backend.Listener.Addr().String()
	cleanups = append(cleanups, func() { backend.Close() })

	ch := New(hops, WithStrategy(NewRandom()), WithRetries(1))
	if strat != nil {
		ch.Strategy = strat(ch)
	}
	cleanup := func() {
		ch.Stop()
		for _, c := range cleanups {
			c()
		}
	}
	return benchEnv{chain: ch, backendAddr: backendAddr, cleanup: cleanup}
}

func BenchmarkDial_SingleHop(b *testing.B) {
	env := newBenchEnv(b, 1, nil)
	defer env.cleanup()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := env.chain.Dial(ctx, "tcp", env.backendAddr)
		if err != nil {
			b.Fatal(err)
		}
		conn.Close()
	}
}

func BenchmarkDial_ThreeHops(b *testing.B) {
	env := newBenchEnv(b, 3, nil)
	defer env.cleanup()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := env.chain.Dial(ctx, "tcp", env.backendAddr)
		if err != nil {
			b.Fatal(err)
		}
		conn.Close()
	}
}

func BenchmarkDial_FiveHops(b *testing.B) {
	env := newBenchEnv(b, 5, nil)
	defer env.cleanup()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := env.chain.Dial(ctx, "tcp", env.backendAddr)
		if err != nil {
			b.Fatal(err)
		}
		conn.Close()
	}
}

func BenchmarkDial_RoundRobin(b *testing.B) {
	env := newBenchEnv(b, 3, func(c *Chain) Rotator { return NewRoundRobin() })
	defer env.cleanup()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := env.chain.Dial(ctx, "tcp", env.backendAddr)
		if err != nil {
			b.Fatal(err)
		}
		conn.Close()
	}
}

func BenchmarkDial_Weighted(b *testing.B) {
	env := newBenchEnv(b, 3, func(c *Chain) Rotator { return NewWeighted(c) })
	defer env.cleanup()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := env.chain.Dial(ctx, "tcp", env.backendAddr)
		if err != nil {
			b.Fatal(err)
		}
		conn.Close()
	}
}

func BenchmarkDial_Latency(b *testing.B) {
	env := newBenchEnv(b, 3, func(c *Chain) Rotator { return NewLatency(c) })
	defer env.cleanup()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := env.chain.Dial(ctx, "tcp", env.backendAddr)
		if err != nil {
			b.Fatal(err)
		}
		conn.Close()
	}
}

func BenchmarkDial_Pooled(b *testing.B) {
	env := newBenchEnv(b, 3, nil)
	env.chain.poolSize = 8
	b.ResetTimer()
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		conn, err := env.chain.Dial(ctx, "tcp", env.backendAddr)
		if err != nil {
			b.Fatal(err)
		}
		conn.Close()
	}
	b.StopTimer()
	env.cleanup()
}

func BenchmarkDial_Parallel(b *testing.B) {
	env := newBenchEnv(b, 3, nil)
	defer env.cleanup()
	ctx := context.Background()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := env.chain.Dial(ctx, "tcp", env.backendAddr)
			if err != nil {
				b.Error(err)
				continue
			}
			conn.Close()
		}
	})
}
