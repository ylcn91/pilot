package telegram

import (
	"context"
	"strings"

	"github.com/ylcn91/pilot/internal/memory"
)

// CommandHandler processes bot commands with access to memory store
type CommandHandler struct {
	handler *Handler
	store   *memory.Store
}

// NewCommandHandler creates a command handler with optional memory store
func NewCommandHandler(h *Handler, store *memory.Store) *CommandHandler {
	return &CommandHandler{
		handler: h,
		store:   store,
	}
}

// HandleCommand routes commands to their handlers
func (c *CommandHandler) HandleCommand(ctx context.Context, chatID, text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/start", "/help":
		c.handleHelp(ctx, chatID)
	case "/status":
		c.handleStatus(ctx, chatID)
	case "/cancel":
		c.handleCancel(ctx, chatID)
	case "/queue":
		c.handleQueue(ctx, chatID)
	case "/projects":
		c.handleProjects(ctx, chatID)
	case "/project", "/switch":
		if len(args) > 0 {
			c.handleSwitch(ctx, chatID, args[0])
		} else {
			c.handleCurrentProject(ctx, chatID)
		}
	case "/history":
		c.handleHistory(ctx, chatID)
	case "/budget":
		c.handleBudget(ctx, chatID)
	case "/tasks", "/list":
		c.handleTasks(ctx, chatID)
	case "/run":
		if len(args) > 0 {
			c.handler.handleRunCommand(ctx, chatID, args[0])
		} else {
			_, _ = c.handler.client.SendMessage(ctx, chatID, "Usage: /run <task-id>\nExample: /run 07", "")
		}
	case "/stop":
		c.handleStop(ctx, chatID)
	case "/voice":
		c.handler.sendVoiceSetupPrompt(ctx, chatID)
	case "/brief":
		c.handleBrief(ctx, chatID)
	case "/nopr":
		if len(args) > 0 {
			c.handleNoPR(ctx, chatID, strings.Join(args, " "))
		} else {
			_, _ = c.handler.client.SendMessage(ctx, chatID, "Usage: /nopr <task description>\nExecutes task without creating a PR.", "")
		}
	case "/pr":
		if len(args) > 0 {
			c.handleForcePR(ctx, chatID, strings.Join(args, " "))
		} else {
			_, _ = c.handler.client.SendMessage(ctx, chatID, "Usage: /pr <task description>\nForces PR creation even for ephemeral-looking tasks.", "")
		}
	default:
		_, _ = c.handler.client.SendMessage(ctx, chatID, "Unknown command. Use /help for available commands.", "")
	}
}

// handleHelp shows comprehensive help with all commands
func (c *CommandHandler) handleHelp(ctx context.Context, chatID string) {
	var helpText string
	if c.handler.plainTextMode {
		helpText = `🤖 Pilot Bot

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
	} else {
		helpText = `🤖 *Pilot Bot*

I execute tasks and answer questions about your codebase.

*Commands*
/status — Current task & queue status
/cancel — Cancel pending/running task
/queue — Show queued tasks
/projects — List configured projects
/switch <name> — Switch active project
/history — Recent task history
/budget — Show usage & costs
/brief — Generate daily summary
/help — This message

*Task Commands*
/tasks — Show task backlog
/run <id> — Execute task (e.g., /run 07)
/stop — Stop running task
/nopr <task> — Execute without creating PR
/pr <task> — Force PR creation

*Quick Patterns*
• 07 or task 07 — Run TASK-07
• status? — Project status
• todos? — List TODOs

*What I Understand*
• Tasks: "Create a file...", "Add feature..."
• Questions: "What handles auth?", "How does X work?"
• Greetings: "Hi", "Hello"

_Note: Ephemeral commands (serve, run, etc.) auto-skip PR creation._`
	}

	_, _ = c.handler.client.SendMessage(ctx, chatID, helpText, c.handler.getParseMode())
}
