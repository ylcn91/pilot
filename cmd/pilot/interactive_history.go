package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/replay"
)

func interactiveHistory(cfg *config.Config) error {
	fmt.Println()
	fmt.Println(titleStyle.Render("  Task History"))
	fmt.Println()

	// Load recordings
	recordingsPath := replay.DefaultRecordingsPath()
	recordings, err := replay.ListRecordings(recordingsPath, &replay.RecordingFilter{Limit: 10})
	if err != nil {
		return fmt.Errorf("failed to list recordings: %w", err)
	}

	if len(recordings) == 0 {
		fmt.Println("  No task history found.")
		fmt.Println()
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("  Press Enter to continue...")
		_, _ = reader.ReadString('\n')
		return nil
	}

	// Show recent tasks
	for i, rec := range recordings {
		statusIcon := "+"
		switch rec.Status {
		case "failed":
			statusIcon = "x"
		case "cancelled":
			statusIcon = "!"
		}

		fmt.Printf("  %s [%s] %s (%s)\n",
			selectedStyle.Render(fmt.Sprintf("[%d]", i+1)),
			statusIcon,
			rec.TaskID,
			rec.Duration.Round(time.Second))
	}

	fmt.Println()
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("  Select task to view (or Enter to go back): ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return nil
	}

	// Parse selection
	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(recordings) {
		return nil
	}

	// Show selected recording
	rec := recordings[idx-1]
	recording, err := replay.LoadRecording(recordingsPath, rec.ID)
	if err != nil {
		return fmt.Errorf("failed to load recording: %w", err)
	}

	fmt.Println()
	fmt.Printf("  Task:     %s\n", recording.TaskID)
	fmt.Printf("  Status:   %s\n", recording.Status)
	fmt.Printf("  Duration: %s\n", recording.Duration.Round(time.Second))
	fmt.Printf("  Events:   %d\n", recording.EventCount)
	if recording.Metadata != nil && recording.Metadata.CommitSHA != "" {
		fmt.Printf("  Commit:   %s\n", recording.Metadata.CommitSHA)
	}
	if recording.Metadata != nil && recording.Metadata.PRUrl != "" {
		fmt.Printf("  PR:       %s\n", recording.Metadata.PRUrl)
	}

	fmt.Println()
	fmt.Print("  Press Enter to continue...")
	_, _ = reader.ReadString('\n')

	return nil
}

func interactiveProjectSwitch(cfg *config.Config) error {
	fmt.Println()
	fmt.Println(titleStyle.Render("  Switch Project"))
	fmt.Println()

	if len(cfg.Projects) == 0 {
		fmt.Println("  No projects configured.")
		fmt.Println("  Add projects to ~/.pilot/config.yaml")
		fmt.Println()
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("  Press Enter to continue...")
		_, _ = reader.ReadString('\n')
		return nil
	}

	for i, proj := range cfg.Projects {
		marker := " "
		if cfg.DefaultProject == proj.Name || (cfg.DefaultProject == "" && i == 0) {
			marker = "*"
		}
		nav := ""
		if proj.Navigator {
			nav = " [Navigator]"
		}
		fmt.Printf("  %s [%d] %s%s\n", marker, i+1, proj.Name, nav)
	}

	fmt.Println()
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("  Select project: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return nil
	}

	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(cfg.Projects) {
		return nil
	}

	selectedProj := cfg.Projects[idx-1]
	cfg.DefaultProject = selectedProj.Name

	// Save updated config
	if err := config.Save(cfg, config.DefaultConfigPath()); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n  Switched to: %s\n", selectedProj.Name)
	fmt.Println()
	fmt.Print("  Press Enter to continue...")
	_, _ = reader.ReadString('\n')

	return nil
}

func interactiveStatus(cfg *config.Config) error {
	fmt.Println()
	fmt.Println(titleStyle.Render("  Pilot Status"))
	fmt.Println()

	fmt.Printf("  Gateway: http://%s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
	fmt.Println()

	// Adapters
	fmt.Println("  Adapters:")
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
		fmt.Println("    + Telegram")
	}
	if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
		fmt.Println("    + Linear")
	}
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
		fmt.Println("    + Slack")
	}
	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
		fmt.Println("    + GitHub")
	}
	fmt.Println()

	// Projects
	fmt.Println("  Projects:")
	if len(cfg.Projects) == 0 {
		fmt.Println("    (none)")
	} else {
		for _, proj := range cfg.Projects {
			nav := ""
			if proj.Navigator {
				nav = " [Navigator]"
			}
			fmt.Printf("    - %s: %s%s\n", proj.Name, proj.Path, nav)
		}
	}
	fmt.Println()

	// Memory stats
	store, err := memory.NewStore(cfg.Memory.Path)
	if err == nil {
		defer func() { _ = store.Close() }()
		stats, err := store.GetCrossPatternStats()
		if err == nil && stats.TotalPatterns > 0 {
			fmt.Printf("  Patterns: %d learned\n", stats.TotalPatterns)
		}
	}

	fmt.Println()
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("  Press Enter to continue...")
	_, _ = reader.ReadString('\n')

	return nil
}
