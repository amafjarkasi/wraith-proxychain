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

	ch := chain.New(proxies, chain.WithSessionTTL(5*time.Minute), chain.WithRetries(2))
	ch.SessionKey = "user-123" // same key => same hop order until TTL
	defer ch.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		conn, err := ch.Dial(ctx, "tcp", "example.com:443")
		if err != nil {
			fmt.Println("dial err:", err)
			continue
		}
		conn.Close()
		fmt.Printf("request %d ok (sticky)\n", i+1)
	}
}