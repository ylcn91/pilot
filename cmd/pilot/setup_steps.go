package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/transcription"
)

func setupTelegram(reader *bufio.Reader, cfg *config.Config) error {
	fmt.Print("  Set up Telegram bot? [Y/n]: ")
	if !readYesNo(reader, true) {
		return nil
	}

	// Initialize telegram config if needed
	if cfg.Adapters == nil {
		cfg.Adapters = &config.AdaptersConfig{}
	}
	if cfg.Adapters.Telegram == nil {
		cfg.Adapters.Telegram = telegram.DefaultConfig()
	}

	// Check for existing token
	if cfg.Adapters.Telegram.BotToken != "" {
		fmt.Printf("  Existing token found. Replace? [y/N]: ")
		if !readYesNo(reader, false) {
			cfg.Adapters.Telegram.Enabled = true
			fmt.Println("  ✓ Keeping existing token")
			return nil
		}
	}

	fmt.Print("  Enter bot token (from @BotFather): ")
	token := readLine(reader)
	if token == "" {
		fmt.Println("  ○ Skipped - no token provided")
		return nil
	}

	cfg.Adapters.Telegram.BotToken = token
	cfg.Adapters.Telegram.Enabled = true
	cfg.Adapters.Telegram.Polling = true

	// Validate token by getting bot info
	fmt.Print("  Validating... ")
	if err := validateTelegramToken(token); err != nil {
		fmt.Println("✗")
		fmt.Printf("  ⚠️  Token validation failed: %v\n", err)
		fmt.Print("  Continue anyway? [y/N]: ")
		if !readYesNo(reader, false) {
			cfg.Adapters.Telegram.BotToken = ""
			cfg.Adapters.Telegram.Enabled = false
			return nil
		}
	} else {
		fmt.Println("✓")
	}

	// Ask for chat ID (required for bot to reply)
	fmt.Println("  Message @userinfobot on Telegram to get your Chat ID")
	fmt.Print("  Paste Chat ID: ")
	chatID := readLine(reader)
	if chatID != "" {
		cfg.Adapters.Telegram.ChatID = chatID
	}

	fmt.Println("  ✓ Telegram configured")
	return nil
}

func setupProjects(reader *bufio.Reader, cfg *config.Config) error {
	// Show existing projects
	if len(cfg.Projects) > 0 {
		fmt.Println("  Existing projects:")
		for _, p := range cfg.Projects {
			fmt.Printf("    • %s: %s\n", p.Name, p.Path)
		}
		fmt.Print("  Add more projects? [y/N]: ")
		if !readYesNo(reader, false) {
			return nil
		}
	}

	for {
		fmt.Print("  Project path (or Enter to finish): ")
		path := readLine(reader)
		if path == "" {
			break
		}

		// Expand ~ and validate
		path = expandPath(path)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("  ⚠️  Path not found: %s\n", path)
			fmt.Print("  Add anyway? [y/N]: ")
			if !readYesNo(reader, false) {
				continue
			}
		}

		// Get project name
		defaultName := filepath.Base(path)
		fmt.Printf("  Project name [%s]: ", defaultName)
		name := readLine(reader)
		if name == "" {
			name = defaultName
		}

		// Check for Navigator
		hasNavigator := false
		agentPath := filepath.Join(path, ".agent")
		if _, err := os.Stat(agentPath); err == nil {
			hasNavigator = true
			fmt.Println("  ✓ Navigator detected")
		}

		// Add project
		cfg.Projects = append(cfg.Projects, &config.ProjectConfig{
			Name:      name,
			Path:      path,
			Navigator: hasNavigator,
		})

		fmt.Printf("  ✓ Added: %s\n", name)
	}

	if len(cfg.Projects) == 0 {
		fmt.Println("  ○ No projects configured")
	} else {
		fmt.Printf("  ✓ %d project(s) configured\n", len(cfg.Projects))
	}

	return nil
}

func setupVoice(reader *bufio.Reader, cfg *config.Config) error {
	// Initialize transcription config
	if cfg.Adapters.Telegram == nil {
		cfg.Adapters.Telegram = telegram.DefaultConfig()
	}
	if cfg.Adapters.Telegram.Transcription == nil {
		cfg.Adapters.Telegram.Transcription = &transcription.Config{
			Backend: "whisper-api",
		}
	}

	// Check for existing OpenAI key
	apiKey := cfg.Adapters.Telegram.Transcription.OpenAIAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	if apiKey != "" {
		fmt.Println("  ✓ OpenAI API key found")
		cfg.Adapters.Telegram.Transcription.OpenAIAPIKey = apiKey
		cfg.Adapters.Telegram.Transcription.Backend = "whisper-api"
	} else {
		fmt.Println("  Voice transcription requires OpenAI API key (Whisper)")
		fmt.Print("  Enter OpenAI API key (or leave empty to skip): ")
		apiKey = readLine(reader)

		if apiKey != "" {
			cfg.Adapters.Telegram.Transcription.OpenAIAPIKey = apiKey
			cfg.Adapters.Telegram.Transcription.Backend = "whisper-api"
			fmt.Println("  ✓ Whisper API configured")
		} else {
			fmt.Println("  ○ Voice transcription not configured")
		}
	}

	return nil
}

func setupBriefs(reader *bufio.Reader, cfg *config.Config) error {
	fmt.Print("  Enable daily briefs? [y/N]: ")
	if !readYesNo(reader, false) {
		return nil
	}

	// Initialize config
	if cfg.Orchestrator == nil {
		cfg.Orchestrator = &config.OrchestratorConfig{
			Model:         "claude-sonnet-4-6",
			MaxConcurrent: 2,
		}
	}
	if cfg.Orchestrator.DailyBrief == nil {
		cfg.Orchestrator.DailyBrief = &config.DailyBriefConfig{
			Channels: []config.BriefChannelConfig{},
			Content: config.BriefContentConfig{
				IncludeMetrics:     true,
				IncludeErrors:      true,
				MaxItemsPerSection: 10,
			},
			Filters: config.BriefFilterConfig{
				Projects: []string{},
			},
		}
	}

	cfg.Orchestrator.DailyBrief.Enabled = true

	// Schedule
	fmt.Print("  What time? (24h format) [9:00]: ")
	timeStr := readLine(reader)
	if timeStr == "" {
		timeStr = "9:00"
	}

	// Parse time into cron
	hour, minute := "9", "0"
	if _, err := fmt.Sscanf(timeStr, "%[0-9]:%[0-9]", &hour, &minute); err == nil {
		cfg.Orchestrator.DailyBrief.Schedule = fmt.Sprintf("%s %s * * 1-5", minute, hour)
	} else {
		cfg.Orchestrator.DailyBrief.Schedule = "0 9 * * 1-5" // default
	}

	// Timezone
	fmt.Print("  Timezone [Europe/Berlin]: ")
	tz := readLine(reader)
	if tz == "" {
		tz = "Europe/Berlin"
	}
	cfg.Orchestrator.DailyBrief.Timezone = tz

	fmt.Printf("  ✓ Daily briefs at %s (%s)\n", timeStr, tz)

	return nil
}

func setupAlerts(reader *bufio.Reader, cfg *config.Config) error {
	fmt.Print("  Enable failure alerts? [Y/n]: ")
	if !readYesNo(reader, true) {
		return nil
	}

	// Initialize config
	if cfg.Alerts == nil {
		cfg.Alerts = &config.AlertsConfig{
			Enabled: true,
		}
	}
	cfg.Alerts.Enabled = true

	// Default to Telegram if configured
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
		fmt.Println("  ✓ Alerts will be sent to Telegram")
	}

	return nil
}
