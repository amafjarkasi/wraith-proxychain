package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
)

func main() {
	primary, _ := hop.New("http://user:pass@primary.example.com:8080")
	spare, _ := hop.New("http://user:pass@spare.example.com:8080")
	dead, _ := hop.New("http://user:pass@dead.example.com:8080")

	ch := chain.New([]*hop.Proxy{dead, primary},
		chain.WithSpareProxies([]*hop.Proxy{spare}),
		chain.WithRetries(2),
	)
	defer ch.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := ch.Dial(ctx, "tcp", "example.com:443")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	defer conn.Close()
	fmt.Println("connected (self-healed if needed)")
	_ = time.Second
}