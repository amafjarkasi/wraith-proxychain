package main

import (
	"fmt"
	"strings"

	"github.com/wraith/proxychain/evade"
)

func main() {
	evade.Jitter(50, 300)
	_ = evade.RandomDelay

	padded := evade.Pad([]byte("secret"), 64)
	original, _ := evade.Unpad(padded)
	fmt.Printf("pad/unpad ok: %s\n", string(original))

	headers := evade.RandomHeaders()
	fmt.Println("user-agent:", headers["User-Agent"])

	sni := evade.RandomSNI([]string{"a.com", "b.com"})
	fmt.Println("sni:", sni)

	token := evade.Canary()
	fmt.Println("canary leaked:", evade.CanaryLeaked("data "+token, token))

	_ = strings.Contains
}