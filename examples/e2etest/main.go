package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/dashboard"
	"github.com/wraith/proxychain/evade"
	"github.com/wraith/proxychain/hop"
)

func load() []*hop.Proxy {
	path := os.Getenv("PROXIES_CSV")
	if path == "" {
		fmt.Println("set PROXIES_CSV=path/to/proxies.csv")
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		fmt.Println(err)
		return nil
	}
	var out []*hop.Proxy
	for i, row := range rows {
		if i == 0 {
			continue
		}
		u := fmt.Sprintf("http://%s:%s@%s:%s", row[3], row[4], row[1], row[2])
		p, _ := hop.New(u)
		out = append(out, p)
	}
	return out
}

func main() {
	ps := load()
	if len(ps) < 3 {
		fmt.Println("need at least 3 proxies in PROXIES_CSV")
		return
	}
	ch := chain.New(ps[:3], chain.WithJitter(50*time.Millisecond, 200*time.Millisecond), chain.WithRetries(2))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// direct HTTP via chain
	conn, err := ch.Dial(ctx, "tcp", "httpbin.org:80")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	// send HTTP request manually
	fmt.Fprintf(conn, "GET /ip HTTP/1.1\r\nHost: httpbin.org\r\nConnection: close\r\n\r\n")
	buf, _ := io.ReadAll(conn)
	conn.Close()
	body := string(buf)
	fmt.Println("response bytes:", len(buf))
	if !strings.Contains(body, "origin") {
		fmt.Println("unexpected response")
	}
	// canary check
	tok := evade.Canary()
	if evade.CanaryLeaked(body, tok) {
		fmt.Println("CANARY LEAKED (unexpected)")
	} else {
		fmt.Println("canary ok (not leaked)")
	}
	// dashboard
	go func() { dashboard.Serve(":8099", ch) }()
	time.Sleep(500 * time.Millisecond)
	resp, _ := http.Get("http://127.0.0.1:8099/health")
	if resp != nil {
		resp.Body.Close()
		fmt.Println("dashboard ok")
	}
	ch.Stop()
	fmt.Println("E2E DONE")
}
