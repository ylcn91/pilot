package logging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetWriter(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		check  func(t *testing.T, err error)
	}{
		{
			name: "stdout",
			config: &Config{
				Output: "stdout",
			},
			check: func(t *testing.T, err error) {
				if err != nil {
					t.Errorf("unexpected error for stdout: %v", err)
				}
			},
		},
		{
			name: "stderr",
			config: &Config{
				Output: "stderr",
			},
			check: func(t *testing.T, err error) {
				if err != nil {
					t.Errorf("unexpected error for stderr: %v", err)
				}
			},
		},
		{
			name: "empty defaults to stdout",
			config: &Config{
				Output: "",
			},
			check: func(t *testing.T, err error) {
				if err != nil {
					t.Errorf("unexpected error for empty output: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getWriter(tt.config)
			tt.check(t, err)
		})
	}
}

func TestInitWithRotation(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "rotated.log")

	err := Init(&Config{
		Level:  "info",
		Format: "json",
		Output: logFile,
		Rotation: &RotationConfig{
			MaxSize:    "1MB",
			MaxAge:     "7d",
			MaxBackups: 3,
		},
	})
	if err != nil {
		t.Fatalf("Init with rotation failed: %v", err)
	}

	Info("test with rotation config")

	// Verify file was created
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("expected log file to be created")
	}
}
