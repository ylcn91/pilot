package comms

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// handleHelp shows comprehensive help with all commands.
func (c *CommandHandler) handleHelp(ctx context.Context, contextID string) {
	helpText := `🤖 Pilot Bot

I execute tasks and answer questions about your codebase.

Commands
/status — Current task & queue status
/cancel — Cancel pending/running task
/queue — Show queued tasks
/projects — List configured projects
/switch <name> — Switch active project
/history — Recent task history
/budget — Show usage & costs
/brief — Generate daily summary
/help — This message

Task Commands
/tasks — Show task backlog
/run <id> — Execute task (e.g., /run 07)
/stop — Stop running task
/nopr <task> — Execute without creating PR
/pr <task> — Force PR creation

Quick Patterns
• 07 or task 07 — Run TASK-07
• status? — Project status
• todos? — List TODOs

What I Understand
• Tasks: "Create a file...", "Add feature..."
• Questions: "What handles auth?", "How does X work?"
• Greetings: "Hi", "Hello"

Note: Ephemeral commands (serve, run, etc.) auto-skip PR creation.`

	_ = c.messenger.SendText(ctx, contextID, helpText)
}

// handleStatus shows current status with running/pending/queue info.
func (c *CommandHandler) handleStatus(ctx context.Context, contextID string) {
	var sb strings.Builder
	sb.WriteString("📊 Status\n\n")

	// Get project name
	projName := "unknown"
	if c.activeProjectFunc != nil {
		name, _ := c.activeProjectFunc(contextID)
		if name != "" {
			projName = name
		}
	}
	sb.WriteString(fmt.Sprintf("📁 Project: %s\n", projName))

	// Get task status
	if c.statusQueryFunc != nil {
		pending, running := c.statusQueryFunc(contextID)

		// Running task
		if running != nil {
			// Type assertion to get task info (duck typing for compatibility)
			if taskIDer, ok := running.(interface{ GetTaskID() string }); ok {
				sb.WriteString(fmt.Sprintf("\n🔄 Running: %s\n", taskIDer.GetTaskID()))
				if startedAter, ok := running.(interface{ GetStartedAt() time.Time }); ok {
					elapsed := time.Since(startedAter.GetStartedAt()).Round(time.Second)
					sb.WriteString(fmt.Sprintf("   ⏱ %s\n", elapsed))
				}
			}
		}

		// Pending task
		if pending != nil {
			if taskIDer, ok := pending.(interface{ GetTaskID() string }); ok {
				sb.WriteString(fmt.Sprintf("\n⏳ Pending: %s\n", taskIDer.GetTaskID()))
				if createdAter, ok := pending.(interface{ GetCreatedAt() time.Time }); ok {
					age := time.Since(createdAter.GetCreatedAt()).Round(time.Second)
					sb.WriteString(fmt.Sprintf("   Awaiting confirmation (%s)\n", age))
				}
			}
		}
	}

	// Queue info from memory store
	if c.store != nil {
		queued, err := c.store.GetQueuedTasks(10)
		if err == nil && len(queued) > 0 {
			sb.WriteString(fmt.Sprintf("\n📋 Queue: %d task(s)\n", len(queued)))
		}
	}

	// No activity
	if c.statusQueryFunc == nil {
		sb.WriteString("\n✅ Ready for tasks")
	}

	_ = c.messenger.SendText(ctx, contextID, sb.String())
}

// handleProjects lists configured projects.
func (c *CommandHandler) handleProjects(ctx context.Context, contextID string) {
	if c.projectListFunc == nil {
		_ = c.messenger.SendText(ctx, contextID,
			"📁 No projects configured.\n\nAdd projects to ~/.pilot/config.yaml")
		return
	}

	projects := c.projectListFunc()
	if len(projects) == 0 {
		_ = c.messenger.SendText(ctx, contextID,
			"📁 No projects configured.\n\nAdd projects to ~/.pilot/config.yaml")
		return
	}

	var sb strings.Builder
	sb.WriteString("📁 Projects\n\n")

	activeName := ""
	if c.activeProjectFunc != nil {
		activeName, _ = c.activeProjectFunc(contextID)
	}

	for _, p := range projects {
		// Duck typing for project info
		marker := ""
		if namer, ok := p.(interface{ GetName() string }); ok {
			if namer.GetName() == activeName {
				marker = " ✅"
			}
		}

		nav := ""
		if navigatorer, ok := p.(interface{ IsNavigator() bool }); ok {
			if navigatorer.IsNavigator() {
				nav = " 🧭"
			}
		}

		if namer, ok := p.(interface{ GetName() string }); ok {
			sb.WriteString(fmt.Sprintf("• %s%s%s\n", namer.GetName(), marker, nav))
		}

		if pather, ok := p.(interface{ GetPath() string }); ok {
			sb.WriteString(fmt.Sprintf("  %s\n\n", pather.GetPath()))
		}
	}

	_ = c.messenger.SendText(ctx, contextID, sb.String())
}

// handleSwitch switches to a different project.
func (c *CommandHandler) handleSwitch(ctx context.Context, contextID, projectName string) {
	if c.setProjectFunc == nil {
		_ = c.messenger.SendText(ctx, contextID, "Project switching not configured")
		return
	}

	if err := c.setProjectFunc(contextID, projectName); err != nil {
		_ = c.messenger.SendText(ctx, contextID,
			fmt.Sprintf("❌ Project '%s' not found\n\nUse /projects to see available projects", projectName))
		return
	}

	name := projectName
	if c.activeProjectFunc != nil {
		actualName, _ := c.activeProjectFunc(contextID)
		if actualName != "" {
			name = actualName
		}
	}

	_ = c.messenger.SendText(ctx, contextID, fmt.Sprintf("✅ Switched to %s", name))
}

// handleCurrentProject shows current active project.
func (c *CommandHandler) handleCurrentProject(ctx context.Context, contextID string) {
	if c.activeProjectFunc == nil {
		_ = c.messenger.SendText(ctx, contextID, "Active project: unknown\n\nUse /projects to see all")
		return
	}

	projName, projPath := c.activeProjectFunc(contextID)
	if projName == "" {
		projName = filepath.Base(projPath)
	}

	text := fmt.Sprintf("📁 Active: %s\n%s\n\nUse /projects to see all", projName, projPath)
	_ = c.messenger.SendText(ctx, contextID, text)
}
