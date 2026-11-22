package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
	"github.com/wraith/proxychain/metrics"
)

func main() {
	raw := os.Getenv("PROXY_URL")
	if raw == "" {
		fmt.Println("set PROXY_URL=http://user:pass@host:port")
		return
	}
	ps := []string{raw}
	hops := make([]*hop.Proxy, 0)
	for _, u := range ps {
		p, _ := hop.New(u)
		hops = append(hops, p)
	}
	m := &metrics.Metrics{}
	ch := chain.New(hops,
		chain.WithRetries(2),
		chain.WithTunnel(true),
		chain.WithMetrics(m),
		chain.WithJitter(time.Millisecond*50, time.Millisecond*150),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ch.Dial(ctx, "tcp", "httpbin.org:80")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	conn.Close()
	fmt.Printf("ok metrics=%v\n", m.Snapshot())
}
