package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
	"github.com/wraith/proxychain/metrics"
)

func main() {
	p, _ := hop.New("http://user:pass@proxy.example.com:8080")
	m := &metrics.Metrics{}
	ch := chain.New([]*hop.Proxy{p}, chain.WithMetrics(m), chain.WithRetries(2))
	defer ch.Stop()

	go http.ListenAndServe(":9090", m.Handler())
	fmt.Println("metrics at http://localhost:9090")
	time.Sleep(time.Hour)
}