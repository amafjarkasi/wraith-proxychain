# Usage Guide

This guide explains how to use `wraith` in real projects, with concrete examples.

## Install

```sh
go get github.com/wraith/proxychain
```

Requires Go 1.25+. Dependencies: `gopkg.in/yaml.v3`, `github.com/fsnotify/fsnotify`.

## 1. Single-hop chain

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
)

func main() {
	p, err := hop.New("http://user:pass@proxy.example.com:8080")
	if err != nil {
		panic(err)
	}
	ch := chain.New([]*hop.Proxy{p}, chain.WithRetries(2))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := ch.Dial(ctx, "tcp", "example.com:443")
	if err != nil {
		fmt.Println("dial err:", err)
		return
	}
	defer conn.Close()
	// conn is a normal net.Conn; read/write as usual
	fmt.Println("connected via", conn.RemoteAddr())
	ch.Stop()
}
```

> Proxy auth: HTTP CONNECT auto-negotiates Basic, Digest, or NTLMv2 from the proxy's `Proxy-Authenticate` challenge. For NTLM use `http://DOMAIN\\user:pass@proxy:8080` (domain optional). HTTPS (uTLS) and HTTP/2 CONNECT currently use Basic only.

### SOCKS5 hop

```go
p, _ := hop.New("socks5://user:pass@proxy.example.com:1080")
```

### HTTPS proxy

```go
p, _ := hop.New("https://user:pass@proxy.example.com:443")
```

## 2. Multi-hop chain

```go
raws := []string{
	"http://user:pass@p1.example.com:8080",
	"socks5://user:pass@p2.example.com:1080",
	"http://user:pass@p3.example.com:8080",
}
proxies := make([]*hop.Proxy, 0, len(raws))
for _, r := range raws {
	p, err := hop.New(r)
	if err != nil {
		panic(err)
	}
	proxies = append(proxies, p)
}

ch := chain.New(proxies,
	chain.WithStrategy(chain.NewRandom()),
	chain.WithJitter(50*time.Millisecond, 300*time.Millisecond),
	chain.WithRetries(3),
	chain.WithTunnel(true),
)
conn, err := ch.Dial(ctx, "tcp", "target.example.com:443")
```

### Chaining mechanics

- First hop dials the second hop's address.
- Each subsequent hop uses `CONNECT` (HTTP) or SOCKS5 `CONNECT` to tunnel.
- Final hop connects to the real target.

## 3. Rotation strategies

```go
// Randomized order each dial
ch := chain.New(proxies, chain.WithStrategy(chain.NewRandom()))

// Cyclic rotation
ch := chain.New(proxies, chain.WithStrategy(chain.NewRoundRobin()))

// Prefer hops with fewer failures / lower latency
ch := chain.New(proxies, chain.WithStrategy(chain.NewWeighted(ch))) // ch needed

// Probe then sort by latency
ch := chain.New(proxies, chain.WithStrategy(chain.NewLatency(ch)))
```

### Sticky sessions

```go
ch := chain.New(proxies, chain.WithSessionTTL(5*time.Minute))
ch.SessionKey = "user-123" // same key => same hop order until TTL
```

### Persistent sessions

```go
ch := chain.New(proxies, chain.WithSessionFile("/var/lib/wraith/sessions.json"))
```

## 4. Options reference

