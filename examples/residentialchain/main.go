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
	entryRaw := os.Getenv("PROXY_ENTRY")      // e.g. http://user:pass@dc.example.com:8080
	residentialRaw := os.Getenv("PROXY_RESIDENTIAL") // e.g. http://user:pass@resi.example.com:9291
	if entryRaw == "" || residentialRaw == "" {
		fmt.Println("set PROXY_ENTRY and PROXY_RESIDENTIAL")
		return
	}
	entry, _ := hop.New(strings.TrimSpace(entryRaw))
	resi, _ := hop.New(strings.TrimSpace(residentialRaw))

	// Tiered chain: datacenter entry -> residential exit
	ch := chain.New([]*hop.Proxy{entry, resi},
		chain.WithStrategy(chain.NewRandom()),
		chain.WithSessionTTL(10*time.Minute), // sticky residential IP
		chain.WithRetries(2),
		chain.WithJitter(100*time.Millisecond, 500*time.Millisecond),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := ch.Dial(ctx, "tcp", "httpbin.org:80")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	conn.Close()
	fmt.Println("tiered chain OK")
	ch.Stop()
}