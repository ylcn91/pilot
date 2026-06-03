package comms

import (
	"context"
	"strings"

	"github.com/ylcn91/pilot/internal/memory"
)

// CommandHandler processes bot commands with access to messenger and memory store.
// It provides a platform-agnostic implementation of all slash commands.
type CommandHandler struct {
	messenger          Messenger
	store              *memory.Store
	runCommandFunc     func(ctx context.Context, contextID, taskID string)
	statusQueryFunc    func(contextID string) (pending, running interface{})
	activeProjectFunc  func(contextID string) (name, path string)
	projectListFunc    func() []interface{}
	setProjectFunc     func(contextID, projectName string) error
	cancelTaskFunc     func(ctx context.Context, contextID string) error
	stopTaskFunc       func(ctx context.Context, contextID string) error
	listTasksFunc      func() string
	briefGeneratorFunc func(ctx context.Context, contextID string) error // Platform-specific brief generation
}

// CommandHandlerConfig groups the platform-specific callbacks a CommandHandler
// needs. Wiring them at construction makes a missing callback visible at the
// call site instead of producing a silent nil-func fall-through at runtime.
// Any field left nil behaves exactly as if its setter was never called.
type CommandHandlerConfig struct {
	RunCommandFunc     func(ctx context.Context, contextID, taskID string)
	StatusQueryFunc    func(contextID string) (pending, running interface{})
	ActiveProjectFunc  func(contextID string) (name, path string)
	ProjectListFunc    func() []interface{}
	SetProjectFunc     func(contextID, projectName string) error
	CancelTaskFunc     func(ctx context.Context, contextID string) error
	StopTaskFunc       func(ctx context.Context, contextID string) error
	ListTasksFunc      func() string
	BriefGeneratorFunc func(ctx context.Context, contextID string) error
}

// NewCommandHandler creates a command handler with messenger and optional memory store.
func NewCommandHandler(messenger Messenger, store *memory.Store) *CommandHandler {
	return &CommandHandler{
		messenger: messenger,
		store:     store,
	}
}

// NewCommandHandlerWithConfig creates a command handler with all platform-specific
// callbacks wired at construction. It is equivalent to NewCommandHandler followed
// by the matching SetXxxFunc calls for each non-nil field.
func NewCommandHandlerWithConfig(messenger Messenger, store *memory.Store, cfg CommandHandlerConfig) *CommandHandler {
	return &CommandHandler{
		messenger:          messenger,
		store:              store,
		runCommandFunc:     cfg.RunCommandFunc,
		statusQueryFunc:    cfg.StatusQueryFunc,
		activeProjectFunc:  cfg.ActiveProjectFunc,
		projectListFunc:    cfg.ProjectListFunc,
		setProjectFunc:     cfg.SetProjectFunc,
		cancelTaskFunc:     cfg.CancelTaskFunc,
		stopTaskFunc:       cfg.StopTaskFunc,
		listTasksFunc:      cfg.ListTasksFunc,
		briefGeneratorFunc: cfg.BriefGeneratorFunc,
	}
}

// SetRunCommandFunc sets the /run command handler (platform-specific).
func (c *CommandHandler) SetRunCommandFunc(f func(ctx context.Context, contextID, taskID string)) {
	c.runCommandFunc = f
}

// SetStatusQueryFunc sets the status query function.
func (c *CommandHandler) SetStatusQueryFunc(f func(contextID string) (pending, running interface{})) {
	c.statusQueryFunc = f
}

// SetActiveProjectFunc sets the active project query function.
func (c *CommandHandler) SetActiveProjectFunc(f func(contextID string) (name, path string)) {
	c.activeProjectFunc = f
}

// SetProjectListFunc sets the project list function.
func (c *CommandHandler) SetProjectListFunc(f func() []interface{}) {
	c.projectListFunc = f
}

// SetSetProjectFunc sets the project switching function.
func (c *CommandHandler) SetSetProjectFunc(f func(contextID, projectName string) error) {
	c.setProjectFunc = f
}

// SetCancelTaskFunc sets the task cancellation function.
func (c *CommandHandler) SetCancelTaskFunc(f func(ctx context.Context, contextID string) error) {
	c.cancelTaskFunc = f
}

// SetStopTaskFunc sets the task stopping function.
func (c *CommandHandler) SetStopTaskFunc(f func(ctx context.Context, contextID string) error) {
	c.stopTaskFunc = f
}

// SetListTasksFunc sets the task listing function.
func (c *CommandHandler) SetListTasksFunc(f func() string) {
	c.listTasksFunc = f
}

// SetBriefGeneratorFunc sets the brief generator function (platform-specific).
func (c *CommandHandler) SetBriefGeneratorFunc(f func(ctx context.Context, contextID string) error) {
	c.briefGeneratorFunc = f
}

// HandleCommand routes slash commands to their handlers.
func (c *CommandHandler) HandleCommand(ctx context.Context, contextID, text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/start", "/help":
		c.handleHelp(ctx, contextID)
	case "/status":
		c.handleStatus(ctx, contextID)
	case "/cancel":
		c.handleCancel(ctx, contextID)
	case "/queue":
		c.handleQueue(ctx, contextID)
	case "/projects":
		c.handleProjects(ctx, contextID)
	case "/project", "/switch":
		if len(args) > 0 {
			c.handleSwitch(ctx, contextID, args[0])
		} else {
			c.handleCurrentProject(ctx, contextID)
		}
	case "/history":
		c.handleHistory(ctx, contextID)
	case "/budget":
		c.handleBudget(ctx, contextID)
	case "/tasks", "/list":
		c.handleTasks(ctx, contextID)
	case "/run":
		if len(args) > 0 {
			if c.runCommandFunc != nil {
				c.runCommandFunc(ctx, contextID, args[0])
			} else {
				_ = c.messenger.SendText(ctx, contextID, "Usage: /run <task-id>\nExample: /run 07")
			}
		} else {
			_ = c.messenger.SendText(ctx, contextID, "Usage: /run <task-id>\nExample: /run 07")
		}
	case "/stop":
		c.handleStop(ctx, contextID)
	case "/brief":
		c.handleBrief(ctx, contextID)
	case "/nopr":
		if len(args) > 0 {
			c.handleNoPR(ctx, contextID, strings.Join(args, " "))
		} else {
			_ = c.messenger.SendText(ctx, contextID, "Usage: /nopr <task description>\nExecutes task without creating a PR.")
		}
	case "/pr":
		if len(args) > 0 {
			c.handleForcePR(ctx, contextID, strings.Join(args, " "))
		} else {
			_ = c.messenger.SendText(ctx, contextID, "Usage: /pr <task description>\nForces PR creation even for ephemeral-looking tasks.")
		}
	default:
		_ = c.messenger.SendText(ctx, contextID, "Unknown command. Use /help for available commands.")
	}
}
