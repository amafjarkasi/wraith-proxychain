package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
)

func main() {
	proxies := make([]*hop.Proxy, 0, 3)
	for _, r := range []string{
		"http://user:pass@p1.example.com:8080",
		"http://user:pass@p2.example.com:8080",
		"http://user:pass@p3.example.com:8080",
	} {
		p, _ := hop.New(r)
		proxies = append(proxies, p)
	}

	strategies := map[string]chain.Rotator{
		"random":     chain.NewRandom(),
		"roundrobin": chain.NewRoundRobin(),
		"weighted":   chain.NewWeighted(nil),
	}
	for name, s := range strategies {
		ch := chain.New(proxies, chain.WithStrategy(s), chain.WithRetries(1))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		conn, err := ch.Dial(ctx, "tcp", "example.com:443")
		cancel()
		if err != nil {
			fmt.Printf("%s: %v\n", name, err)
		} else {
			fmt.Printf("%s: ok %v\n", name, conn.RemoteAddr())
			conn.Close()
		}
		ch.Stop()
	}
	_ = time.Second
}