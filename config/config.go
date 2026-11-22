package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type HopConfig struct {
	URL string `yaml:"url"`
}

type ChainConfig struct {
	Name       string      `yaml:"name"`
	Hops       []HopConfig `yaml:"hops"`
	Strategy   string      `yaml:"strategy"`
	SessionTTL string      `yaml:"session_ttl"`
	JitterMin  string      `yaml:"jitter_min"`
	JitterMax  string      `yaml:"jitter_max"`
}

func Load(path string) (*ChainConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c ChainConfig
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func FromEnv() *ChainConfig {
	raw := os.Getenv("PROXY_CHAIN")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	hops := make([]HopConfig, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			hops = append(hops, HopConfig{URL: p})
		}
	}
	c := &ChainConfig{Hops: hops, Strategy: "random"}
	_ = c.Validate()
	return c
}

func (c *ChainConfig) Validate() error {
	if len(c.Hops) == 0 {
		return fmt.Errorf("config: no hops")
	}
	for i := 1; i < len(c.Hops); i++ {
		if c.Hops[i].URL == c.Hops[i-1].URL {
			return fmt.Errorf("config: duplicate adjacent hop %s", c.Hops[i].URL)
		}
	}
	if c.Strategy == "" {
		c.Strategy = "random"
	}
	return nil
}
