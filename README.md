# wraith — Resilient Multi-Hop Proxy Chains for Go

<p align="center">
  <img src="./logo.svg" alt="wraith logo" width="500" style="margin: 16px 0;">
</p>

<p align="center">
  <strong>A Go library and toolkit for building resilient, multi-hop proxy chains with rotation, an encrypted tunnel, traffic evasion, circuit breakers, and observability.</strong>
</p>

<p align="center">
  Chain HTTP CONNECT / SOCKS5 proxies, rotate their order, add an encrypted tunnel, shape traffic, monitor health, and self-heal. Stdlib-first with a small set of focused dependencies.
</p>

## Overview

wraith gives you one `net.Conn` that reaches a target through a sequence of proxies. The caller sees a normal connection; the library handles proxy negotiation, hop ordering, failure recovery, optional encryption, and optional traffic shaping.

- **Hop types**: HTTP CONNECT (Basic / Digest / NTLMv2; `https` hops get TLS only in the first position), SOCKS5 TCP (username/password), HTTP/2 CONNECT (`h2`, `h2c`). SOCKS5 UDP ASSOCIATE is a standalone single-hop helper, not a chain hop.
- **Chaining**: N-hop sequential tunneling with per-hop CONNECT / SOCKS5 negotiation, dials whose `ctx` bounds the TCP connect (proxy handshakes have no deadline yet), and buffered reads so no handshake bytes leak into application data.
- **Rotation**: Random, RoundRobin, Weighted (fewest failures, then lowest latency), Latency (probes hops in parallel, then sorts).
- **Resilience**: per-hop circuit breaker with exponential backoff + jitter, retries, spare-pool self-healing, background health probes, fail-closed mode, host-based routing, and multipath policies.
- **Encryption**: X25519 + AES-256-GCM tunnel and pre-shared-key mode. The key exchange is unauthenticated and frame nonces are reused across directions; see Encryption before relying on it.
- **Evasion**: jitter, padding, fragmentation, decoy connections, SNI/header randomization, canary tokens, honeypot checks, DoH, and a ghost-mode preset.
- **Observability**: atomic counters with an HTTP handler, JSON/HTML health dashboard, and hop-level stats (`HopsSnapshot`, `HopStats`).
- **Config**: YAML (JSON-compatible), `PROXY_CHAIN` env input, validation, and hot reload via fsnotify.
- **Extensibility**: pluggable `SessionStore`, `RateLimiter`, and `Metrics` interfaces.

Full walkthrough: [USAGE.md](./USAGE.md).

## Capabilities

| Area | Details |
|---|---|
| Hop protocols | HTTP CONNECT (Basic/Digest/NTLMv2; TLS in first position only), SOCKS5 TCP, HTTP/2 CONNECT (`h2`/`h2c`), SOCKS5 UDP ASSOCIATE (standalone) |
| Chaining | Sequential tunneling, N hops, connection pooling, host-based routing, opt-in fail-closed |
| Rotation | Random, RoundRobin, Weighted, Latency; sticky sessions with TTL and pluggable persistence |
| Resilience | Circuit breaker, retries, spare-pool replacement, pooling, background probes, multipath failover/stripe/fastest/race |
| Crypto | `Wrap`, `WrapKey`, `DeriveKey`, `OnionKeyExchange`. `WrapLayers`, `WrapPadded` and `Rekey` are exported but do not behave as their names suggest (see Encryption) |
| Evasion | Jitter, padding, fragmentation, decoys, SNI/header randomization, canaries, honeypot detection |
| DNS | DNS-over-HTTPS resolver dialer; wire into `chain` with `WithDialer(dns.NewDialer())` |
| PROXY protocol | `proxyproto` emits and parses a PROXY v2 header, but its signature bytes differ from the HAProxy spec, so other implementations do not recognize it. `proxyd -proxyproto` requires the header but does not use the client address |
| WebSocket | Binary WebSocket tunnels with ping/pong keepalive and dead-peer detection |
| Config | YAML (JSON-compatible), env (`PROXY_CHAIN`), validation, hot reload (fsnotify) |
| Binaries | `proxychain` (ping/list), `proxyd` (CONNECT server forwarding through a chain) |
| Dashboard | HTTP JSON health endpoint + HTML view |

## In plain English (what each part does)

