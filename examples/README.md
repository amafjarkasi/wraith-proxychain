# Examples index

Run from the repo root:

```powershell
go run ./examples/<name>
```

Anything marked **live** needs real proxies via an environment variable; the rest use placeholders or local servers and run offline. Never commit real credentials or proxy lists.

## Start here

| Example | Source | Env / args | Use case |
|---|---|---|---|
| usage | [main.go](usage/main.go) | none | Smallest program: ghost options, metrics, direct dial. Start here. |
| basic | [main.go](basic/main.go) | edit placeholder URL | Single HTTP CONNECT proxy with retries and a deadline. |
| advancedtest | [main.go](advancedtest/main.go) | `PROXY_URL` | Real proxy with tunnel, metrics, jitter, retries. |
| config | [main.go](config/main.go) | `config.yaml` or `PROXY_CHAIN` | Load from YAML, fall back to env, build a chain. |
| csvtest | [main.go](csvtest/main.go) | `PROXIES_CSV` | Load `Type,Host,Port,User,Pass` CSV, test single/2-hop and egress IP. |
| livefeatures | [main.go](livefeatures/main.go) | `PROXIES_CSV` | **Live** — all 8 features against real proxies, with pass/fail per case. |
| e2etest | [main.go](e2etest/main.go) | `PROXIES_CSV` | **Live** — 3-hop chain, canary check, dashboard health probe. |

## Chaining and routing

| Example | Source | Env / args | Use case |
|---|---|---|---|
| multihop | [main.go](multihop/main.go) | edit placeholder URLs | Mixed HTTP + SOCKS5 + HTTP chain, jitter, retries, tunnel. |
| residentialchain | [main.go](residentialchain/main.go) | `PROXY_ENTRY`, `PROXY_RESIDENTIAL` | Tiered datacenter entry → sticky residential exit. |
| rotation | [main.go](rotation/main.go) | edit placeholder URLs | Compare random / roundrobin / weighted strategies. |
| sticky | [main.go](sticky/main.go) | edit placeholder URLs | `SessionKey` keeps hop order stable until TTL. |
| failover | [main.go](failover/main.go) | edit placeholder URLs | Dead hop + spare proxy; circuit breaker self-heals. |
| pooling | [main.go](pooling/main.go) | edit placeholder URL | `WithPoolSize(8)` reuses connections; `Close` returns them. |
| multipath | [main.go](multipath/main.go) | `PROXY_CHAIN` (≥2 URLs) | `MultiChain` with failover/stripe/fastest/race policies. |

## Encryption and tunnels

| Example | Source | Env / args | Use case |
|---|---|---|---|
| encryption | [main.go](encryption/main.go) | none | `Wrap`, `WrapLayers`, `WrapPadded`, `WrapKey` over a raw socket. |
| websocket | [main.go](websocket/main.go) | none | Local WS echo server + `WithWebSocketTunnel` ping/pong keepalive. |

## Observability, DNS, evasion

| Example | Source | Env / args | Use case |
|---|---|---|---|
| metrics | [main.go](metrics/main.go) | none | Serve aggregate + per-hop counters on `:9090`. |
| dashboard | [main.go](dashboard/main.go) | none | Serve HTML health dashboard on `:8099`. |
| dns | [main.go](dns/main.go) | none | `dns.Resolve` + `dns.DialContext` DoH helper. |
| doh | [main.go](doh/main.go) | `PROXY_CHAIN` (optional), target arg | Dial through a chain using `dns.NewDialer()`; verify egress IP. |
| evade | [main.go](evade/main.go) | none | Jitter, padding, random headers/SNI, canary tokens. |
| decoy | [main.go](decoy/main.go) | `DECOY_DNS=1` (optional) | Write jitter, framed cover traffic, benign request sequencing, region hints. |
| ghost | [main.go](ghost/main.go) | edit placeholder URL | Apply `chain.GhostOptions()` plus metrics. |
| honeypot | [main.go](honeypot/main.go) | none | CIDR-based honeypot detection + deadline-bounded canary probe. |

## Infrastructure

| Example | Source | Env / args | Use case |
|---|---|---|---|
| proxyproto | [main.go](proxyproto/main.go) | none | HAProxy PROXY v2 header write/read and listener addr override. |

## Adding an example

1. Create `examples/<name>/main.go` with `package main`.
2. Read config from `os.Getenv`, never hardcode credentials.
3. Add a row to the matching table above and to the README examples list.
4. Verify with `go run ./examples/<name>` and `go build ./...`.
