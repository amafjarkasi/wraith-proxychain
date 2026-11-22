package config

import "testing"

func TestValidate(t *testing.T) {
	c := &ChainConfig{Hops: []HopConfig{{URL: "http://a"}, {URL: "http://b"}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c2 := &ChainConfig{Hops: []HopConfig{{URL: "http://a"}, {URL: "http://a"}}}
	if err := c2.Validate(); err == nil {
		t.Fatal("expected duplicate error")
	}
	c3 := &ChainConfig{}
	if err := c3.Validate(); err == nil {
		t.Fatal("expected empty error")
	}
}
