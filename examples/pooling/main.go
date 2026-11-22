package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
)

func main() {
	p, _ := hop.New("http://user:pass@proxy.example.com:8080")
	ch := chain.New([]*hop.Proxy{p}, chain.WithPoolSize(8), chain.WithRetries(2))
	defer ch.Stop()

	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		conn, err := ch.Dial(ctx, "tcp", "example.com:443")
		cancel()
		if err != nil {
			fmt.Println("dial err:", err)
			continue
		}
		conn.Close() // returns to pool
		fmt.Printf("request %d ok\n", i+1)
	}
	_ = time.Second
}