| Option | Purpose | Example |
|---|---|---|
| `WithStrategy(r)` | Rotation strategy | `chain.NewRandom()` |
| `WithJitter(min,max)` | Random inter-hop delay | `50ms, 300ms` |
| `WithTimingJitter(min,max)` | Jitter before each write | `10ms, 80ms` |
| `WithCoverTraffic(cfg)` | Framed decoy frames (both ends) | `evade.CoverConfig{Interval: 2*time.Second}` |
| `WithRetries(n)` | Retry count on failure | `3` |
| `WithTunnel(bool)` | End-to-end encryption | `true` |
| `WithWebSocketTunnel(url,cfg)` | WebSocket transport with keepalive | `ws.Config{Interval: 30*time.Second}` |
| `WithMetrics(m)` | Metrics recorder (per-hop if `HopMetrics`) | `&metrics.Metrics{}` |
| `WithDialer(d)` | Raw TCP dialer | `dns.NewDialer()` |
| `WithDoH()` | Shortcut for DoH dialer | — |
| `WithPoolSize(n)` | Connection pooling | `4` |
| `WithRateLimit(bps)` | Bandwidth throttle | `50*1024` |
| `WithSessionTTL(d)` | Session stickiness TTL | `5*time.Minute` |
| `WithSessionFile(path)` | Persist sessions | `/tmp/sessions.json` |
| `WithHealthInterval(d)` | Background probes | `30*time.Second` |
| `WithSpareProxies(spares)` | Self-healing pool | `[]*hop.Proxy{...}` |
| `WithFailClosed(on)` | Disable direct dial when no proxies | `true` |
| `WithRoute(fn)` | Host-based split tunneling | `func(host string) []*hop.Proxy` |
| `GhostOptions()` | Max-evasion preset | see below |

### Ghost mode (max evasion)

```go
opts := append([]chain.Option{chain.WithMetrics(m)}, chain.GhostOptions()...)
ch := chain.New(proxies, opts...)
```

Ghost mode enables: jitter (50-500ms), 3 retries, tunnel encryption, 50KB/s rate limit, pool size 4, 15s health probes.

## 5. Resilience behavior

- **Circuit breaker**: on failure, hop is marked dead for exponential backoff (1s, 2s, 4s, ... up to 5m) + jitter. Auto-recovers after deadline.
- **Retries**: `Dial` retries up to `retries` times, rebuilding the chain each attempt.
- **Self-healing**: dead hops replaced from spare pool (`WithSpareProxies`).
- **Pooling**: `WithPoolSize` reuses connections; `Close()` returns to pool, real close on eviction.
- **Draining**: `ch.Stop()` stops probes and drains pool.

## 6. Encryption (`crypto` package)

```go
import "github.com/wraith/proxychain/crypto"

// X25519 key exchange + AES-256-GCM
tun, err := crypto.Wrap(conn, true) // initiator=true on client side
// use tun as net.Conn

// Layered (onion) encryption
onion, _ := crypto.WrapLayers(conn, true, 3)

// Pre-shared key
key := make([]byte, 32)
// fill key
kt, _ := crypto.WrapKey(conn, key)

// Constant-size frames (traffic shaping)
padded, _ := crypto.WrapPadded(conn, true, 512)

// Rekey
newTun, _ := crypto.Rekey(conn, newKey)

// Derive key from shared secret
derived := crypto.DeriveKey(sharedSecret, "label")
```

### Nonce safety

Counters are 64-bit; wraparound is practically impossible. Each tunnel instance has independent counters.

## 7. Evasion (`evade` package)

```go
import (
	"net/netip"
	"time"

	"github.com/wraith/proxychain/evade"
)

// Timing
evade.Jitter(50*time.Millisecond, 300*time.Millisecond)
evade.RandomDelay(200 * time.Millisecond)

// Padding
padded := evade.Pad([]byte("secret"), 64)
original, _ := evade.Unpad(padded)

// Fragmented writes
evade.FragmentWrite(writer, data, 10, 100)

// Fingerprint randomization
headers := evade.RandomHeaders()
sni := evade.RandomSNI([]string{"a.com", "b.com"})
order := evade.RandomOrder(items)

// Decoy traffic
evade.DecoyBurst([]string{"example.com:443"}, 5)

// Domain fronting
sni, host := evade.DomainFront("front.example.com", "real.example.com")

// Canary tokens
token := evade.Canary()
leaked := evade.CanaryLeaked(observedData, token)

// Honeypot detection (configurable CIDR checker)
hc := evade.NewHoneypotChecker()
_ = hc.AddCIDR("10.99.0.0/16")
if addr, err := netip.ParseAddr(ip); err == nil && hc.ContainsAddr(addr) { /* sinkhole */ }
intercepted := evade.HoneypotProbeTimeout(conn, token, 2*time.Second)

// Opt-in decoys (cost bandwidth; use only where safe)
shaped := evade.NewTimingConn(conn, 10*time.Millisecond, 80*time.Millisecond)
cover := evade.NewCoverConn(shaped, evade.CoverConfig{Interval: 2 * time.Second})
_ = evade.BenignRequests(ctx, client, []string{"https://example.com/health"})
_ = evade.DecoyDNS(ctx, dns.NewResolver(), []string{"example.com"})
evade.ConnectionChurn(ctx, []string{"example.com:443"}, 3, true)
_ = evade.BrowserHeaders("chrome")
hint := evade.RegionForHost("proxy.example.de:8080")
```

