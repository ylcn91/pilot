package telegram

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// handleProjects lists configured projects
func (c *CommandHandler) handleProjects(ctx context.Context, chatID string) {
	if c.handler.projects == nil {
		_, _ = c.handler.client.SendMessage(ctx, chatID,
			"📁 No projects configured.\n\nAdd projects to ~/.pilot/config.yaml", "")
		return
	}

	projects := c.handler.projects.ListProjects()
	if len(projects) == 0 {
		_, _ = c.handler.client.SendMessage(ctx, chatID,
			"📁 No projects configured.\n\nAdd projects to ~/.pilot/config.yaml", "")
		return
	}

	activeProjectPath := c.handler.getActiveProjectPath(chatID)
	plainText := c.handler.plainTextMode

	var sb strings.Builder
	if plainText {
		sb.WriteString("📁 Projects\n\n")
	} else {
		sb.WriteString("📁 *Projects*\n\n")
	}

	// Build keyboard for quick switching
	var keyboard [][]InlineKeyboardButton

	for _, p := range projects {
		marker := ""
		if p.Path == activeProjectPath {
			marker = " ✅"
		}
		nav := ""
		if p.Navigator {
			nav = " 🧭"
		}
		if plainText {
			sb.WriteString(fmt.Sprintf("• %s%s%s\n", p.Name, marker, nav))
			sb.WriteString(fmt.Sprintf("  %s\n\n", p.Path))
		} else {
			sb.WriteString(fmt.Sprintf("• *%s*%s%s\n", escapeMarkdown(p.Name), marker, nav))
			sb.WriteString(fmt.Sprintf("  `%s`\n\n", p.Path))
		}

		// Add keyboard button if not active
		if p.Path != activeProjectPath {
			keyboard = append(keyboard, []InlineKeyboardButton{
				{Text: fmt.Sprintf("📂 %s", p.Name), CallbackData: fmt.Sprintf("switch_%s", p.Name)},
			})
		}
	}

	if len(keyboard) > 0 {
		_, _ = c.handler.client.SendMessageWithKeyboard(ctx, chatID, sb.String(), c.handler.getParseMode(), keyboard)
	} else {
		_, _ = c.handler.client.SendMessage(ctx, chatID, sb.String(), c.handler.getParseMode())
	}
}

// handleSwitch switches to a different project
func (c *CommandHandler) handleSwitch(ctx context.Context, chatID, projectName string) {
	proj, err := c.handler.setActiveProject(chatID, projectName)
	if err != nil {
		// Try fuzzy match
		if c.handler.projects != nil {
			for _, p := range c.handler.projects.ListProjects() {
				if strings.Contains(strings.ToLower(p.Name), strings.ToLower(projectName)) {
					proj, err = c.handler.setActiveProject(chatID, p.Name)
					break
				}
			}
		}

		if err != nil {
			_, _ = c.handler.client.SendMessage(ctx, chatID,
				fmt.Sprintf("❌ Project '%s' not found\n\nUse /projects to see available projects", projectName), "")
			return
		}
	}

	nav := ""
	if proj.Navigator {
		nav = " 🧭"
	}
	var text string
	if c.handler.plainTextMode {
		text = fmt.Sprintf("✅ Switched to %s%s\n%s", proj.Name, nav, proj.Path)
	} else {
		text = fmt.Sprintf("✅ Switched to *%s*%s\n`%s`", escapeMarkdown(proj.Name), nav, proj.Path)
	}
	_, _ = c.handler.client.SendMessage(ctx, chatID, text, c.handler.getParseMode())
}

// handleCurrentProject shows current active project
func (c *CommandHandler) handleCurrentProject(ctx context.Context, chatID string) {
	activeProjectPath := c.handler.getActiveProjectPath(chatID)
	projInfo := c.handler.getActiveProjectInfo(chatID)

	var projName string
	nav := ""
	if projInfo != nil {
		projName = projInfo.Name
		if projInfo.Navigator {
			nav = " 🧭"
		}
	} else {
		projName = filepath.Base(activeProjectPath)
	}

	var text string
	if c.handler.plainTextMode {
		text = fmt.Sprintf("📁 Active: %s%s\n%s\n\nUse /projects to see all", projName, nav, activeProjectPath)
	} else {
		text = fmt.Sprintf("📁 Active: *%s*%s\n`%s`\n\nUse /projects to see all", escapeMarkdown(projName), nav, activeProjectPath)
	}
	_, _ = c.handler.client.SendMessage(ctx, chatID, text, c.handler.getParseMode())
}

// HandleCallbackSwitch handles project switch callbacks
func (c *CommandHandler) HandleCallbackSwitch(ctx context.Context, chatID, projectName string) {
	c.handleSwitch(ctx, chatID, projectName)
}
