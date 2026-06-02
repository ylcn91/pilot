package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Helper functions

func printStatus(name string, configured bool) {
	if configured {
		fmt.Printf("  ✓ %s\n", name)
	} else {
		fmt.Printf("  ○ %s (not configured)\n", name)
	}
}

func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func readYesNo(reader *bufio.Reader, defaultYes bool) bool {
	line := readLine(reader)
	if line == "" {
		return defaultYes
	}
	line = strings.ToLower(line)
	return line == "y" || line == "yes"
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	return path
}

func validateTelegramToken(token string) error {
	// Simple validation - check format
	if !strings.Contains(token, ":") {
		return fmt.Errorf("invalid token format")
	}
	// Could add actual API call validation here
	return nil
}
