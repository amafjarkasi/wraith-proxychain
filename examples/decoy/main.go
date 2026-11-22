// Command decoy demonstrates opt-in traffic-decoy helpers.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/wraith/proxychain/evade"
)

func main() {
	fmt.Println("browser headers:", evade.BrowserHeaders("chrome")[0])
	fmt.Println("exit region hint:", evade.RegionForHost("proxy.example.de:8080"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = evade.BenignRequests(ctx, srv.Client(), []string{srv.URL + "/health", srv.URL + "/robots.txt"})
	fmt.Println("benign request sequence done")

	if os.Getenv("DECOY_DNS") == "1" {
		// Requires network; opt-in so the example stays offline by default.
		// _ = evade.DecoyDNS(ctx, dns.NewResolver(), []string{"example.com", "golang.org"})
		fmt.Println("set your own resolver to enable DecoyDNS")
	}

	// Framed cover traffic: both ends must use evade.NewCoverConn.
	client, peer := net.Pipe()
	cc := evade.NewCoverConn(client, evade.CoverConfig{Interval: 20 * time.Millisecond})
	sc := evade.NewCoverConn(peer, evade.CoverConfig{Interval: 30 * time.Millisecond})
	defer cc.Close()
	defer sc.Close()
	go io.Copy(sc, sc)
	_ = cc.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := cc.Write([]byte("real payload")); err != nil {
		fmt.Println("cover write err:", err)
		return
	}
	buf := make([]byte, len("real payload"))
	if _, err := io.ReadFull(cc, buf); err != nil {
		fmt.Println("cover read err:", err)
		return
	}
	fmt.Println("cover conn delivered:", string(buf))
}