- **`chain.New(proxies, options...)`** — builds a chain. You give it parsed proxies and settings; it returns a `*Chain`.
- **`ch.Dial(ctx, "tcp", "host:port")`** — opens a connection to the target through the whole chain and returns a normal `net.Conn`.
- **Strategies** — choose hop order: random, round-robin, or “best first” (weighted/latency).
- **`chain.NewMultiChain(paths, policy)`** — combines several chains/paths. `MultipathFailover` tries in order, `MultipathStripe` rotates, `MultipathFastest` prefers low latency, `MultipathRace` dials all and takes the first success.
- **`WithRetries(n)`** — retry a failed dial up to `n` extra times.
- **`WithTunnel(true)`** — wrap the connection in the `crypto` tunnel. The target must run the same protocol, and the tunnel is not authenticated; see Encryption.
- **`WithWebSocketTunnel(wsURL, cfg)`** — carries the chained connection inside a WebSocket, with ping/pong keepalive that closes dead peers.
- **`WithJitter(min, max)`** — waits a random time between hops to break timing patterns.
- **`WithFailClosed(true)`** — never fall back to a direct dial when no proxy is available.
- **`WithRoute(fn)`** — pick a proxy list per target host.
- **`crypto.Wrap(conn, initiator)`** — wrap a connection in X25519 + AES-256-GCM.
- **`evade.Pad` / `evade.FragmentWrite`** — hide payload sizes and write chunk sizes.
- **`config.Watch`** — reload settings automatically when the config file changes.
- **`dashboard.Serve`** — serve a small page showing which proxies are alive.

## Quick start

```go
import (
    "context"
    "github.com/wraith/proxychain/chain"
    "github.com/wraith/proxychain/hop"
)

p, _ := hop.New("http://user:pass@proxy.example.com:8080")
ch := chain.New([]*hop.Proxy{p}, chain.WithStrategy(chain.NewRandom()))

conn, err := ch.Dial(context.Background(), "tcp", "example.com:443")
if err != nil {
    // handle error
}
defer conn.Close()
```

## How chaining works

1. `Dial` asks the rotation strategy for an ordered list of hops.
2. Dead hops (circuit breaker open) are filtered; spares can replace them.
3. The first hop dials the second hop’s proxy address; each middle hop receives a `CONNECT` (or SOCKS5 request) for the next hop.
4. The final hop receives a `CONNECT` for the real target.
5. The returned `net.Conn` is the innermost tunnel. Optional wrappers (WebSocket, crypto tunnel, timing jitter, cover frames, pool, rate limiter) are applied on top.

Each `bufferedConn` keeps handshake bytes from bleeding into application reads. `Chain.Stop()` closes the health-probe goroutine and pooled connections; it is safe to call more than once.

## Options

| Option | Purpose |
|---|---|
| `WithStrategy(r)` | Rotation strategy |
| `WithJitter(min, max)` | Random inter-hop delay |
| `WithTimingJitter(min, max)` | Random delay before each application write |
| `WithCoverTraffic(cfg)` | Length-prefixed decoy frames on idle connections (both ends must enable) |
| `WithRetries(n)` | Extra dial attempts |
| `WithTunnel(bool)` | X25519 + AES-GCM tunnel to a peer running the same protocol (unauthenticated; see Encryption) |
| `WithWebSocketTunnel(rawURL, cfg)` | WebSocket transport with keepalive |
| `WithMetrics(m)` | Metrics recorder (adds per-hop stats when it implements `HopMetrics`) |
| `WithDialer(d)` | Raw TCP dialer for direct targets and proxy connections |
| `WithDoH()` | Shortcut for `WithDialer(dns.NewDialer())` |
| `WithPoolSize(n)` | Connection pool size |
| `WithRateLimit(bps)` | Built-in byte-rate throttle |
| `WithRateLimiter(rl)` | Custom/shared rate limiter |
| `WithSessionTTL(d)` | Sticky-session TTL |
| `WithSessionFile(path)` | Persist sessions to JSON |
| `WithSessionStore(s)` | Custom session persistence (e.g. Redis) |
| `WithHealthInterval(d)` | Background probe interval |
| `WithSpareProxies(spares)` | Self-healing replacement pool |
| `WithFailClosed(on)` | Disable direct dial when no proxies are available/routed |
| `WithRoute(fn)` | Pick a proxy list per target host |
| `GhostOptions()` | Preset slice combining jitter, retries, tunnel, rate limit, pool, probes |

## Extensibility

Defaults are in-memory or disabled; inject your own implementations for distributed setups.

- `SessionStore` — `Get(key) ([]*hop.Proxy, bool)` and `Set(key, order, ttl)`; inject with `WithSessionStore`.
- `RateLimiter` — `Wait(n)`; inject with `WithRateLimiter`.
- `Metrics` — `RecordDial(latency, err)`; optionally also `RecordHop(proxy, latency, err)` for per-hop stats; inject with `WithMetrics`.
- `hop.ContextDialer` — `DialContext(ctx, network, addr)`; inject with `WithDialer` (or `WithDoH()`).

## Strategies

