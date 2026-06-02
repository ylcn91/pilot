package health

import (
	"github.com/ylcn91/pilot/internal/config"
)

// checkFeatures checks feature availability
func checkFeatures(cfg *config.Config) []FeatureStatus {
	features := []FeatureStatus{}

	// Determine active backend
	backendType := "claude-code"
	if cfg.Executor != nil && cfg.Executor.Type != "" {
		backendType = cfg.Executor.Type
	}

	// Find the command for the active backend
	backendCmd := "claude" // default
	for _, b := range backends {
		if b.backendType == backendType {
			backendCmd = b.command
			break
		}
	}

	// Core execution - check active backend + git
	hasBackend := commandExists(backendCmd)
	hasGit := commandExists("git")
	if hasBackend && hasGit {
		features = append(features, FeatureStatus{
			Name:    "Task Execution",
			Enabled: true,
			Status:  StatusOK,
		})
	} else {
		missing := []string{}
		if !hasBackend {
			missing = append(missing, backendCmd)
		}
		if !hasGit {
			missing = append(missing, "git")
		}
		features = append(features, FeatureStatus{
			Name:    "Task Execution",
			Enabled: false,
			Status:  StatusError,
			Missing: missing,
		})
	}

	// Telegram
	telegramEnabled := cfg.Adapters != nil &&
		cfg.Adapters.Telegram != nil &&
		cfg.Adapters.Telegram.Enabled &&
		cfg.Adapters.Telegram.BotToken != ""
	telegramNote := ""
	if cfg.Adapters != nil && cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken == "" {
		telegramNote = "missing bot_token"
	}
	features = append(features, FeatureStatus{
		Name:    "Telegram",
		Enabled: telegramEnabled,
		Status:  boolToStatus(telegramEnabled),
		Note:    telegramNote,
	})

	// Image analysis (available via multimodal backends)
	features = append(features, FeatureStatus{
		Name:    "Images",
		Enabled: hasBackend,
		Status:  boolToStatus(hasBackend),
	})

	// Voice transcription (only requires OpenAI API key)
	hasOpenAIKey := cfg.Adapters != nil &&
		cfg.Adapters.Telegram != nil &&
		cfg.Adapters.Telegram.Transcription != nil &&
		cfg.Adapters.Telegram.Transcription.OpenAIAPIKey != ""

	var voiceStatus Status
	var voiceNote string
	var voiceMissing []string
	voiceEnabled := false

	if hasOpenAIKey {
		voiceEnabled = true
		voiceStatus = StatusOK
		voiceNote = "Whisper API"
	} else {
		voiceStatus = StatusWarning
		voiceMissing = append(voiceMissing, "OPENAI_API_KEY")
		voiceNote = "missing: OPENAI_API_KEY"
	}

	features = append(features, FeatureStatus{
		Name:    "Voice",
		Enabled: voiceEnabled,
		Status:  voiceStatus,
		Note:    voiceNote,
		Missing: voiceMissing,
	})

	// Daily briefs
	briefsEnabled := cfg.Orchestrator != nil &&
		cfg.Orchestrator.DailyBrief != nil &&
		cfg.Orchestrator.DailyBrief.Enabled
	briefsNote := ""
	if briefsEnabled && cfg.Orchestrator.DailyBrief.Schedule == "" {
		briefsNote = "no schedule"
	}
	features = append(features, FeatureStatus{
		Name:    "Briefs",
		Enabled: briefsEnabled,
		Status:  boolToStatus(briefsEnabled),
		Note:    briefsNote,
	})

	// Alerts
	alertsEnabled := cfg.Alerts != nil && cfg.Alerts.Enabled
	features = append(features, FeatureStatus{
		Name:    "Alerts",
		Enabled: alertsEnabled,
		Status:  boolToStatus(alertsEnabled),
	})

	// Cross-project memory
	memoryEnabled := cfg.Memory != nil && cfg.Memory.CrossProject
	features = append(features, FeatureStatus{
		Name:    "Memory",
		Enabled: memoryEnabled,
		Status:  boolToStatus(memoryEnabled),
	})

	// PR creation
	hasGH := commandExists("gh")
	prNote := ""
	if !hasGH {
		prNote = "gh CLI not installed"
	}
	features = append(features, FeatureStatus{
		Name:    "PRs",
		Enabled: hasGH,
		Status:  boolToStatus(hasGH),
		Note:    prNote,
	})

	return features
}
