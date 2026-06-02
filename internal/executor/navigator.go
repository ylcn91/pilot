// Package executor provides task execution with Navigator integration.
package executor

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/*
var embeddedTemplates embed.FS

// NavigatorConfig holds auto-init configuration.
type NavigatorConfig struct {
	AutoInit      bool   `yaml:"auto_init"`      // Enable auto-init (default: true)
	TemplatesPath string `yaml:"templates_path"` // Override plugin templates location
}

// DefaultNavigatorConfig returns default Navigator configuration.
func DefaultNavigatorConfig() *NavigatorConfig {
	return &NavigatorConfig{
		AutoInit: true,
	}
}

// ProjectInfo holds detected project metadata.
type ProjectInfo struct {
	Name         string `json:"name"`
	TechStack    string `json:"tech_stack"`
	DetectedFrom string `json:"detected_from"`
}

// NavigatorInitializer handles Navigator structure creation.
type NavigatorInitializer struct {
	templatesPath string
	log           *slog.Logger
}

// NewNavigatorInitializer creates an initializer with plugin templates.
func NewNavigatorInitializer(log *slog.Logger) (*NavigatorInitializer, error) {
	templatesPath, err := FindTemplatesPath()
	if err != nil {
		// Fall back to embedded templates
		log.Debug("Using embedded templates", slog.Any("error", err))
		return &NavigatorInitializer{
			templatesPath: "", // Empty means use embedded
			log:           log,
		}, nil
	}

	return &NavigatorInitializer{
		templatesPath: templatesPath,
		log:           log,
	}, nil
}

// FindTemplatesPath locates Navigator plugin templates.
func FindTemplatesPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot get home directory: %w", err)
	}

	// Check standard plugin locations
	pluginBase := filepath.Join(homeDir, ".claude", "plugins", "cache", "navigator-marketplace", "navigator")

	// Find latest version directory
	entries, err := os.ReadDir(pluginBase)
	if err != nil {
		return "", fmt.Errorf("navigator plugin not found: %w", err)
	}

	var latestVersion string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "6.") {
			if entry.Name() > latestVersion {
				latestVersion = entry.Name()
			}
		}
	}

	if latestVersion == "" {
		return "", fmt.Errorf("no navigator version found in %s", pluginBase)
	}

	templatesPath := filepath.Join(pluginBase, latestVersion, "templates")
	if _, err := os.Stat(templatesPath); err != nil {
		return "", fmt.Errorf("templates not found: %w", err)
	}

	return templatesPath, nil
}

// IsInitialized checks if .agent/ exists and has valid structure.
func (n *NavigatorInitializer) IsInitialized(projectPath string) bool {
	agentDir := filepath.Join(projectPath, ".agent")
	info, err := os.Stat(agentDir)
	if err != nil || !info.IsDir() {
		return false
	}

	// Check for required files
	devReadme := filepath.Join(agentDir, "DEVELOPMENT-README.md")
	if _, err := os.Stat(devReadme); err != nil {
		return false
	}

	return true
}

// Initialize creates .agent/ structure from templates.
func (n *NavigatorInitializer) Initialize(projectPath string) error {
	if n.IsInitialized(projectPath) {
		n.log.Debug("Navigator already initialized", slog.String("path", projectPath))
		return nil
	}

	// Detect project info
	info, err := n.DetectProjectInfo(projectPath)
	if err != nil {
		n.log.Warn("Project detection failed, using defaults", slog.Any("error", err))
		info = &ProjectInfo{
			Name:         filepath.Base(projectPath),
			TechStack:    "Unknown",
			DetectedFrom: "directory_name",
		}
	}

	n.log.Info("Initializing Navigator",
		slog.String("project", info.Name),
		slog.String("tech_stack", info.TechStack),
		slog.String("detected_from", info.DetectedFrom),
	)

	// Create directory structure
	agentDir := filepath.Join(projectPath, ".agent")
	dirs := []string{
		agentDir,
		filepath.Join(agentDir, "tasks"),
		filepath.Join(agentDir, "system"),
		filepath.Join(agentDir, "sops"),
		filepath.Join(agentDir, "sops", "integrations"),
		filepath.Join(agentDir, "sops", "debugging"),
		filepath.Join(agentDir, "sops", "development"),
		filepath.Join(agentDir, "sops", "deployment"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Create .gitkeep files for empty directories
	gitkeepDirs := []string{
		filepath.Join(agentDir, "tasks"),
		filepath.Join(agentDir, "system"),
		filepath.Join(agentDir, "sops", "integrations"),
		filepath.Join(agentDir, "sops", "debugging"),
		filepath.Join(agentDir, "sops", "development"),
		filepath.Join(agentDir, "sops", "deployment"),
	}

	for _, dir := range gitkeepDirs {
		gitkeep := filepath.Join(dir, ".gitkeep")
		if err := os.WriteFile(gitkeep, []byte{}, 0644); err != nil {
			n.log.Warn("Failed to create .gitkeep", slog.String("path", gitkeep))
		}
	}

	// Copy and customize templates — continue on individual failures
	var initErrors []string

	if err := n.copyTemplate("DEVELOPMENT-README.md", filepath.Join(agentDir, "DEVELOPMENT-README.md"), info); err != nil {
		n.log.Warn("Failed to copy DEVELOPMENT-README.md", slog.Any("error", err))
		initErrors = append(initErrors, err.Error())
	}

	// Create .nav-config.json
	if err := n.createNavConfig(agentDir, info); err != nil {
		n.log.Warn("Failed to create .nav-config.json", slog.Any("error", err))
		initErrors = append(initErrors, err.Error())
	}

	// Create .gitignore for .agent/
	gitignoreContent := `# Navigator session-specific data
.context-markers/
*.log
`
	if err := os.WriteFile(filepath.Join(agentDir, ".gitignore"), []byte(gitignoreContent), 0644); err != nil {
		n.log.Warn("Failed to create .gitignore", slog.Any("error", err))
	}

	if len(initErrors) > 0 {
		return fmt.Errorf("partial init (%d errors): %s", len(initErrors), strings.Join(initErrors, "; "))
	}

	n.log.Info("Navigator initialized successfully",
		slog.String("path", agentDir),
	)

	return nil
}