- `NewRandom()` — shuffle hop order with crypto-randomness.
- `NewRoundRobin()` — cycle through hops.
- `NewWeighted(c)` — fewest failures first, then lowest latency.
- `NewLatency(c)` — probe hops in parallel, then sort by latency.

All strategies drop circuit-broken hops when healthy alternatives exist.

## Authentication

Proxy credentials go in the proxy URL:

```text
http://user:pass@proxy.example.com:8080
http://DOMAIN%5Cuser:pass@proxy.example.com:8080    # NTLM; DOMAIN optional, backslash written as %5C
socks5://user:pass@proxy.example.com:1080
```

- HTTP CONNECT supports Basic, Digest, and NTLMv2.
- HTTP/2 CONNECT (`h2`/`h2c`) supports Basic only.
- HTTPS proxies use TLS with browser-like fingerprints via uTLS.
- SOCKS5 supports username/password (RFC 1929) or no auth.

## Encryption (`crypto` package)

- `Wrap(conn, initiator)` — X25519 key exchange + AES-256-GCM frames.
- `WrapLayers(conn, initiator, n)` — nested `Wrap` tunnels between the same two endpoints. No hop peels a layer, so the layers add overhead rather than a second line of defense.
- `WrapKey(conn, key)` — use a pre-shared key (16, 24 or 32 bytes). Frame numbers restart at zero on every connection, so one key must not be reused across connections.
- `WrapPadded(conn, initiator, size)` — pads writes shorter than `size` with random bytes. The padding carries no length marker and is not removed on read, so it corrupts the stream for any receiver that does not strip it itself.
- `Rekey(conn, newKey)` — stack a new `WrapKey` tunnel on `conn`. There is no handshake, so both ends must switch at the same point in the stream.
- `DeriveKey(shared, label)` — HKDF-like key derivation.

`WithTunnel(true)` uses `Wrap` on each dial. The far end has to run `Wrap(conn, false)` itself; against a server that does not, the tunnel does not work. The tunnel has two known weaknesses:

- The key exchange is not authenticated. Anyone on the path, including every proxy in the chain, can sit in the middle and relay plaintext.
- Both ends derive one key and number frames from zero in each direction, so the same key and nonce protect frame *n* both ways. A passive observer can XOR the plaintexts. With `WrapKey` this also repeats across every connection.

Treat it as protection against passive eavesdropping only until it is replaced with an authenticated protocol.

## Evasion toolkit (`evade` package)

- `Jitter`, `RandomDelay` — timing randomization.
- `Pad` / `Unpad` — length padding.
- `FragmentWrite` — random-sized writes.
- `RandomSNI`, `RandomHeaders`, `RandomOrder` — fingerprint randomization.
- `DecoyBurst` — short-lived noise connections.
- `DomainFront` — returns its two arguments unchanged. It is a placeholder; nothing in the library performs domain fronting.
- `Canary` / `CanaryLeaked` — interception detection.
- `HoneypotIP` / `HoneypotProbe` — sinkhole and altered-traffic detection. `HoneypotChecker` supports configurable CIDR sets; `HoneypotProbeTimeout` adds a read deadline.

`chain.GhostOptions()` bundles jitter, retries, `WithTunnel(true)`, a rate limit, pooling, and frequent health probes. Because it enables the tunnel, it only works against a peer that runs `Wrap(conn, false)`.

Opt-in decoy helpers:

- `NewTimingConn` / `WithTimingJitter` — jitter around application writes, not only proxy handshakes. Bytes are unchanged.
- `NewCoverConn` / `WithCoverTraffic` — length-prefixed cover frames between real writes. Both ends must wrap; the peer strips cover frames before handing bytes to the application. `CoverConfig` sets interval and payload size.
- `BenignRequests` — shuffled harmless GETs through an HTTP client. Give it an HTTP/2 transport for h2 sequencing.
- `DecoyDNS` — shuffled lookups through a `dns.Resolver` (or any `ResolveContext` implementation) to blend resolver traffic.
- `ConnectionChurn` — short-lived TCP or TLS handshakes to benign hosts.
- `BrowserHeaders(profile)` — browser-consistent header order and casing for manual CONNECT writers.
- `RegionForHost` / `FilterByRegion` — ccTLD-based region hints for proxy selection. This is a heuristic: it cannot tell where an exit actually terminates.

These helpers cost bandwidth and can break protocol state if applied blindly. Keep them opt-in.

## DNS (`dns` package)

- `Resolver` — multi-endpoint DoH resolver (Cloudflare primary, Google fallback), 5s timeout, and a 1-minute positive cache. `NewResolver()` takes no arguments; for custom endpoints set the exported `Endpoints` field.
- `Resolve(host)` / `ResolveContext(ctx, host)` — query the default resolver.
- `DialContext(ctx, network, addr)` — resolve via DoH, then dial the returned IP.
- `NewDialer()` — a `hop.ContextDialer` that performs DoH resolution before dialing.

