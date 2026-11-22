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
	// No proxies = direct dial; add hop.New(...) entries for real chains.
	var hops []*hop.Proxy
	m := &metrics.Metrics{}
	opts := append([]chain.Option{chain.WithMetrics(m)}, chain.GhostOptions()...)
	ch := chain.New(hops, opts...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := ch.Dial(ctx, "tcp", "example.com:80")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	conn.Close()
	fmt.Printf("ok metrics=%v\n", m.Snapshot())
	ch.Stop()
}