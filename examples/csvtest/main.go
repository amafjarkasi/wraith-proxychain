package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
)

func main() {
	path := os.Getenv("PROXIES_CSV")
	if path == "" {
		fmt.Println("set PROXIES_CSV=path/to/proxies.csv")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		fmt.Println(err)
		return
	}
	var proxies []*hop.Proxy
	for i, row := range rows {
		if i == 0 {
			continue
		}
		// Type,Host,Port,Username,Password,...
		typ, host, port, user, pass := row[0], row[1], row[2], row[3], row[4]
		if !strings.EqualFold(typ, "HTTP") {
			continue
		}
		u, _ := url.Parse(fmt.Sprintf("http://%s:%s@%s:%s", user, pass, host, port))
		p, err := hop.New(u.String())
		if err != nil {
			fmt.Println("parse err", err)
			continue
		}
		proxies = append(proxies, p)
		fmt.Printf("loaded %s %s:%s\n", typ, host, port)
	}

	// Test single-hop: dial httpbin ip through each proxy
	target := "httpbin.org:80"
	for i, p := range proxies {
		ch := chain.New([]*hop.Proxy{p}, chain.WithStrategy(chain.NewRandom()))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		conn, err := ch.Dial(ctx, "tcp", target)
		cancel()
		if err != nil {
			fmt.Printf("[%d] single dial FAIL: %v\n", i, err)
			continue
		}
		conn.Close()
		fmt.Printf("[%d] single dial OK\n", i)
	}

	// Test 2-hop chain with first two
	if len(proxies) >= 2 {
		ch := chain.New(proxies[:2], chain.WithStrategy(chain.NewRandom()))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		conn, err := ch.Dial(ctx, "tcp", target)
		cancel()
		if err != nil {
			fmt.Printf("2-hop dial FAIL: %v\n", err)
		} else {
			conn.Close()
			fmt.Println("2-hop dial OK")
		}
	}

	// HTTP GET through single proxy to verify egress IP
	if len(proxies) > 0 {
		p := proxies[0]
		u, _ := url.Parse("http://" + p.URL.Host)
		u.User = p.URL.User
		client := &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(u)},
			Timeout:   20 * time.Second,
		}
		resp, err := client.Get("http://httpbin.org/ip")
		if err != nil {
			fmt.Println("http get err:", err)
			return
		}
		defer resp.Body.Close()
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		fmt.Printf("egress: %s\n", strings.TrimSpace(string(buf[:n])))
	}
}
