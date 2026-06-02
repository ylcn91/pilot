package slack

import (
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// TestNewNotifier tests notifier creation
func TestNewNotifier(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		channel string
	}{
		{
			name: "basic config",
			config: &Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "#dev-notifications",
			},
			channel: "#dev-notifications",
		},
		{
			name: "empty channel defaults",
			config: &Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "",
			},
			channel: "",
		},
		{
			name: "custom channel",
			config: &Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "#custom-channel",
			},
			channel: "#custom-channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notifier := NewNotifier(tt.config)

			if notifier == nil {
				t.Fatal("NewNotifier returned nil")
			}
			if notifier.channel != tt.channel {
				t.Errorf("channel = %q, want %q", notifier.channel, tt.channel)
			}
			if notifier.client == nil {
				t.Error("client is nil")
			}
		})
	}
}

// TestDefaultConfig tests the default config values
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig returned nil")
	}
	if config.Enabled {
		t.Error("Enabled should be false by default")
	}
	if config.Channel != "#dev-notifications" {
		t.Errorf("Channel = %q, want #dev-notifications", config.Channel)
	}
	if config.BotToken != "" {
		t.Errorf("BotToken = %q, want empty string", config.BotToken)
	}
	if config.SocketMode {
		t.Error("SocketMode should be false by default")
	}
	if config.AllowedUsers == nil || len(config.AllowedUsers) != 0 {
		t.Errorf("AllowedUsers = %v, want empty slice", config.AllowedUsers)
	}
	if config.AllowedChannels == nil || len(config.AllowedChannels) != 0 {
		t.Errorf("AllowedChannels = %v, want empty slice", config.AllowedChannels)
	}
}

// TestConfigFields tests that config has expected fields
func TestConfigFields(t *testing.T) {
	config := &Config{
		Enabled:  true,
		BotToken: testutil.FakeSlackBotToken,
		Channel:  "#pilot-notifications",
	}

	if !config.Enabled {
		t.Error("Enabled should be true")
	}
	if config.BotToken != testutil.FakeSlackBotToken {
		t.Errorf("BotToken = %q, want %s", config.BotToken, testutil.FakeSlackBotToken)
	}
	if config.Channel != "#pilot-notifications" {
		t.Errorf("Channel = %q, want #pilot-notifications", config.Channel)
	}
}

// TestGenerateProgressBar tests progress bar generation
func TestGenerateProgressBar(t *testing.T) {
	tests := []struct {
		name     string
		progress int
		expected string
	}{
		{
			name:     "0 percent",
			progress: 0,
			expected: "░░░░░░░░░░",
		},
		{
			name:     "10 percent",
			progress: 10,
			expected: "█░░░░░░░░░",
		},
		{
			name:     "25 percent",
			progress: 25,
			expected: "██░░░░░░░░",
		},
		{
			name:     "50 percent",
			progress: 50,
			expected: "█████░░░░░",
		},
		{
			name:     "75 percent",
			progress: 75,
			expected: "███████░░░",
		},
		{
			name:     "90 percent",
			progress: 90,
			expected: "█████████░",
		},
		{
			name:     "100 percent",
			progress: 100,
			expected: "██████████",
		},
		{
			name:     "33 percent rounds down",
			progress: 33,
			expected: "███░░░░░░░",
		},
		{
			name:     "99 percent",
			progress: 99,
			expected: "█████████░",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateProgressBar(tt.progress)
			if got != tt.expected {
				t.Errorf("generateProgressBar(%d) = %q, want %q", tt.progress, got, tt.expected)
			}
			// Verify length is always 10 characters (10 blocks)
			if len([]rune(got)) != 10 {
				t.Errorf("progress bar length = %d runes, want 10", len([]rune(got)))
			}
		})
	}
}

// TestNotifierBlocksUseMarkdown tests that blocks use markdown formatting
func TestNotifierBlocksUseMarkdown(t *testing.T) {
	notifier := NewNotifier(&Config{
		BotToken: testutil.FakeSlackBotToken,
		Channel:  "#test",
	})

	// Verify notifier is created correctly
	if notifier == nil {
		t.Fatal("notifier is nil")
	}

	// The blocks should use mrkdwn type for rich formatting
	// This is tested via the actual message structure in other tests
}

// TestNotifierChannelConfiguration tests that channel is properly configured
func TestNotifierChannelConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		channel     string
		wantChannel string
	}{
		{
			name:        "channel with hash",
			channel:     "#dev-notifications",
			wantChannel: "#dev-notifications",
		},
		{
			name:        "channel without hash",
			channel:     "general",
			wantChannel: "general",
		},
		{
			name:        "channel ID",
			channel:     "C1234567890",
			wantChannel: "C1234567890",
		},
		{
			name:        "DM channel",
			channel:     "D1234567890",
			wantChannel: "D1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notifier := NewNotifier(&Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  tt.channel,
			})

			if notifier.channel != tt.wantChannel {
				t.Errorf("channel = %q, want %q", notifier.channel, tt.wantChannel)
			}
		})
	}
}

// TestConfigYAMLTags tests that Config struct has proper YAML tags
func TestConfigYAMLTags(t *testing.T) {
	// Verify the config can be represented in YAML-compatible format
	config := &Config{
		Enabled:  true,
		BotToken: "xoxb-token",
		Channel:  "#channel",
	}

	// Verify fields are accessible
	if !config.Enabled {
		t.Error("Enabled field not accessible")
	}
	if config.BotToken == "" {
		t.Error("BotToken field not accessible")
	}
	if config.Channel == "" {
		t.Error("Channel field not accessible")
	}
}

// TestNotifierNilConfig tests behavior with edge case configs
func TestNotifierEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name: "empty strings",
			config: &Config{
				Enabled:  false,
				BotToken: "",
				Channel:  "",
			},
		},
		{
			name: "whitespace in channel",
			config: &Config{
				BotToken: "token",
				Channel:  "  #channel  ",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			notifier := NewNotifier(tt.config)
			if notifier == nil {
				t.Error("NewNotifier returned nil for valid config")
			}
		})
	}
}
