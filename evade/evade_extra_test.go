package evade

import "testing"

func TestCanary(t *testing.T) {
	tok := Canary()
	if tok == "" {
		t.Fatal("empty canary")
	}
	if !CanaryLeaked("x"+tok+"y", tok) {
		t.Fatal("should leak")
	}
	if CanaryLeaked("nope", tok) {
		t.Fatal("false positive")
	}
}

func TestHoneypotIP(t *testing.T) {
	if !HoneypotIP("198.51.100.5") {
		t.Fatal("expected honepot")
	}
	if HoneypotIP("8.8.8.8") {
		t.Fatal("false positive")
	}
}