Wire it into the chain with `chain.WithDoH()` (or `chain.WithDialer(dns.NewDialer())`). It then resolves direct targets and the hostnames of hops configured at construction via DoH. Resolution failures fall back to the system resolver; hops supplied by `WithRoute` or restored from a session are not covered.

## Config

YAML (JSON is a YAML subset):

```yaml
name: chain
hops:
  - url: http://user:pass@proxy1:8080
  - url: socks5://proxy2:1080
strategy: random
session_ttl: 5m
jitter_min: 50ms
jitter_max: 300ms
```

Env: `PROXY_CHAIN="http://p1,http://p2"`.

Hot reload: `config.Watch(path, onChange, stop)`.

## Binaries

```sh
go run ./cmd/proxychain list
go run ./cmd/proxychain ping example.com:80

go run ./cmd/proxyd -addr :8080 -config chain.yaml
go run ./cmd/proxyd -addr :8080 -config chain.yaml -proxyproto
```

- `proxychain` — list chains or ping a target through them.
- `proxyd` — HTTP CONNECT server that forwards through a configured chain. `-proxyproto` requires a PROXY v2 header on every inbound connection. The client address it carries is not used yet, and the header does not match the HAProxy spec.

## Dashboard

```go
dashboard.Serve(":8099", ch)
```

Serves `/health` JSON and a small HTML auto-refresh view.

## Advanced roadmap

- Done: uTLS fingerprint randomization for HTTPS proxies
- Done: HTTP/2 CONNECT (`h2`/`h2c`) via `golang.org/x/net/http2`
- Done: SOCKS5 UDP ASSOCIATE (standalone `hop.DialSocks5UDP`, single hop)
- Partial: PROXY protocol v2 (`proxyproto` + `proxyd -proxyproto`); the signature does not match the spec yet
- Done: NTLMv2 proxy auth (HTTP CONNECT; HTTP/2 CONNECT stays Basic-only)
- Done: WebSocket tunnel with keepalive (`ws` + `chain.WithWebSocketTunnel`)
- Done: Multipath routing (`chain.MultiChain`: failover/stripe/fastest/race)
- Not done: distributed Redis session store (interface exists: `SessionStore`)
- Not done: gRPC/HTTP2 multiplexing
- Not done: ML-based adaptive routing
- Not done: eBPF kernel interception

## Examples

Full index with links, environment variables, and use cases: [`examples/README.md`](examples/README.md).

Runnable examples live in `examples/`:

- `basic` — single hop
- `multihop` — chain multiple proxies with tunneling
- `rotation` — random / round-robin / weighted
- `encryption` — Wrap, WrapLayers, WrapPadded, WrapKey
- `failover` — spare-pool self-healing
- `multipath` — combine chains with a multipath policy
- `websocket` — WebSocket tunnel with keepalive
- `doh` — DoH-backed dialing via `WithDialer`
- `pooling` — connection reuse
- `sticky` — session stickiness
- `ghost` — max-evasion preset
- `config` — YAML/env loading
- `dashboard` — health dashboard
- `metrics` — atomic metrics + HTTP handler
- `dns` — DoH resolution
- `evade` — evasion toolkit
- `decoy` — timing, cover frames, benign requests, region hints
- `honeypot` — CIDR sinkhole checks + deadline canary probe
- `proxyproto` — HAProxy PROXY v2 header write/read and listener override
- `usage` — end-to-end usage
- `residentialchain` — tiered datacenter → residential
- `livefeatures` — live feature sweep against your own proxy CSV
- `csvtest` / `e2etest` / `advancedtest` — proxy CSV tests

`csvtest`, `e2etest`, `livefeatures`, and `advancedtest` read `PROXIES_CSV` (or `PROXY_URL`). No proxy credentials or local paths are stored in the repo.

## Testing

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...
```

`chain/stress_test.go` runs mixed hop counts (1/2/3/5/8) across all strategies with spares, pooling and jitter enabled, and passes as long as no more than 10 of its 20 concurrent dials fail; it does not assert dead-hop filtering, spare replacement or pool reuse. `chain/bench_test.go` contains dial benchmarks. One fuzz target covers `hop.New`. Unit tests exercise the HTTP CONNECT, NTLM, HTTP/2 (`h2c`) and UDP ASSOCIATE handshakes, the `crypto.Wrap` round trip, several `evade` helpers, config validation and the metrics snapshot. There are no tests yet for SOCKS5 TCP, HTTPS hops, fail-closed, routing, sessions, rate limiting, hot reload, the dashboard or the two binaries.

## License

MIT
