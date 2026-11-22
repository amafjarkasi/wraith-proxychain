package main

import (
	"fmt"
	"net"

	"github.com/wraith/proxychain/crypto"
)

func main() {
	// In real use, conn comes from chain.Dial or net.Dial.
	conn, err := net.Dial("tcp", "example.com:443")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	// End-to-end X25519 + AES-256-GCM
	tun, err := crypto.Wrap(conn, true)
	if err != nil {
		panic(err)
	}
	defer tun.Close()

	// Layered (onion) encryption
	onion, _ := crypto.WrapLayers(conn, true, 2)
	defer onion.Close()

	// Constant-size frames
	padded, _ := crypto.WrapPadded(conn, true, 512)
	defer padded.Close()

	// Pre-shared key tunnel
	key := make([]byte, 32)
	kt, _ := crypto.WrapKey(conn, key)
	defer kt.Close()

	fmt.Println("tunnels created:", tun != nil, onion != nil, padded != nil, kt != nil)
}