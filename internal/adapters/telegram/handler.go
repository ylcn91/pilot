package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/transcription"
)

const startupAPITimeout = 10 * time.Second

// MemberResolver resolves a Telegram user to a team member ID for RBAC (GH-634).
// Decoupled from teams package to avoid import cycles.
type MemberResolver interface {
	// ResolveTelegramIdentity maps a Telegram user ID and/or email to a member ID.
	// Returns ("", nil) when no match is found (= skip RBAC).
	ResolveTelegramIdentity(telegramID int64, email string) (string, error)
}

// MemberResolverAdapter wraps a telegram.MemberResolver as comms.MemberResolver.
type MemberResolverAdapter struct {
	Inner MemberResolver
}

// ResolveIdentity maps a sender ID string to a team member ID via the inner Telegram resolver.
func (a *MemberResolverAdapter) ResolveIdentity(senderID string) (string, error) {
	id, err := strconv.ParseInt(senderID, 10, 64)
	if err != nil {
		return "", nil // not a valid Telegram ID
	}
	return a.Inner.ResolveTelegramIdentity(id, "")
}

// ApprovalCallbackHandler handles approve:/reject: inline-keyboard callbacks dispatched from Telegram.
type ApprovalCallbackHandler interface {
	HandleCallback(ctx context.Context, callbackID, data, userID, username string) bool
}

// Handler processes incoming Telegram messages and executes tasks
type Handler struct {
	client           *Client
	runner           *executor.Runner
	projects         comms.ProjectSource // Project source for multi-project support
	projectPath      string              // Default/fallback project path
	allowedIDs       map[int64]bool      // Allowed user/chat IDs for security
	offset           int64               // Last processed update ID
	mu               sync.Mutex
	stopCh           chan struct{}
	wg               sync.WaitGroup
	transcriber      *transcription.Service  // Voice transcription service (optional)
	transcriptionErr error                   // Error from transcription init (for guidance)
	store            *memory.Store           // Memory store for history/queue/budget (optional)
	cmdHandler       *CommandHandler         // Command handler for /commands
	plainTextMode    bool                    // Use plain text instead of Markdown
	botUsername      string                  // Bot username for mention stripping (GH-2129)
	commsHandler     *comms.Handler          // Shared message handler (GH-2143)
	approvalHandler  ApprovalCallbackHandler // Routes approve:/reject: callbacks (GH-2651)
}

// HandlerConfig holds configuration for the Telegram handler
type HandlerConfig struct {
	BotToken        string
	ProjectPath     string                  // Default/fallback project path
	Projects        comms.ProjectSource     // Project source for multi-project support
	AllowedIDs      []int64                 // User/chat IDs allowed to send tasks
	Transcription   *transcription.Config   // Voice transcription config (optional)
	Store           *memory.Store           // Memory store for history/queue/budget (optional)
	PlainTextMode   bool                    // Use plain text instead of Markdown (default: true)
	RateLimit       *comms.RateLimitConfig  // Rate limiting config (optional)
	LLMClassifier   *LLMClassifierConfig    // LLM intent classification config (optional)
	MemberResolver  MemberResolver          // Team member resolver for RBAC (optional, GH-634)
	CommsHandler    *comms.Handler          // Shared message handler (optional, GH-2143)
	Client          *Client                 // Optional reuse of existing client
	ApprovalHandler ApprovalCallbackHandler // Routes approve:/reject: callbacks (optional, GH-2651)
}

// NewHandler creates a new Telegram message handler
func NewHandler(config *HandlerConfig, runner *executor.Runner) *Handler {
	allowedIDs := make(map[int64]bool)
	for _, id := range config.AllowedIDs {
		allowedIDs[id] = true
	}

	// Determine default project path
	projectPath := config.ProjectPath
	if projectPath == "" && config.Projects != nil {
		if defaultProj := config.Projects.GetDefaultProject(); defaultProj != nil {
			projectPath = defaultProj.Path
		}
	}

	// Use provided client or create a new one
	client := config.Client
	if client == nil {
		client = NewClient(config.BotToken)
	}

	h := &Handler{
		client:          client,
		runner:          runner,
		projects:        config.Projects,
		projectPath:     projectPath,
		allowedIDs:      allowedIDs,
		stopCh:          make(chan struct{}),
		store:           config.Store,
		plainTextMode:   config.PlainTextMode,
		commsHandler:    config.CommsHandler,
		approvalHandler: config.ApprovalHandler,
	}

	// Initialize command handler
	h.cmdHandler = NewCommandHandler(h, config.Store)

	// Initialize transcription service if configured
	if config.Transcription != nil {
		svc, err := transcription.NewService(config.Transcription)
		if err != nil {
			h.transcriptionErr = err
			logging.WithComponent("telegram").Warn("Transcription not available", slog.Any("error", err))
		} else {
			h.transcriber = svc
			logging.WithComponent("telegram").Debug("Voice transcription enabled", slog.String("backend", svc.BackendName()))
		}
	}

	return h
}

