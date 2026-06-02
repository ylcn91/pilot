// Package comms provides shared communication handler logic for adapter implementations.
package comms

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/intent"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	texthelper "github.com/ylcn91/pilot/internal/text"
)

// MemberResolver resolves a platform user to a team member ID for RBAC.
// Each adapter provides a concrete implementation.
type MemberResolver interface {
	// ResolveIdentity maps a sender ID to a team member ID.
	// Returns ("", nil) when no match is found (= skip RBAC).
	ResolveIdentity(senderID string) (string, error)
}

// HandlerConfig holds configuration for creating a shared Handler.
type HandlerConfig struct {
	Messenger      Messenger
	Runner         *executor.Runner
	Projects       ProjectSource
	ProjectPath    string
	RateLimit      *RateLimitConfig
	LLMClassifier  intent.Classifier
	ConvStore      *intent.ConversationStore
	MemberResolver MemberResolver
	Store          *memory.Store
	// TaskIDPrefix is the adapter-specific prefix for task IDs (e.g., "TG", "SLACK").
	TaskIDPrefix string
	Log          *slog.Logger
}

// Handler implements platform-agnostic message handling with intent dispatch,
// rate limiting, task lifecycle, and progress tracking.
type Handler struct {
	messenger      Messenger
	runner         *executor.Runner
	projects       ProjectSource
	projectPath    string
	rateLimit      *RateLimiter
	llmClassifier  intent.Classifier
	convStore      *intent.ConversationStore
	memberResolver MemberResolver
	store          *memory.Store
	taskIDPrefix   string
	log            *slog.Logger

	activeProject map[string]string       // contextID -> projectPath
	pendingTasks  map[string]*PendingTask // contextID -> pending task
	runningTasks  map[string]*RunningTask // contextID -> running task
	lastSender    map[string]string       // contextID -> senderID
	mu            sync.Mutex
}

// NewHandler creates a shared Handler from the given config.
func NewHandler(cfg *HandlerConfig) *Handler {
	var rl *RateLimiter
	if cfg.RateLimit != nil {
		rl = NewRateLimiter(cfg.RateLimit)
	} else {
		rl = NewRateLimiter(DefaultRateLimitConfig())
	}

	prefix := cfg.TaskIDPrefix
	if prefix == "" {
		prefix = "MSG"
	}

	lg := cfg.Log
	if lg == nil {
		lg = logging.WithComponent("comms.handler")
	}

	return &Handler{
		messenger:      cfg.Messenger,
		runner:         cfg.Runner,
		projects:       cfg.Projects,
		projectPath:    cfg.ProjectPath,
		rateLimit:      rl,
		llmClassifier:  cfg.LLMClassifier,
		convStore:      cfg.ConvStore,
		memberResolver: cfg.MemberResolver,
		store:          cfg.Store,
		taskIDPrefix:   prefix,
		log:            lg,
		activeProject:  make(map[string]string),
		pendingTasks:   make(map[string]*PendingTask),
		runningTasks:   make(map[string]*RunningTask),
		lastSender:     make(map[string]string),
	}
}

