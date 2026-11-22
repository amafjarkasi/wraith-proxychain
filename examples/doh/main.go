// Command doh dials a target through a chain whose raw connections are
// resolved with DNS-over-HTTPS.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/dns"
	"github.com/wraith/proxychain/hop"
)

func main() {
	target := "httpbin.org:80"
	if len(os.Args) > 1 {
		target = os.Args[1]
	}
	var hops []*hop.Proxy
	if raw := os.Getenv("PROXY_CHAIN"); raw != "" {
		for _, s := range split(raw) {
			p, err := hop.New(s)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return
			}
			hops = append(hops, p)
		}
	}
	ch := chain.New(hops, chain.WithDialer(dns.NewDialer()), chain.WithHealthInterval(time.Hour))
	defer ch.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ch.Dial(ctx, "tcp", target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /ip HTTP/1.1\r\nHost: httpbin.org\r\nConnection: close\r\n\r\n")
	b, err := io.ReadAll(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Print(string(b))
}

func split(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
