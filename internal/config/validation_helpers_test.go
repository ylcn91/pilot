package config

import (
	"github.com/ylcn91/pilot/internal/gateway"
)

// baseValidConfig returns a minimal valid config for testing
func baseValidConfig() *Config {
	return &Config{
		Gateway: &gateway.Config{
			Host: "127.0.0.1",
			Port: 9091,
		},
		Projects: []*ProjectConfig{
			{Name: "test", Path: "/tmp/test"},
		},
	}
}
