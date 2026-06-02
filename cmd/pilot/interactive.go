package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ylcn91/pilot/internal/config"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	menuStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))
)

// runInteractiveMode starts an interactive CLI session
func runInteractiveMode() error {
	fmt.Println()
	fmt.Println(titleStyle.Render("  Pilot Interactive Mode"))
	fmt.Println(dimStyle.Render("  AI that ships your tickets"))
	fmt.Println()

	// Load config
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	for {
		option := showMainMenu(cfg)

		switch option {
		case "task":
			if err := interactiveNewTask(cfg); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		case "history":
			if err := interactiveHistory(cfg); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		case "project":
			if err := interactiveProjectSwitch(cfg); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		case "status":
			if err := interactiveStatus(cfg); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		case "quit":
			fmt.Println("\nGoodbye!")
			return nil
		}
	}
}

func showMainMenu(cfg *config.Config) string {
	fmt.Println(menuStyle.Render("  ─────────────────────────────────────"))
	fmt.Println()
	fmt.Printf("  %s Run a new task\n", selectedStyle.Render("[1]"))
	fmt.Printf("  %s View task history\n", selectedStyle.Render("[2]"))
	fmt.Printf("  %s Switch project\n", selectedStyle.Render("[3]"))
	fmt.Printf("  %s Show status\n", selectedStyle.Render("[4]"))
	fmt.Printf("  %s Quit\n", selectedStyle.Render("[q]"))
	fmt.Println()

	// Show current project
	if defaultProj := cfg.GetDefaultProject(); defaultProj != nil {
		fmt.Printf("  %s %s\n", dimStyle.Render("Current project:"), defaultProj.Name)
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("  Select option: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	switch input {
	case "1":
		return "task"
	case "2":
		return "history"
	case "3":
		return "project"
	case "4":
		return "status"
	case "q", "Q", "quit", "exit":
		return "quit"
	default:
		return ""
	}
}
