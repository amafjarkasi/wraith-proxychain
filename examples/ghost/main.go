package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
	"github.com/wraith/proxychain/metrics"
)

func main() {
	p, _ := hop.New("http://user:pass@proxy.example.com:8080")
	m := &metrics.Metrics{}
	opts := append([]chain.Option{chain.WithMetrics(m)}, chain.GhostOptions()...)
	ch := chain.New([]*hop.Proxy{p}, opts...)
	defer ch.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := ch.Dial(ctx, "tcp", "example.com:443")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	defer conn.Close()
	fmt.Println("ghost mode connected; metrics:", m.Snapshot())
	_ = time.Second
}