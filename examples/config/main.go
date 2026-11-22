package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/config"
	"github.com/wraith/proxychain/hop"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		// fallback to env
		cfg = config.FromEnv()
	}
	if cfg == nil {
		panic("no config")
	}

	hops := make([]*hop.Proxy, 0, len(cfg.Hops))
	for _, h := range cfg.Hops {
		p, err := hop.New(h.URL)
		if err != nil {
			panic(err)
		}
		hops = append(hops, p)
	}
	ch := chain.New(hops, chain.WithStrategy(chain.NewRandom()))
	defer ch.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ch.Dial(ctx, "tcp", "example.com:443")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	defer conn.Close()
	fmt.Println("connected from config")
	_ = time.Second
}