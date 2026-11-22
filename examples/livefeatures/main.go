// Command livefeatures exercises chain features against real proxies listed in PROXIES_CSV.
package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/crypto"
	"github.com/wraith/proxychain/hop"
	"github.com/wraith/proxychain/metrics"
)

const target = "httpbin.org:80"

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
	for _, row := range rows[1:] {
		if len(row) < 5 || !strings.EqualFold(row[0], "HTTP") {
			continue
		}
		u := fmt.Sprintf("http://%s:%s@%s:%s", row[3], row[4], row[1], row[2])
		p, err := hop.New(u)
		if err == nil {
			out = append(out, p)
		}
	}
	return out
}

func getIP(ch *chain.Chain) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	conn, err := ch.Dial(ctx, "tcp", target)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /ip HTTP/1.1\r\nHost: httpbin.org\r\nConnection: close\r\n\r\n")
	b, err := io.ReadAll(conn)
	if err != nil {
		return "", err
	}
	s := string(b)
	i := strings.Index(s, `"origin": "`)
	if i < 0 {
		return "", fmt.Errorf("no origin in response")
	}
	s = s[i+len(`"origin": "`):]
	j := strings.Index(s, `"`)
	if j < 0 {
		return "", fmt.Errorf("bad origin")
	}
	return s[:j], nil
}

func run(name string, fn func() error) {
	start := time.Now()
	if err := fn(); err != nil {
		fmt.Printf("FAIL %s: %v\n", name, err)
		return
	}
	fmt.Printf("OK   %s (%s)\n", name, time.Since(start).Round(time.Millisecond))
}

func main() {
	ps := load()
	if len(ps) < 3 {
		fmt.Println("need >=3 HTTP proxies in PROXIES_CSV")
		return
	}

	run("single-hop", func() error {
		ip, err := getIP(chain.New(ps[:1]))
		if err != nil {
			return err
		}
		fmt.Println("     egress", ip)
		return nil
	})

	run("3-hop-chain", func() error {
		ip, err := getIP(chain.New(ps[:3], chain.WithRetries(1)))
		if err != nil {
			return err
		}
		fmt.Println("     egress", ip)
		return nil
	})

	run("failover-dead-hop", func() error {
		dead, _ := hop.New("http://127.0.0.1:1")
		_, err := getIP(chain.New([]*hop.Proxy{dead, ps[0], ps[1]}, chain.WithRetries(1)))
		return err
	})

	run("sticky-session", func() error {
		ch := chain.New(ps[:3], chain.WithSessionTTL(time.Minute))
		ch.SessionKey = "livefeatures"
		a, err := getIP(ch)
		if err != nil {
			return err
		}
		b, err := getIP(ch)
		if err != nil {
			return err
		}
		fmt.Printf("     egress %s -> %s (sticky)\n", a, b)
		return nil
	})

	run("fail-closed", func() error {
		ch := chain.New(nil, chain.WithFailClosed(true))
		_, err := ch.Dial(context.Background(), "tcp", target)
		if err == nil {
			return fmt.Errorf("expected fail-closed error")
		}
		return nil
	})

	run("host-routing", func() error {
		ch := chain.New(nil,
			chain.WithFailClosed(true),
			chain.WithRoute(func(host string) []*hop.Proxy {
				if strings.Contains(host, "httpbin") {
					return ps[:2]
				}
				return nil
			}),
		)
		ip, err := getIP(ch)
		if err != nil {
			return err
		}
		fmt.Println("     egress", ip)
		return nil
	})

	run("metrics", func() error {
		m := &metrics.Metrics{}
		ch := chain.New(ps[:2], chain.WithMetrics(m))
		if _, err := getIP(ch); err != nil {
			return err
		}
		fmt.Printf("     %v\n", m.Snapshot())
		return nil
	})

	// Proxies reject loopback targets, so the tunnel handshake is verified locally.
	run("tls-tunnel-local", func() error {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		defer ln.Close()
		go func() {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
			t, err := crypto.Wrap(c, false)
			if err != nil {
				return
			}
			defer t.Close()
			br := bufio.NewReader(t)
			for {
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(line) == "" {
					break
				}
			}
			fmt.Fprint(t, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		}()
		ch := chain.New(nil, chain.WithTunnel(true))
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		conn, err := ch.Dial(ctx, "tcp", ln.Addr().String())
		if err != nil {
			return err
		}
		defer conn.Close()
		fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
		b, err := io.ReadAll(conn)
		if err != nil {
			return err
		}
		if !strings.Contains(string(b), "ok") {
			return fmt.Errorf("bad tunnel response: %q", b)
		}
		return nil
	})

}
