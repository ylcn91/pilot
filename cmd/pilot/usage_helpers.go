package main

import (
	"fmt"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/memory"
)

// openStore is a helper to open the memory store
func openStore() (*memory.Store, error) {
	configPath := cfgFile
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	store, err := memory.NewStore(cfg.Memory.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open memory store: %w", err)
	}

	return store, nil
}

// formatBytes formats bytes as human-readable string
func formatBytes(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
}
