package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/config"
	"github.com/wraith/proxychain/hop"
)

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
	if cfg.JitterMin != "" || cfg.JitterMax != "" {
		mn, _ := time.ParseDuration(cfg.JitterMin)
		mx, _ := time.ParseDuration(cfg.JitterMax)
		opts = append(opts, chain.WithJitter(mn, mx))
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
	configPath := flag.String("config", "", "config yaml path")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Println("usage: proxychain <ping|list> [args]")
		return
	}
	var cfg *config.ChainConfig
	if *configPath != "" {
		c, err := config.Load(*configPath)
		if err != nil {
			fmt.Println("config error:", err)
			return
		}
		cfg = c
	} else {
		cfg = config.FromEnv()
	}
	if cfg == nil {
		fmt.Println("no config; set PROXY_CHAIN or -config")
		return
	}
	switch args[0] {
	case "list":
		for i, h := range cfg.Hops {
			fmt.Printf("%d: %s\n", i, h.URL)
		}
	case "ping":
		if len(args) < 2 {
			fmt.Println("usage: ping <host:port>")
			return
		}
		ch, err := buildChain(cfg)
		if err != nil {
			fmt.Println("chain error:", err)
			return
		}
		ctx := context.Background()
		start := time.Now()
		conn, err := ch.Dial(ctx, "tcp", args[1])
		if err != nil {
			fmt.Println("dial error:", err)
			return
		}
		conn.Close()
		fmt.Printf("ok %s in %s\n", args[1], time.Since(start))
	default:
		fmt.Println("unknown command")
	}
	_ = net.Dialer{}
}
