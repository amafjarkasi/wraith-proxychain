package hop

import "testing"

func FuzzNew(f *testing.F) {
	f.Add("http://user:pass@host:8080")
	f.Add("socks5://127.0.0.1:1080")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = New(s)
	})
}
