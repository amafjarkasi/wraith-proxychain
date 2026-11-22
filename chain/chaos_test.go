package chain

import "testing"

func TestChaosDial(t *testing.T) {
	c := New(nil)
	conn, err := c.Dial(t.Context(), "tcp", "127.0.0.1:1")
	if err == nil {
		conn.Close()
	}
}
