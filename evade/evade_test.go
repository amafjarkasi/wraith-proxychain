package evade

import "testing"

func TestPadUnpad(t *testing.T) {
	b := []byte("secret")
	p := Pad(b, 8)
	u, err := Unpad(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(u) != string(b) {
		t.Fatalf("got %q", u)
	}
}