## 8. Config

### YAML

```yaml
name: mychain
hops:
  - url: http://user:pass@proxy1:8080
  - url: socks5://proxy2:1080
strategy: weighted
session_ttl: 5m
jitter_min: 50ms
jitter_max: 300ms
```

### Load

```go
cfg, err := config.Load("config.yaml")
```

### Env

```sh
export PROXY_CHAIN="http://p1,http://p2"
```

```go
cfg := config.FromEnv()
```

### Validation

- At least one hop.
- No duplicate adjacent hops.
- Strategy defaults to `random`.

### Hot reload

```go
stop := make(chan struct{})
config.Watch("config.yaml", func(c *config.ChainConfig) {
	// rebuild chain from c
}, stop)
```

## 9. DNS (DNS-over-HTTPS dialer)

```go
import (
	"context"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/dns"
)

ip, err := dns.Resolve("example.com") // DoH, Cloudflare + Google fallback, cached 1m

ch := chain.New(hops, chain.WithDoH())
// or: chain.WithDialer(dns.NewDialer())
// Both direct targets and proxy hostnames are resolved via DoH.
// Resolution failure falls back to the original hostname.

r := dns.NewResolver("https://1.1.1.1/dns-query") // custom endpoints
ip2, _ := r.Resolve(context.Background(), "example.com")
```

## 10. Binaries

### proxychain CLI

```sh
export PROXY_CHAIN="http://p1,http://p2"
go build ./cmd/proxychain
./proxychain list
./proxychain ping example.com:443
./proxychain -config config.yaml ping example.com:443
```

### proxyd (CONNECT server)

```sh
go build ./cmd/proxyd
./proxyd -addr :8080 -config config.yaml
# clients use http://localhost:8080 as HTTP proxy
```

## 11. Dashboard

```go
import "github.com/wraith/proxychain/dashboard"

go dashboard.Serve(":8099", ch)
// GET /health -> JSON hop health
// GET / -> HTML view
```

## 12. Metrics

```go
import "github.com/wraith/proxychain/metrics"

m := &metrics.Metrics{}
ch := chain.New(proxies, chain.WithMetrics(m))
// after dials:
snap := m.Snapshot() // dials, failures, bytes, avg_lat_ns, and "hops" per-proxy map
http.Handle("/metrics", m.Handler()) // prints per-hop lines too
// Inject any recorder implementing RecordDial and, optionally,
// RecordHop(proxy string, latency time.Duration, err error).
```

## 13. Extensibility

### SessionStore (distributed stickiness)

```go
type MyStore struct{}
func (s *MyStore) Get(key string) ([]*hop.Proxy, bool) { /* ... */ }
func (s *MyStore) Set(key string, hops []*hop.Proxy, ttl time.Duration) { /* ... */ }

ch := chain.New(proxies, chain.WithSessionStore(&MyStore{}))
```

### RateLimiter (shared throttling)

```go
type MyLimiter struct{}
func (l *MyLimiter) Wait(n int) { /* block until n bytes allowed */ }

ch := chain.New(proxies, chain.WithRateLimiter(&MyLimiter{}))
```

### Metrics

