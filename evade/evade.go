package evade

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
	"time"
)

func Jitter(min, max time.Duration) {
	if max <= min {
		return
	}
	d := max - min
	r, _ := rand.Int(rand.Reader, big.NewInt(int64(d)))
	time.Sleep(min + time.Duration(r.Int64()))
}

func Pad(b []byte, maxPad int) []byte {
	if maxPad < 0 {
		maxPad = 0
	}
	r, _ := rand.Int(rand.Reader, big.NewInt(int64(maxPad+1)))
	pad := int(r.Int64())
	out := make([]byte, 2+len(b)+pad)
	binary.BigEndian.PutUint16(out, uint16(len(b)))
	copy(out[2:], b)
	// random padding
	rand.Read(out[2+len(b):])
	return out
}

func Unpad(b []byte) ([]byte, error) {
	if len(b) < 2 {
		return nil, errShort
	}
	n := int(binary.BigEndian.Uint16(b))
	if n+2 > len(b) {
		return nil, errShort
	}
	out := make([]byte, n)
	copy(out, b[2:2+n])
	return out, nil
}

type sentinel string

var errShort = &shortErr{}

type shortErr struct{}

func (e *shortErr) Error() string { return "evade: short buffer" }

func RandomOrder[T any](s []T) []T {
	out := append([]T(nil), s...)
	for i := len(out) - 1; i > 0; i-- {
		r, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := int(r.Int64())
		out[i], out[j] = out[j], out[i]
	}
	return out
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0",
}

func RandomHeaders() map[string]string {
	ua := userAgents[0]
	if r, _ := rand.Int(rand.Reader, big.NewInt(int64(len(userAgents)))); r != nil {
		ua = userAgents[r.Int64()]
	}
	return map[string]string{
		"User-Agent":      ua,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9",
	}
}
