package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		err := Init(nil)
		if err != nil {
			t.Fatalf("Init(nil) failed: %v", err)
		}
	})

	t.Run("json format", func(t *testing.T) {
		err := Init(&Config{
			Level:  "debug",
			Format: "json",
			Output: "stdout",
		})
		if err != nil {
			t.Fatalf("Init failed: %v", err)
		}
	})

	t.Run("text format", func(t *testing.T) {
		err := Init(&Config{
			Level:  "info",
			Format: "text",
			Output: "stderr",
		})
		if err != nil {
			t.Fatalf("Init failed: %v", err)
		}
	})
}

func TestFileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	err := Init(&Config{
		Level:  "info",
		Format: "text",
		Output: logFile,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	Info("test file output")

	// Give a moment for async operations
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "test file output") {
		t.Errorf("log file does not contain expected message")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Level != "info" {
		t.Errorf("expected level=info, got %s", cfg.Level)
	}
	if cfg.Format != "text" {
		t.Errorf("expected format=text, got %s", cfg.Format)
	}
	if cfg.Output != "stdout" {
		t.Errorf("expected output=stdout, got %s", cfg.Output)
	}
}

func TestLogger(t *testing.T) {
	// Initialize with known config
	err := Init(&Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	logger := Logger()
	if logger == nil {
		t.Error("Logger() returned nil")
	}
}

func TestInitWithDebugLevel(t *testing.T) {
	// Test that debug level enables AddSource option
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "debug.log")

	err := Init(&Config{
		Level:  "debug",
		Format: "json",
		Output: logFile,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	Debug("debug message with source")

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	// Debug level should include source information
	if !strings.Contains(string(content), "source") {
		t.Errorf("expected source info in debug log, content: %s", content)
	}
}

func TestInitWithEmptyOutput(t *testing.T) {
	// Empty output should default to stdout
	err := Init(&Config{
		Level:  "info",
		Format: "text",
		Output: "",
	})
	if err != nil {
		t.Fatalf("Init with empty output failed: %v", err)
	}
}
