package evade

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"time"
)

// FragmentWrite splits data into randomly sized chunks to evade traffic analysis.
func FragmentWrite(w io.Writer, data []byte, minChunk, maxChunk int) error {
	if minChunk < 1 {
		minChunk = 1
	}
	if maxChunk < minChunk {
		maxChunk = minChunk
	}
	i := 0
	for i < len(data) {
		size := minChunk
		if maxChunk > minChunk {
			r, _ := rand.Int(rand.Reader, big.NewInt(int64(maxChunk-minChunk+1)))
			size = minChunk + int(r.Int64())
		}
		if size > len(data)-i {
			size = len(data) - i
		}
		if _, err := w.Write(data[i : i+size]); err != nil {
			return err
		}
		i += size
		// tiny jitter
		jr, _ := rand.Int(rand.Reader, big.NewInt(5))
		time.Sleep(time.Duration(jr.Int64()) * time.Millisecond)
	}
	return nil
}

// RandomSNI picks a plausible SNI from a list to front a connection.
func RandomSNI(hosts []string) string {
	if len(hosts) == 0 {
		return ""
	}
	r, _ := rand.Int(rand.Reader, big.NewInt(int64(len(hosts))))
	return hosts[r.Int64()]
}

// DecoyBurst opens short-lived connections to benign hosts to add noise.
func DecoyBurst(hosts []string, count int) {
	for i := 0; i < count; i++ {
		if len(hosts) == 0 {
			return
		}
		r, _ := rand.Int(rand.Reader, big.NewInt(int64(len(hosts))))
		h := hosts[r.Int64()]
		conn, err := net.DialTimeout("tcp", h, 3*time.Second)
		if err != nil {
			continue
		}
		conn.Close()
		time.Sleep(10 * time.Millisecond)
	}
}

// DomainFront returns a front domain for SNI while keeping real host in Host header.
func DomainFront(front, realHost string) (sni, host string) {
	return front, realHost
}

// RandomDelay sleeps a crypto-random duration up to max.
func RandomDelay(max time.Duration) {
	if max <= 0 {
		return
	}
	r, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	time.Sleep(time.Duration(r.Int64()))
}

var _ = binary.LittleEndian
var _ = io.EOF
