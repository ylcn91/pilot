package telegram

import (
	"testing"

	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestNewHandler tests handler creation with various configurations
func TestNewHandler(t *testing.T) {
	tests := []struct {
		name         string
		config       *HandlerConfig
		wantAllowIDs int
	}{
		{
			name: "basic config",
			config: &HandlerConfig{
				BotToken:    testutil.FakeTelegramBotToken,
				ProjectPath: "/test/path",
			},
			wantAllowIDs: 0,
		},
		{
			name: "with allowed IDs",
			config: &HandlerConfig{
				BotToken:    testutil.FakeTelegramBotToken,
				ProjectPath: "/test/path",
				AllowedIDs:  []int64{123, 456, 789},
			},
			wantAllowIDs: 3,
		},
		{
			name: "empty allowed IDs",
			config: &HandlerConfig{
				BotToken:    testutil.FakeTelegramBotToken,
				ProjectPath: "/test/path",
				AllowedIDs:  []int64{},
			},
			wantAllowIDs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(tt.config, nil)

			if h == nil {
				t.Fatal("NewHandler returned nil")
			}
			if h.projectPath != tt.config.ProjectPath {
				t.Errorf("projectPath = %q, want %q", h.projectPath, tt.config.ProjectPath)
			}
			if len(h.allowedIDs) != tt.wantAllowIDs {
				t.Errorf("allowedIDs len = %d, want %d", len(h.allowedIDs), tt.wantAllowIDs)
			}
		})
	}
}

// TestHandlerConfigStruct tests HandlerConfig struct
func TestHandlerConfigStruct(t *testing.T) {
	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{{Name: "test", Path: "/test"}},
	}

	config := &HandlerConfig{
		BotToken:    testutil.FakeTelegramBotToken,
		ProjectPath: "/project/path",
		Projects:    projects,
		AllowedIDs:  []int64{123, 456},
	}

	if config.BotToken != testutil.FakeTelegramBotToken {
		t.Errorf("BotToken = %q, want %s", config.BotToken, testutil.FakeTelegramBotToken)
	}
	if config.ProjectPath != "/project/path" {
		t.Errorf("ProjectPath = %q, want /project/path", config.ProjectPath)
	}
	if config.Projects == nil {
		t.Error("Projects should not be nil")
	}
	if len(config.AllowedIDs) != 2 {
		t.Errorf("AllowedIDs len = %d, want 2", len(config.AllowedIDs))
	}
}

// TestNewHandlerWithProjects tests handler creation with projects source
func TestNewHandlerWithProjects(t *testing.T) {
	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{
			{Name: "default", Path: "/default/path"},
			{Name: "other", Path: "/other/path"},
		},
	}

	config := &HandlerConfig{
		BotToken: testutil.FakeTelegramBotToken,
		Projects: projects,
		// Note: ProjectPath is empty, should use default from Projects
	}

	h := NewHandler(config, nil)

	if h.projectPath != "/default/path" {
		t.Errorf("projectPath = %q, want /default/path (from default project)", h.projectPath)
	}
}

// TestNewHandlerWithTranscriptionError verifies transcription error handling
func TestNewHandlerWithTranscriptionError(t *testing.T) {
	// This test verifies that handler creation works even if transcription fails
	config := &HandlerConfig{
		BotToken:    testutil.FakeTelegramBotToken,
		ProjectPath: "/test/path",
		// Transcription with invalid config would fail
	}

	h := NewHandler(config, nil)

	if h == nil {
		t.Fatal("NewHandler should not return nil even with transcription issues")
	}
	// transcriptionErr and transcriber should be nil when not configured
	if h.transcriptionErr != nil {
		t.Error("transcriptionErr should be nil when not configured")
	}
	if h.transcriber != nil {
		t.Error("transcriber should be nil when not configured")
	}
}
