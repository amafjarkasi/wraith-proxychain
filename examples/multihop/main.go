package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
)

func main() {
	raws := []string{
		"http://user:pass@p1.example.com:8080",
		"socks5://user:pass@p2.example.com:1080",
		"http://user:pass@p3.example.com:8080",
	}
	proxies := make([]*hop.Proxy, 0, len(raws))
	for _, r := range raws {
		p, err := hop.New(r)
		if err != nil {
			panic(err)
		}
		proxies = append(proxies, p)
	}

	ch := chain.New(proxies,
		chain.WithStrategy(chain.NewRandom()),
		chain.WithJitter(50*time.Millisecond, 300*time.Millisecond),
		chain.WithRetries(3),
		chain.WithTunnel(true),
	)
	defer ch.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := ch.Dial(ctx, "tcp", "target.example.com:443")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	defer conn.Close()
	fmt.Println("connected via", len(proxies), "hops")
}