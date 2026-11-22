package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wraith/proxychain/dns"
)

func main() {
	ip, err := dns.Resolve("example.com")
	if err != nil {
		fmt.Println("resolve err:", err)
		return
	}
	fmt.Println("resolved:", ip)

	conn, err := dns.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	defer conn.Close()
	fmt.Println("connected via DoH")
	_ = time.Second
}