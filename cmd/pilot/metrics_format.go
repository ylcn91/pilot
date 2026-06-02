package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Helper functions

func formatDuration(ms int64) string {
	if ms == 0 {
		return "0s"
	}

	d := time.Duration(ms) * time.Millisecond

	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

func formatDurationShort(ms int64) string {
	if ms == 0 {
		return "0s"
	}

	d := time.Duration(ms) * time.Millisecond

	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

func formatTokens(tokens int64) string {
	if tokens == 0 {
		return "0"
	}
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens < 1_000_000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%.2fM", float64(tokens)/1_000_000)
}

func formatTokensShort(tokens int64) string {
	if tokens == 0 {
		return "0"
	}
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens < 1_000_000 {
		return fmt.Sprintf("%dK", tokens/1000)
	}
	return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
}

func shortenPath(path string) string {
	home, _ := os.UserHomeDir()
	if home != "" && len(path) > len(home) && path[:len(home)] == home {
		return "~" + path[len(home):]
	}
	// Just show the last 2 components
	parts := []string{}
	for path != "" && path != "/" {
		dir := filepath.Base(path)
		parts = append([]string{dir}, parts...)
		path = filepath.Dir(path)
		if len(parts) >= 2 {
			break
		}
	}
	if len(parts) > 0 {
		return ".../" + filepath.Join(parts...)
	}
	return path
}
