package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/codexruntime"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

const (
	runtimeActionStart           = "codexruntime.start"
	runtimeActionTurn            = "codexruntime.turn"
	runtimeActionStop            = "codexruntime.stop"
	runtimeActionApprovalRespond = "codexruntime.approval.respond"
	runtimeApprovalTimeout       = 5 * time.Minute
	runtimeSource                = "codexruntime"
)

type runtimeTaskPayload struct {
	Action    string          `json:"action"`
	Prompt    string          `json:"prompt"`
	Cwd       string          `json:"cwd"`
	Model     string          `json:"model,omitempty"`
	Sandbox   string          `json:"sandbox,omitempty"`
	RequestID json.RawMessage `json:"requestId,omitempty"`
	Decision  string          `json:"decision,omitempty"`
	Scope     string          `json:"scope,omitempty"`
}

// CodexRuntimeConfig configures gateway WebSocket sessions backed by `codex app-server`.
type CodexRuntimeConfig struct {
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
	Model   string   `yaml:"model,omitempty"`
	Sandbox string   `yaml:"sandbox,omitempty"`
	// DisablePriming opts out of injecting the .agent guidance preamble on the
	// first turn (see BuildGuidancePreamble). Priming is on by default.
	DisablePriming bool `yaml:"disable_priming,omitempty"`
}

type resolvedCodexRuntimeConfig struct {
	Command        string
	Args           []string
	Model          string
	Sandbox        codexruntime.SandboxMode
	DisablePriming bool
}

type runtimeProgressPayload struct {
	Source    string              `json:"source"`
	Kind      string              `json:"kind"`
	Event     *codexruntime.Event `json:"event,omitempty"`
	Error     string              `json:"error,omitempty"`
	RequestID any                 `json:"requestId,omitempty"`
	Method    string              `json:"method,omitempty"`
	Params    json.RawMessage     `json:"params,omitempty"`
	Choices   []string            `json:"choices,omitempty"`
}

type runtimeApprovalResponsePayload struct {
	RequestID int
	Decision  string
	Scope     string
}

type runtimeApprovalKey struct {
	sessionID string
	requestID int
}

type runtimeApprovalRegistry struct {
	mu      sync.Mutex
	pending map[runtimeApprovalKey]chan runtimeApprovalResponsePayload
}

type runtimeSessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*runtimeSessionController
}

type runtimeSessionController struct {
	client   *codexruntime.Client
	threadID string
	cwd      string
	model    string
	turns    chan runtimeTurnRequest
	stop     chan struct{}
	done     chan struct{}

	mu       sync.Mutex
	running  bool
	closed   bool
	stopOnce sync.Once
	doneOnce sync.Once
}

type runtimeTurnRequest struct {
	prompt string
	err    chan error
}

func DefaultCodexRuntimeConfig() *CodexRuntimeConfig {
	return &CodexRuntimeConfig{
		Command: "codex",
		Sandbox: string(codexruntime.SandboxReadOnly),
	}
}

func (s *Server) registerRuntimeHandlers() {
	s.router.RegisterMessageHandler(MessageTypeTask, s.handleRuntimeTask)
}

func (s *Server) handleRuntimeTask(session *Session, payload json.RawMessage) {
	var task runtimeTaskPayload
	if err := json.Unmarshal(payload, &task); err != nil {
		_ = sendRuntimeError(session, fmt.Errorf("invalid runtime task payload: %w", err))
		return
	}
	switch task.Action {
	case runtimeActionStart:
		go s.runRuntimeTask(session, task)
	case runtimeActionTurn:
		if err := s.startRuntimeTurn(session.ID, task.Prompt); err != nil {
			_ = sendRuntimeError(session, err)
			return
		}
	case runtimeActionStop:
		s.runtimeSessions.close(session.ID)
	case runtimeActionApprovalRespond:
		response, err := parseRuntimeApprovalResponse(task)
		if err != nil {
			_ = sendRuntimeError(session, err)
			return
		}
		if err := s.runtimeApprovals.resolve(session.ID, response); err != nil {
			_ = sendRuntimeError(session, err)
			return
		}
	default:
		return
	}
}

func (s *Server) runRuntimeTask(session *Session, task runtimeTaskPayload) {
	if err := s.runRuntimeSession(context.Background(), session, task); err != nil {
		logging.WithComponent("gateway").Warn("codex runtime session failed", slog.Any("error", err))
		_ = sendRuntimeError(session, err)
	}
}

