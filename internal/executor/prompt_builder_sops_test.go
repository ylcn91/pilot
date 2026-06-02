package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRelevantSOPs(t *testing.T) {
	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "pilot-test-sops")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	agentDir := filepath.Join(tempDir, ".agent")
	sopsDir := filepath.Join(agentDir, "sops")

	// Create nested directory structure
	dirs := []string{
		filepath.Join(sopsDir, "debugging"),
		filepath.Join(sopsDir, "integrations"),
		filepath.Join(sopsDir, "development"),
	}

	for _, dir := range dirs {
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			t.Fatalf("Failed to create dir %s: %v", dir, err)
		}
	}

	// Create test SOP files
	testFiles := map[string]string{
		"debugging/sqlite-busy.md":     "SQLite debugging guide",
		"integrations/github-api.md":   "GitHub API integration",
		"integrations/telegram-bot.md": "Telegram bot setup",
		"development/testing-guide.md": "Testing guidelines",
		"database-migrations.md":       "Database migration SOP",
	}

	for filePath, content := range testFiles {
		fullPath := filepath.Join(sopsDir, filePath)
		err = os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to write test file %s: %v", fullPath, err)
		}
	}

	tests := []struct {
		name        string
		description string
		expected    []string // Expected SOP paths to be found
	}{
		{
			name:        "SQLite task",
			description: "Fix SQLite database connection issues",
			expected:    []string{"sops/debugging/sqlite-busy.md"},
		},
		{
			name:        "GitHub integration task",
			description: "Add GitHub API webhook handler",
			expected:    []string{"sops/integrations/github-api.md"},
		},
		{
			name:        "Telegram bot task",
			description: "Update Telegram bot message handling",
			expected:    []string{"sops/integrations/telegram-bot.md"},
		},
		{
			name:        "Testing task",
			description: "Add unit tests for authentication module",
			expected:    []string{"sops/development/testing-guide.md"},
		},
		{
			name:        "Database task",
			description: "Create database migration for user table",
			expected:    []string{"sops/database-migrations.md"},
		},
		{
			name:        "No matching SOPs",
			description: "Update frontend styling",
			expected:    []string{}, // No matches expected
		},
		{
			name:        "Multiple matches",
			description: "Debug GitHub API integration tests",
			expected: []string{
				"sops/integrations/github-api.md",
				"sops/development/testing-guide.md",
			}, // Should match both github and testing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findRelevantSOPs(agentDir, tt.description)

			if len(tt.expected) == 0 {
				if len(result) > 0 {
					t.Errorf("Expected no SOPs, but got: %v", result)
				}
				return
			}

			// Check that we got some results when expected
			if len(result) == 0 {
				t.Errorf("Expected SOPs %v, but got none", tt.expected)
				return
			}

			// Check that expected SOPs are present
			for _, expectedSOP := range tt.expected {
				found := false
				for _, resultSOP := range result {
					if resultSOP == expectedSOP {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected SOP %s not found in results: %v", expectedSOP, result)
				}
			}
		})
	}
}

func TestFindRelevantSOPsNoDirectory(t *testing.T) {
	// Test with non-existent .agent directory
	tempDir, err := os.MkdirTemp("", "pilot-test-no-agent")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	agentDir := filepath.Join(tempDir, ".agent")
	// Don't create the directory

	result := findRelevantSOPs(agentDir, "test description")
	if len(result) > 0 {
		t.Errorf("Expected no SOPs for missing directory, got: %v", result)
	}
}

func TestExtractTaskKeywords(t *testing.T) {
	tests := []struct {
		name        string
		description string
		expected    []string
	}{
		{
			name:        "Database task",
			description: "Fix SQLite database connection timeout",
			expected:    []string{"sqlite", "database"},
		},
		{
			name:        "API integration",
			description: "Add GitHub API webhook integration",
			expected:    []string{"github", "api", "webhook", "integration"},
		},
		{
			name:        "Testing task",
			description: "Write unit tests for authentication module",
			expected:    []string{"test", "auth", "authentication"},
		},
		{
			name:        "Case insensitive",
			description: "Update TELEGRAM bot with OAuth support",
			expected:    []string{"telegram", "auth", "oauth"},
		},
		{
			name:        "No keywords",
			description: "Update README file styling",
			expected:    []string{},
		},
		{
			name:        "Multiple matches",
			description: "Debug Docker container in Kubernetes CI pipeline",
			expected:    []string{"docker", "kubernetes", "ci", "pipeline", "debug"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTaskKeywords(tt.description)

			if len(tt.expected) != len(result) {
				t.Errorf("Expected %d keywords, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			// Check all expected keywords are present
			for _, expected := range tt.expected {
				found := false
				for _, keyword := range result {
					if keyword == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected keyword %s not found in result: %v", expected, result)
				}
			}
		})
	}
}
