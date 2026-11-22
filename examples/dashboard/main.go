package main

import (
	"fmt"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/dashboard"
	"github.com/wraith/proxychain/hop"
)

func main() {
	p, _ := hop.New("http://user:pass@proxy.example.com:8080")
	ch := chain.New([]*hop.Proxy{p}, chain.WithRetries(2))
	defer ch.Stop()

	go func() {
		if err := dashboard.Serve(":8099", ch); err != nil {
			fmt.Println("dashboard err:", err)
		}
	}()
	fmt.Println("dashboard at http://localhost:8099")
	time.Sleep(time.Hour)
}