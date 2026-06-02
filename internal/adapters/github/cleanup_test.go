package github

import (
	"log/slog"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewCleaner(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	tests := []struct {
		name    string
		repo    string
		config  *StaleLabelCleanupConfig
		wantErr bool
	}{
		{
			name: "valid repo format",
			repo: "owner/repo",
			config: &StaleLabelCleanupConfig{
				Enabled:   true,
				Interval:  30 * time.Minute,
				Threshold: 1 * time.Hour,
			},
			wantErr: false,
		},
		{
			name: "invalid repo format - no slash",
			repo: "ownerrepo",
			config: &StaleLabelCleanupConfig{
				Enabled: true,
			},
			wantErr: true,
		},
		{
			name: "invalid repo format - multiple slashes",
			repo: "owner/repo/extra",
			config: &StaleLabelCleanupConfig{
				Enabled: true,
			},
			wantErr: true,
		},
		{
			name: "invalid repo format - empty",
			repo: "",
			config: &StaleLabelCleanupConfig{
				Enabled: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(testutil.FakeGitHubToken)
			cleaner, err := NewCleaner(client, store, tt.repo, tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewCleaner() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if cleaner == nil {
					t.Fatal("NewCleaner returned nil")
				}
				if cleaner.client != client {
					t.Error("cleaner.client not set correctly")
				}
				if cleaner.store != store {
					t.Error("cleaner.store not set correctly")
				}
			}
		})
	}
}

func TestNewCleaner_DefaultValues(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	client := NewClient(testutil.FakeGitHubToken)

	// Test with zero values - should use defaults
	config := &StaleLabelCleanupConfig{
		Enabled:   true,
		Interval:  0, // Should default to 30m
		Threshold: 0, // Should default to 1h
	}

	cleaner, err := NewCleaner(client, store, "owner/repo", config)
	if err != nil {
		t.Fatalf("NewCleaner() error = %v", err)
	}

	if cleaner.interval != 30*time.Minute {
		t.Errorf("cleaner.interval = %v, want 30m", cleaner.interval)
	}
	if cleaner.threshold != 1*time.Hour {
		t.Errorf("cleaner.threshold = %v, want 1h", cleaner.threshold)
	}
}

func TestWithCleanerLogger(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	client := NewClient(testutil.FakeGitHubToken)
	customLogger := slog.Default()

	cleaner, err := NewCleaner(client, store, "owner/repo",
		&StaleLabelCleanupConfig{Enabled: true},
		WithCleanerLogger(customLogger),
	)
	if err != nil {
		t.Fatalf("NewCleaner() error = %v", err)
	}

	if cleaner.logger != customLogger {
		t.Error("custom logger should be set")
	}
}

func TestStaleLabelCleanupConfig_InDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.StaleLabelCleanup == nil {
		t.Fatal("StaleLabelCleanup should be set in default config")
	}

	if !cfg.StaleLabelCleanup.Enabled {
		t.Error("StaleLabelCleanup.Enabled should be true by default")
	}

	if cfg.StaleLabelCleanup.Interval != 30*time.Minute {
		t.Errorf("StaleLabelCleanup.Interval = %v, want 30m", cfg.StaleLabelCleanup.Interval)
	}

	if cfg.StaleLabelCleanup.Threshold != 1*time.Hour {
		t.Errorf("StaleLabelCleanup.Threshold = %v, want 1h", cfg.StaleLabelCleanup.Threshold)
	}

	if cfg.StaleLabelCleanup.FailedThreshold != 24*time.Hour {
		t.Errorf("StaleLabelCleanup.FailedThreshold = %v, want 24h", cfg.StaleLabelCleanup.FailedThreshold)
	}
}

func TestCleaner_FailedThreshold_DefaultValue(t *testing.T) {
	store := createTestStore(t)
	defer func() { _ = store.Close() }()

	client := NewClient(testutil.FakeGitHubToken)

	// Test with zero FailedThreshold - should default to 24h
	config := &StaleLabelCleanupConfig{
		Enabled:         true,
		Interval:        30 * time.Minute,
		Threshold:       1 * time.Hour,
		FailedThreshold: 0, // Should default to 24h
	}

	cleaner, err := NewCleaner(client, store, "owner/repo", config)
	if err != nil {
		t.Fatalf("NewCleaner() error = %v", err)
	}

	if cleaner.failedThreshold != 24*time.Hour {
		t.Errorf("cleaner.failedThreshold = %v, want 24h", cleaner.failedThreshold)
	}
}
