// Command honeypot demonstrates CIDR-based sinkhole detection and a
// deadline-bounded canary probe.
package main

import (
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/wraith/proxychain/evade"
)

func main() {
	checker := evade.NewHoneypotChecker()
	_ = checker.AddCIDR("198.18.0.0/15") // already in defaults; safe duplicate
	for _, ip := range []string{"93.184.216.34", "127.0.0.1", "192.0.2.7", "2001:db8::1", "8.8.8.8"} {
		fmt.Printf("%-16s sinkhole=%v\n", ip, checker.Contains(ip))
	}
	addr, _ := netip.ParseAddr("10.0.0.9")
	fmt.Println("netip Addr check:", checker.ContainsAddr(addr))
	fmt.Println("ranges loaded:", checker.Len())

	// A peer that echoes the canary is a normal tunnel; an altered or missing
	// echo indicates interception. The probe has a hard read deadline.
	client, peer := net.Pipe()
	go func() {
		defer peer.Close()
		buf := make([]byte, 256)
		n, err := peer.Read(buf)
		if err != nil {
			return
		}
		_, _ = peer.Write(buf[:n])
	}()

	leaked := evade.HoneypotProbeTimeout(client, evade.Canary(), time.Second)
	fmt.Println("interception detected:", leaked)
	client.Close()
}
