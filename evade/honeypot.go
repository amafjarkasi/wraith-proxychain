package evade

import (
	"crypto/rand"
	"encoding/hex"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// defaultSinkholes are non-routable, documentation, benchmarking, and
// sinkhole ranges that a real proxy exit should never occupy.
var defaultSinkholes = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
	"192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24",
	"203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "fc00::/7", "fe80::/10", "2001:db8::/32",
}

// HoneypotChecker tests IPs against configurable sinkhole/reserved ranges.
type HoneypotChecker struct {
	mu       sync.RWMutex
	prefixes []netip.Prefix
}

// NewHoneypotChecker returns a checker preloaded with reserved/sinkhole ranges.
func NewHoneypotChecker() *HoneypotChecker {
	h := &HoneypotChecker{}
	for _, cidr := range defaultSinkholes {
		_ = h.AddCIDR(cidr)
	}
	return h
}

var defaultChecker = NewHoneypotChecker()

// AddCIDR adds a custom range to the checker.
func (h *HoneypotChecker) AddCIDR(cidr string) error {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.prefixes = append(h.prefixes, p.Masked())
	h.mu.Unlock()
	return nil
}

// Contains reports whether ip falls in a configured range.
func (h *HoneypotChecker) Contains(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return h.ContainsAddr(addr)
}

// ContainsAddr reports whether addr falls in a configured range.
func (h *HoneypotChecker) ContainsAddr(addr netip.Addr) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	addr = addr.Unmap()
	for _, p := range h.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Len returns the number of configured ranges.
func (h *HoneypotChecker) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.prefixes)
}

// HoneypotIP reports whether ip falls in a known reserved/sinkhole range.
func HoneypotIP(ip string) bool { return defaultChecker.Contains(ip) }

// HoneypotCheckerDefault returns the shared default checker.
func HoneypotCheckerDefault() *HoneypotChecker { return defaultChecker }

// HoneypotProbe sends a canary through conn and reports whether the response
// looks altered. It uses a read deadline when conn supports one.
func HoneypotProbe(conn interface {
	Write([]byte) (int, error)
	Read([]byte) (int, error)
}, token string) bool {
	return HoneypotProbeTimeout(conn, token, 2*time.Second)
}

// HoneypotProbeTimeout is HoneypotProbe with an explicit read deadline.
func HoneypotProbeTimeout(conn interface {
	Write([]byte) (int, error)
	Read([]byte) (int, error)
}, token string, timeout time.Duration) bool {
	if token == "" {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		token = "CANARY-" + hex.EncodeToString(b)
	}
	if d, ok := conn.(interface{ SetReadDeadline(time.Time) error }); ok && timeout > 0 {
		_ = d.SetReadDeadline(time.Now().Add(timeout))
	}
	if _, err := conn.Write([]byte(token)); err != nil {
		return true
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return true
	}
	return !strings.Contains(string(buf[:n]), token)
}
