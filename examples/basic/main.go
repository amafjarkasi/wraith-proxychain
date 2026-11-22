package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
)

func main() {
	p, err := hop.New("http://user:pass@proxy.example.com:8080")
	if err != nil {
		panic(err)
	}
	ch := chain.New([]*hop.Proxy{p}, chain.WithRetries(2))
	defer ch.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := ch.Dial(ctx, "tcp", "example.com:443")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	defer conn.Close()
	fmt.Println("connected via", conn.RemoteAddr())
}