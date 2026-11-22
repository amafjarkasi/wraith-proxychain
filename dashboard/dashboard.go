package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/wraith/proxychain/chain"
	"github.com/wraith/proxychain/hop"
)

// Serve starts an HTTP dashboard showing chain health.
func Serve(addr string, c *chain.Chain) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>wraith dashboard</title>
<style>body{font-family:sans-serif;background:#111;color:#eee;padding:20px} .card{background:#222;padding:10px;margin:10px;border-radius:8px}</style>
</head><body><h1>wraith dashboard</h1><div id="h"></div>
<script>async function load(){const r=await fetch('/health');const d=await r.json();document.getElementById('h').innerHTML=JSON.stringify(d,null,2)};load();setInterval(load,5000);</script></body></html>`)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		info := map[string]any{
			"time": time.Now().Format(time.RFC3339),
			"hops": len(c.Hops),
		}
		info["hops_detail"] = c.HopStats()
		json.NewEncoder(w).Encode(info)
	})
	return http.ListenAndServe(addr, mux)
}

var _ = hop.Proxy{}
