package health

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/transcription"
)

// ---------------------------------------------------------------------------
// checkFeatures
// ---------------------------------------------------------------------------

func TestCheckFeatures_MinimalConfig(t *testing.T) {
	cfg := &config.Config{}
	features := checkFeatures(cfg)

	// Should always have core features regardless of config
	if len(features) == 0 {
		t.Fatal("expected at least one feature check")
	}

	// Find Task Execution — should be OK if claude+git are on PATH
	exec := findFeature(features, "Task Execution")
	if exec == nil {
		t.Fatal("expected 'Task Execution' feature")
	}
	// On dev machines claude+git should be present
	if exec.Status == StatusError && len(exec.Missing) == 0 {
		t.Error("Task Execution error but Missing is empty")
	}
}

func TestCheckFeatures_TelegramDisabled(t *testing.T) {
	cfg := &config.Config{}
	features := checkFeatures(cfg)

	tg := findFeature(features, "Telegram")
	if tg == nil {
		t.Fatal("expected 'Telegram' feature")
	}
	if tg.Enabled {
		t.Error("Telegram should be disabled with empty config")
	}
	if tg.Status != StatusDisabled {
		t.Errorf("Telegram status = %v, want StatusDisabled", tg.Status)
	}
}

func TestCheckFeatures_TelegramEnabled(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			Telegram: &telegram.Config{
				Enabled:  true,
				BotToken: "test-bot-token",
			},
		},
	}
	features := checkFeatures(cfg)

	tg := findFeature(features, "Telegram")
	if tg == nil {
		t.Fatal("expected 'Telegram' feature")
	}
	if !tg.Enabled {
		t.Error("Telegram should be enabled")
	}
	if tg.Status != StatusOK {
		t.Errorf("Telegram status = %v, want StatusOK", tg.Status)
	}
}

func TestCheckFeatures_TelegramEnabledNoToken(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			Telegram: &telegram.Config{
				Enabled:  true,
				BotToken: "",
			},
		},
	}
	features := checkFeatures(cfg)

	tg := findFeature(features, "Telegram")
	if tg == nil {
		t.Fatal("expected 'Telegram' feature")
	}
	if tg.Enabled {
		t.Error("Telegram should be disabled without token")
	}
	if tg.Note != "missing bot_token" {
		t.Errorf("Telegram note = %q, want %q", tg.Note, "missing bot_token")
	}
}

func TestCheckFeatures_VoiceWithoutKey(t *testing.T) {
	cfg := &config.Config{}
	features := checkFeatures(cfg)

	voice := findFeature(features, "Voice")
	if voice == nil {
		t.Fatal("expected 'Voice' feature")
	}
	if voice.Enabled {
		t.Error("Voice should be disabled without OpenAI key")
	}
	if voice.Status != StatusWarning {
		t.Errorf("Voice status = %v, want StatusWarning", voice.Status)
	}
}

func TestCheckFeatures_VoiceWithKey(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			Telegram: &telegram.Config{
				Transcription: &transcription.Config{
					OpenAIAPIKey: "test-openai-key",
				},
			},
		},
	}
	features := checkFeatures(cfg)

	voice := findFeature(features, "Voice")
	if voice == nil {
		t.Fatal("expected 'Voice' feature")
	}
	if !voice.Enabled {
		t.Error("Voice should be enabled with OpenAI key")
	}
	if voice.Status != StatusOK {
		t.Errorf("Voice status = %v, want StatusOK", voice.Status)
	}
}

func TestCheckFeatures_AlertsEnabled(t *testing.T) {
	cfg := &config.Config{
		Alerts: &config.AlertsConfig{
			Enabled: true,
		},
	}
	features := checkFeatures(cfg)

	a := findFeature(features, "Alerts")
	if a == nil {
		t.Fatal("expected 'Alerts' feature")
	}
	if !a.Enabled {
		t.Error("Alerts should be enabled")
	}
}

func TestCheckFeatures_AlertsDisabled(t *testing.T) {
	cfg := &config.Config{}
	features := checkFeatures(cfg)

	a := findFeature(features, "Alerts")
	if a == nil {
		t.Fatal("expected 'Alerts' feature")
	}
	if a.Enabled {
		t.Error("Alerts should be disabled with empty config")
	}
}

// ---------------------------------------------------------------------------
// RunChecks — integration-level
// ---------------------------------------------------------------------------

func TestRunChecks_SetsFlags(t *testing.T) {
	cfg := &config.Config{}
	report := RunChecks(cfg)

	if report == nil {
		t.Fatal("RunChecks returned nil")
	}
	if len(report.Dependencies) == 0 {
		t.Error("expected at least one dependency check")
	}
	if len(report.Features) == 0 {
		t.Error("expected at least one feature check")
	}
}

func TestRunChecks_HasErrorsFlagSet(t *testing.T) {
	// Force an error by using a config with enabled adapter but no token
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			Telegram: &telegram.Config{
				Enabled:  true,
				BotToken: "",
			},
		},
	}
	report := RunChecks(cfg)

	// Config should have an error for missing telegram token
	hasConfigError := false
	for _, c := range report.Config {
		if c.Status == StatusError {
			hasConfigError = true
		}
	}
	if !hasConfigError {
		t.Error("expected at least one config error for missing telegram token")
	}
	if !report.HasErrors {
		t.Error("HasErrors should be true when config has errors")
	}
}

func TestRunChecks_ProjectCount(t *testing.T) {
	cfg := &config.Config{
		Projects: []*config.ProjectConfig{
			{Name: "a", Path: "/tmp"},
			{Name: "b", Path: "/tmp"},
		},
	}
	report := RunChecks(cfg)

	if report.Projects != 2 {
		t.Errorf("Projects = %d, want 2", report.Projects)
	}
}
