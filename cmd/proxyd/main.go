package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/config"
	"github.com/wraith/proxychain/hop"
	"github.com/wraith/proxychain/proxyproto"
)

type chainHolder struct {
	mu sync.RWMutex
	c  *chain.Chain
}

func (h *chainHolder) get() *chain.Chain {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.c
}

func (h *chainHolder) set(c *chain.Chain) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.c != nil {
		h.c.Stop()
	}
	h.c = c
}

func buildChain(cfg *config.ChainConfig) (*chain.Chain, error) {
	hops := make([]*hop.Proxy, 0, len(cfg.Hops))
	for _, h := range cfg.Hops {
		p, err := hop.New(h.URL)
		if err != nil {
			return nil, err
		}
		hops = append(hops, p)
	}
	opts := []chain.Option{chain.WithStrategy(chain.NewRandom())}
	if cfg.SessionTTL != "" {
		if d, err := time.ParseDuration(cfg.SessionTTL); err == nil {
			opts = append(opts, chain.WithSessionTTL(d))
		}
	}
	ch := chain.New(hops, opts...)
	switch cfg.Strategy {
	case "roundrobin":
		ch.Strategy = chain.NewRoundRobin()
	case "weighted":
		ch.Strategy = chain.NewWeighted(ch)
	case "latency":
		ch.Strategy = chain.NewLatency(ch)
	}
	return ch, nil
}

func main() {
	addr := flag.String("addr", ":8080", "listen addr")
	configPath := flag.String("config", "", "config yaml")
	proxyProto := flag.Bool("proxyproto", false, "require HAProxy PROXY protocol v2 on inbound connections")
	flag.Parse()

	var cfg *config.ChainConfig
	var err error
	if *configPath != "" {
		cfg, err = config.Load(*configPath)
		if err != nil {
			fmt.Println("config error:", err)
			return
		}
	} else {
		cfg = config.FromEnv()
	}
	if cfg == nil {
		fmt.Println("no config")
		return
	}
	ch, err := buildChain(cfg)
	if err != nil {
		fmt.Println(err)
		return
	}
	holder := &chainHolder{c: ch}

	if *configPath != "" {
		stop := make(chan struct{})
		defer close(stop)
		if err := config.Watch(*configPath, func(c *config.ChainConfig) {
			nc, err := buildChain(c)
			if err != nil {
				fmt.Println("reload error:", err)
				return
			}
			holder.set(nc)
			fmt.Println("config reloaded")
		}, stop); err != nil {
			fmt.Println("watch error:", err)
		}
	}

	srv := &http.Server{Addr: *addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "only CONNECT", http.StatusMethodNotAllowed)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		ch := holder.get()
		up, err := ch.Dial(context.Background(), "tcp", r.Host)
		if err != nil {
			fmt.Fprintf(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		defer up.Close()
		fmt.Fprintf(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { ioCopy(conn, up) }()
		ioCopy(up, conn)
	})}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Println("listen error:", err)
		return
	}
	defer ln.Close()
	var listener net.Listener = ln
	if *proxyProto {
		listener = proxyproto.NewListener(ln, 5*time.Second)
		fmt.Println("PROXY protocol v2 enabled")
	}
	fmt.Println("proxyd listening", *addr)
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Println("server error:", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	holder.get().Stop()
}

func ioCopy(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