Implement `RecordDial(lat time.Duration, err error)`.

## 14. Hop stats / snapshot

```go
snap := ch.HopsSnapshot() // current hop list
stats := ch.HopStats()    // per-hop failures/latency/dead status
```

## 15. Testing

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...
```

`chain/stress_test.go` covers: hop counts 1/2/3/5/8, all 4 strategies, dead-hop filtering, spares, pooling, jitter, 20 concurrent dials.

## 16. Cleanup

```go
ch.Stop() // stops health probes, drains connection pool
```

## 17b. Multipath routing

`chain.MultiChain` combines several `chain.Chain` paths with one dial call:

```go
paths := []chain.PathDialer{chain.New(hopsA), chain.New(hopsB)}
m := chain.NewMultiChain(paths, chain.MultipathFailover) // or Stripe/Fastest/Race
defer m.Stop()
conn, err := m.Dial(ctx, "tcp", "example.com:443")
```

- `MultipathFailover` — try paths in order until one succeeds.
- `MultipathStripe` — rotate the starting path per dial.
- `MultipathFastest` — prefer the path with fewer failures, then lower latency.
- `MultipathRace` — dial all paths in parallel; return the first success and close losers.

## 17. Troubleshooting

- **"no config"**: set `PROXY_CHAIN` or pass `-config`.
- **Socket exhaustion in tests**: reduce concurrency or run packages separately.
- **Dead hops never recover**: check `recordFailure` backoff; ensure health probes enabled.
- **DNS leaks**: use `dns.Resolve` / `dns.DialContext` instead of system resolver.
- **TLS fingerprinting**: HTTPS proxy hops randomize ClientHello via uTLS; HTTP/2 CONNECT hops stay Basic-only.

## 19. Example programs

See [`examples/README.md`](examples/README.md) for the full index with links, environment variables, and use cases.

Runnable programs in `examples/`:

- `basic` — single hop
- `multihop` — multi-hop with tunnel
- `rotation` — strategy comparison
- `encryption` — crypto wrappers
- `failover` — spare pool self-healing
- `multipath` — combine chains with a multipath policy
- `websocket` — WebSocket tunnel with keepalive
- `doh` — DoH-backed dialing via `WithDialer`
- `pooling` — connection reuse
- `sticky` — session stickiness
- `ghost` — max-evasion preset
- `config` — config loading
- `dashboard` — health dashboard
- `metrics` — metrics handler
- `dns` — DoH
- `evade` — evasion toolkit
- `honeypot` — CIDR sinkhole checks + deadline canary probe
- `proxyproto` — HAProxy PROXY v2 header write/read and listener override

### WebSocket tunnels

`chain.WithWebSocketTunnel(wsURL, cfg)` upgrades the dialed connection to a
WebSocket before the optional crypto tunnel. The client sends ping frames on
`cfg.Interval` and closes the connection after `cfg.Missed` missed pongs
(`cfg.Timeout` each). Servers use `ws.Upgrade(w, r, cfg)` to accept the tunnel.

```go
ch := chain.New(proxies, chain.WithWebSocketTunnel("ws://relay.example/tunnel", ws.Config{
    Interval: 30 * time.Second,
    Timeout:  10 * time.Second,
    Missed:   3,
}))
```

## 18. Roadmap

- (done) uTLS fingerprint randomization for HTTPS proxies
- (done) HTTP/2 CONNECT (h2/h2c) via golang.org/x/net/http2
- (done) SOCKS5 UDP associate
- (done) HAProxy PROXY protocol v2 (`proxyd -proxyproto`)
- (done) NTLMv2 proxy auth (Basic + Digest for HTTP CONNECT; Basic for HTTP/2 CONNECT)
- Redis session store
- (done) WebSocket tunnel with keepalive (`ws` package + `chain.WithWebSocketTunnel`)
- gRPC/H2 multiplexing
- (done) Multipath routing (`chain.NewMultiChain` with failover/stripe/fastest/race)
- ML-based adaptive routing
- eBPF kernel interception