func (s *Server) runRuntimeSession(ctx context.Context, session *Session, task runtimeTaskPayload) error {
	if task.Prompt == "" {
		return errors.New("prompt is required")
	}

	cwd := task.Cwd
	if cwd == "" {
		cwd = "."
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}

	runtimeConfig, err := s.resolveCodexRuntimeConfig(task)
	if err != nil {
		return err
	}

	client, err := codexruntime.Start(ctx, codexruntime.Config{
		Command: runtimeConfig.Command,
		Args:    runtimeConfig.Args,
		Cwd:     absCwd,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	title := "Pilot Gateway"
	if _, err := client.Initialize(ctx, codexruntime.InitializeParams{
		ClientInfo: codexruntime.ClientInfo{
			Name:    "pilot-gateway",
			Title:   &title,
			Version: "1.0.0",
		},
		Capabilities: &codexruntime.InitializeCapabilities{
			ExperimentalAPI:           true,
			RequestAttestation:        false,
			OptOutNotificationMethods: []string{},
		},
	}); err != nil {
		return err
	}
	if err := client.Notify("initialized", nil); err != nil {
		return err
	}

	ephemeral := true
	thread, err := client.ThreadStart(ctx, codexruntime.ThreadStartParams{
		Cwd:                absCwd,
		Model:              runtimeConfig.Model,
		ApprovalPolicy:     codexruntime.ApprovalNever,
		ApprovalsReviewer:  codexruntime.ApprovalsReviewerUser,
		Sandbox:            runtimeConfig.Sandbox,
		Ephemeral:          &ephemeral,
		ThreadSource:       codexruntime.ThreadSourceUser,
		SessionStartSource: codexruntime.ThreadStartSourceStartup,
	})
	if err != nil {
		return err
	}

	// Prime the first turn with .agent guidance so the codex-app-server backend
	// receives the same project context Claude Code gets via BuildPrompt. Only
	// the start turn is primed; follow-up turns (startTurn) carry the raw prompt.
	firstPrompt := task.Prompt
	if !runtimeConfig.DisablePriming {
		if preamble := executor.BuildGuidancePreamble(filepath.Join(absCwd, ".agent"), task.Prompt); preamble != "" {
			firstPrompt = preamble + "\n\n" + task.Prompt
		}
	}

	if _, err := client.TurnStart(ctx, codexruntime.TurnStartParams{
		ThreadID:       thread.Thread.ID,
		Cwd:            absCwd,
		ApprovalPolicy: codexruntime.ApprovalNever,
		Model:          runtimeConfig.Model,
		Input:          []codexruntime.UserInput{codexruntime.TextUserInput(firstPrompt)},
	}); err != nil {
		return err
	}

	controller := newRuntimeSessionController(client, thread.Thread.ID, absCwd, runtimeConfig.Model)
	controller.setRunning(true)
	defer func() {
		controller.finish()
		s.runtimeSessions.removeIf(session.ID, controller)
	}()
	s.runtimeSessions.replace(session.ID, controller)

	for {
		select {
		case msg, ok := <-client.Notifications():
			if !ok {
				return errors.New("codex runtime notification stream closed")
			}
			event, err := codexruntime.MapNotification(msg)
			if err != nil {
				return err
			}
			if err := sendRuntimePayload(session, runtimeProgressPayload{
				Source: runtimeSource,
				Kind:   "event",
				Event:  &event,
			}); err != nil {
				return err
			}
			if event.Type == codexruntime.EventTurnCompleted {
				controller.setRunning(false)
				continue
			}
			if event.Type == codexruntime.EventError {
				if event.Error == "" {
					event.Error = "codex runtime error"
				}
				return errors.New(event.Error)
			}
		case req, ok := <-client.ServerRequests():
			if !ok {
				continue
			}
			response, err := s.awaitRuntimeApproval(ctx, session, req)
			if err != nil {
				return err
			}
			if err := respondRuntimeServerRequest(client, req, response); err != nil {
				return err
			}
		case turn := <-controller.turns:
			turn.err <- controller.startTurn(ctx, turn.prompt)
		case err := <-client.Errors():
			return err
		case <-controller.stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *Server) resolveCodexRuntimeConfig(task runtimeTaskPayload) (resolvedCodexRuntimeConfig, error) {
	cfg := DefaultCodexRuntimeConfig()
	if s != nil && s.config != nil && s.config.CodexRuntime != nil {
		configured := s.config.CodexRuntime
		if configured.Command != "" {
			cfg.Command = configured.Command
		}
		if len(configured.Args) > 0 {
			cfg.Args = append([]string(nil), configured.Args...)
		}
		if configured.Model != "" {
			cfg.Model = configured.Model
		}
		if configured.Sandbox != "" {
			cfg.Sandbox = configured.Sandbox
		}
	}
	if task.Model != "" {
		cfg.Model = task.Model
	}
	if task.Sandbox != "" {
		cfg.Sandbox = task.Sandbox
	}

	sandbox, err := parseRuntimeSandbox(cfg.Sandbox)
	if err != nil {
		return resolvedCodexRuntimeConfig{}, err
	}
	return resolvedCodexRuntimeConfig{
		Command:        cfg.Command,
		Args:           cfg.Args,
		Model:          cfg.Model,
		Sandbox:        sandbox,
		DisablePriming: cfg.DisablePriming,
	}, nil
}
