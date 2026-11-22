package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
)

func main() {
	raw := os.Getenv("PROXY_CHAIN")
	if raw == "" {
		fmt.Println("set PROXY_CHAIN to comma-separated proxy URLs")
		return
	}
	var paths []chain.PathDialer
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, err := hop.New(s)
		if err != nil {
			fmt.Println("proxy error:", err)
			return
		}
		paths = append(paths, chain.New([]*hop.Proxy{p}))
	}
	if len(paths) < 2 {
		fmt.Println("multipath needs at least two proxies")
		return
	}
	m := chain.NewMultiChain(paths, chain.MultipathFailover)
	defer m.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := m.Dial(ctx, "tcp", "example.com:443")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	defer conn.Close()
	fmt.Println("connected via multipath")
}