// HandleMessage is the main entry point for processing an incoming message.
// It performs rate limiting, intent detection, and dispatches to the appropriate handler.
//
// This is the shared chokepoint for Telegram/Slack/Discord inbound text.
// Every platform adapter populates IncomingMessage.Text (and optionally
// VoiceText) here, so sanitizing once in this function is equivalent to
// sanitizing at every chat adapter site. See internal/text/sanitize.go
// for the threat model.
func (h *Handler) HandleMessage(ctx context.Context, msg *IncomingMessage) {
	// Strip invisible Unicode format characters before any downstream
	// logic reads the message. This also means confirmation echoes,
	// intent routing, and memory writes all see the cleaned text.
	var textStripped, voiceStripped int
	msg.Text, textStripped = texthelper.SanitizeUntrusted(msg.Text)
	msg.VoiceText, voiceStripped = texthelper.SanitizeUntrusted(msg.VoiceText)
	if textStripped+voiceStripped > 0 {
		h.log.Warn("invisible_unicode_stripped",
			slog.String("source", msg.Platform),
			slog.String("context_id", msg.ContextID),
			slog.String("sender_id", msg.SenderID),
			slog.Int("text_stripped", textStripped),
			slog.Int("voice_stripped", voiceStripped),
		)
	}

	contextID := msg.ContextID
	text := msg.Text

	// Track sender for RBAC
	if msg.SenderID != "" {
		h.mu.Lock()
		h.lastSender[contextID] = msg.SenderID
		h.mu.Unlock()
	}

	// Rate limit check
	if !h.rateLimit.AllowMessage(contextID) {
		h.log.Warn("Message rate limit exceeded", slog.String("context_id", contextID))
		_ = h.messenger.SendText(ctx, contextID, "⚠️ Rate limit exceeded. Please wait before sending more messages.")
		return
	}

	// Handle callback (button press) — check pending confirmation
	if msg.IsCallback {
		_ = h.messenger.AcknowledgeCallback(ctx, msg.CallbackID)
		confirmed := msg.ActionID == "execute" || msg.ActionID == "confirm" || msg.ActionID == "yes"
		h.handleConfirmation(ctx, contextID, msg.ThreadID, confirmed)
		return
	}

	// Text-based confirmation shortcuts
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "yes" || lower == "y" || lower == "confirm" || lower == "ok" {
		h.handleConfirmation(ctx, contextID, msg.ThreadID, true)
		return
	}
	if lower == "no" || lower == "n" || lower == "cancel" || lower == "nope" {
		h.handleConfirmation(ctx, contextID, msg.ThreadID, false)
		return
	}

	// Detect intent
	detected := h.detectIntent(ctx, contextID, text)

	// Record user message in conversation history
	if h.convStore != nil {
		h.convStore.Add(contextID, "user", TruncateText(text, 500))
	}

	// Dispatch
	switch detected {
	case intent.IntentGreeting:
		h.handleGreeting(ctx, contextID)
	case intent.IntentQuestion:
		h.handleQuestion(ctx, contextID, msg.ThreadID, text)
	case intent.IntentResearch:
		h.handleResearch(ctx, contextID, msg.ThreadID, text)
	case intent.IntentPlanning:
		h.handlePlanning(ctx, contextID, msg.ThreadID, text)
	case intent.IntentChat:
		h.handleChat(ctx, contextID, msg.ThreadID, text)
	case intent.IntentTask:
		h.handleTask(ctx, contextID, msg.ThreadID, text, msg.SenderID)
	default:
		// Fallback: treat as task
		h.handleTask(ctx, contextID, msg.ThreadID, text, msg.SenderID)
	}
}

// ---------- intent detection ----------

func (h *Handler) detectIntent(ctx context.Context, contextID, text string) intent.Intent {
	// Fast path: commands
	if strings.HasPrefix(text, "/") {
		return intent.IntentCommand
	}

	// Fast path: greeting-prefixed messages (e.g. "Hello! How is it going?")
	// Must be checked before IsClearQuestion so that greetings with trailing
	// questions don't get misclassified as codebase questions.
	if intent.StartsWithGreeting(text) {
		return intent.IntentGreeting
	}

	// Fast path: clear questions
	if intent.IsClearQuestion(text) {
		return intent.IntentQuestion
	}

	// LLM classification if available
	if h.llmClassifier != nil {
		classifyCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		var history []intent.ConversationMessage
		if h.convStore != nil {
			history = h.convStore.Get(contextID)
		}

		detected, err := h.llmClassifier.Classify(classifyCtx, history, text)
		if err != nil {
			h.log.Debug("LLM classification failed, using regex", slog.Any("error", err))
			return intent.DetectIntent(text)
		}

		h.log.Debug("LLM classified intent",
			slog.String("context_id", contextID),
			slog.String("intent", string(detected)),
			slog.String("text", TruncateText(text, 50)))
		return detected
	}

	return intent.DetectIntent(text)
}
