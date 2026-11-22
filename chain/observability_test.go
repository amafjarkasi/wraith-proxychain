package chain

import (
	"testing"
	"time"

	"github.com/wraith/proxychain/dns"
	"github.com/wraith/proxychain/hop"
)

type hopRecorder struct {
	proxy string
	lat   time.Duration
	err   error
}

func (r *hopRecorder) RecordDial(time.Duration, error) {}
func (r *hopRecorder) RecordHop(proxy string, lat time.Duration, err error) {
	r.proxy, r.lat, r.err = proxy, lat, err
}

func TestWithDoH(t *testing.T) {
	ch := New(nil, WithDoH())
	if _, ok := ch.dialer.(*dns.Dialer); !ok {
		t.Fatalf("dialer = %T", ch.dialer)
	}
}

func TestHopMetricsIntegration(t *testing.T) {
	h, err := hop.New("http://proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	rec := &hopRecorder{}
	ch := New([]*hop.Proxy{h}, WithMetrics(rec))
	ch.recordSuccess(h, 42*time.Millisecond)
	if rec.proxy != "http://proxy.example:8080" || rec.lat != 42*time.Millisecond || rec.err != nil {
		t.Fatalf("success record: %+v", rec)
	}
	ch.recordFailure(h)
	if rec.err == nil {
		t.Fatal("failure not recorded")
	}
}
