package executor

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// copyTemplate copies a template file with variable substitution.
func (n *NavigatorInitializer) copyTemplate(templateName, destPath string, info *ProjectInfo) error {
	var content []byte
	var err error

	if n.templatesPath != "" {
		// Read from plugin templates
		content, err = os.ReadFile(filepath.Join(n.templatesPath, templateName))
	} else {
		// Read from embedded templates — use forward slashes (embed.FS requirement, not filepath)
		content, err = fs.ReadFile(embeddedTemplates, "templates/"+templateName)
	}

	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", templateName, err)
	}

	// Customize template
	customized := n.customizeTemplate(string(content), info)

	return os.WriteFile(destPath, []byte(customized), 0644)
}

// customizeTemplate replaces placeholders with project info.
func (n *NavigatorInitializer) customizeTemplate(content string, info *ProjectInfo) string {
	now := time.Now()

	replacements := map[string]string{
		"[Project Name]":              info.Name,
		"${PROJECT_NAME}":             info.Name,
		"${project_name}":             strings.ToLower(strings.ReplaceAll(info.Name, " ", "-")),
		"[Your tech stack]":           info.TechStack,
		"${TECH_STACK}":               info.TechStack,
		"[Date]":                      now.Format("2006-01-02"),
		"${DATE}":                     now.Format("2006-01-02"),
		"${YEAR}":                     now.Format("2006"),
		"${DETECTED_FROM}":            info.DetectedFrom,
		"[Brief project description]": fmt.Sprintf("Autonomous development with %s", info.TechStack),
	}

	result := content
	for placeholder, value := range replacements {
		result = strings.ReplaceAll(result, placeholder, value)
	}

	return result
}

// createNavConfig creates the .nav-config.json file.
func (n *NavigatorInitializer) createNavConfig(agentDir string, info *ProjectInfo) error {
	config := map[string]interface{}{
		"version":             "6.1.0",
		"project_name":        info.Name,
		"tech_stack":          info.TechStack,
		"project_management":  "github", // Pilot uses GitHub by default
		"task_prefix":         "GH",
		"team_chat":           "none",
		"auto_load_navigator": true,
		"compact_strategy":    "conservative",
		"auto_update": map[string]interface{}{
			"enabled":              true,
			"check_interval_hours": 1,
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(agentDir, ".nav-config.json"), data, 0644)
}