// getActiveProjectPath returns the active project path for a chat
func (h *Handler) getActiveProjectPath(chatID string) string {
	if h.commsHandler != nil {
		_, path := h.commsHandler.GetActiveProject(chatID)
		if path != "" {
			return path
		}
	}
	return h.projectPath
}

// setActiveProject sets the active project for a chat by name
func (h *Handler) setActiveProject(chatID, projectName string) (*comms.ProjectInfo, error) {
	if h.commsHandler != nil {
		if err := h.commsHandler.SetActiveProject(chatID, projectName); err != nil {
			return nil, err
		}
	}
	if h.projects == nil {
		return nil, fmt.Errorf("no projects configured")
	}
	proj := h.projects.GetProjectByName(projectName)
	if proj == nil {
		return nil, fmt.Errorf("project '%s' not found", projectName)
	}
	return proj, nil
}

// getActiveProjectInfo returns the active project info for a chat
func (h *Handler) getActiveProjectInfo(chatID string) *comms.ProjectInfo {
	if h.projects == nil {
		return nil
	}

	path := h.getActiveProjectPath(chatID)
	return h.projects.GetProjectByPath(path)
}

// getParseMode returns the parse mode based on plainTextMode setting.
// Returns empty string for plain text, "Markdown" for markdown mode.
func (h *Handler) getParseMode() string {
	if h.plainTextMode {
		return ""
	}
	return "Markdown"
}

// CheckSingleton verifies no other bot instance is already running.
// Returns ErrConflict if another instance is detected.
func (h *Handler) CheckSingleton(ctx context.Context) error {
	startupCtx, cancel := context.WithTimeout(ctx, startupAPITimeout)
	defer cancel()
	return h.client.CheckSingleton(startupCtx)
}

// StartPolling starts polling for updates in a goroutine
func (h *Handler) StartPolling(ctx context.Context) {
	// Fetch bot username for mention stripping (GH-2129)
	startupCtx, cancel := context.WithTimeout(ctx, startupAPITimeout)
	defer cancel()
	if me, err := h.client.GetMe(startupCtx); err != nil {
		logging.WithComponent("telegram").Warn("Failed to fetch bot username via getMe", slog.String("error", err.Error()))
	} else if me != nil {
		h.botUsername = me.Username
		logging.WithComponent("telegram").Debug("Bot username resolved", slog.String("username", me.Username))
	}

	h.wg.Add(1)
	go h.pollLoop(ctx)

	// Start cleanup goroutine for expired pending tasks
	h.wg.Add(1)
	go h.cleanupLoop(ctx)
}

// Stop gracefully stops the polling loop
func (h *Handler) Stop() {
	close(h.stopCh)
	h.wg.Wait()
}

// pollLoop continuously polls for updates
func (h *Handler) pollLoop(ctx context.Context) {
	defer h.wg.Done()

	logging.WithComponent("telegram").Debug("Starting poll loop")

	for {
		select {
		case <-ctx.Done():
			logging.WithComponent("telegram").Debug("Poll loop stopped")
			return
		case <-h.stopCh:
			logging.WithComponent("telegram").Debug("Poll loop stopped")
			return
		default:
			h.fetchAndProcess(ctx)
		}
	}
}

// cleanupLoop delegates cleanup to the shared commsHandler.
func (h *Handler) cleanupLoop(ctx context.Context) {
	defer h.wg.Done()
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer logging.Recover("telegram.handler.cleanupCancelWatch")
		select {
		case <-h.stopCh:
			cancel()
		case <-cctx.Done():
		}
	}()
	if h.commsHandler != nil {
		h.commsHandler.CleanupLoop(cctx)
	}
}

// fetchAndProcess fetches updates and processes them
func (h *Handler) fetchAndProcess(ctx context.Context) {
	// Use long polling with 30 second timeout
	updates, err := h.client.GetUpdates(ctx, h.offset, 30)
	if err != nil {
		// Don't spam logs on context cancellation
		if ctx.Err() == nil {
			logging.WithComponent("telegram").Warn("Error fetching updates", slog.Any("error", err))
		}
		// Brief pause before retry on error
		time.Sleep(time.Second)
		return
	}

	for _, update := range updates {
		h.processUpdate(ctx, update)
		// Update offset to acknowledge this update
		h.mu.Lock()
		if update.UpdateID >= h.offset {
			h.offset = update.UpdateID + 1
		}
		h.mu.Unlock()
	}
}
