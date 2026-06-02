package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/qf-studio/pilot/internal/codexruntime"
	"github.com/qf-studio/pilot/internal/logging"
)

const (
	runtimeActionStart = "codexruntime.start"
	runtimeSource      = "codexruntime"
)

type runtimeTaskPayload struct {
	Action  string `json:"action"`
	Prompt  string `json:"prompt"`
	Cwd     string `json:"cwd"`
	Model   string `json:"model,omitempty"`
	Sandbox string `json:"sandbox,omitempty"`
	Command string `json:"command,omitempty"`
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

func (s *Server) registerRuntimeHandlers() {
	s.router.RegisterMessageHandler(MessageTypeTask, s.handleRuntimeTask)
}

func (s *Server) handleRuntimeTask(session *Session, payload json.RawMessage) {
	var task runtimeTaskPayload
	if err := json.Unmarshal(payload, &task); err != nil {
		_ = sendRuntimeError(session, fmt.Errorf("invalid runtime task payload: %w", err))
		return
	}
	if task.Action != runtimeActionStart {
		return
	}

	go s.runRuntimeTask(session, task)
}

func (s *Server) runRuntimeTask(session *Session, task runtimeTaskPayload) {
	if err := runRuntimeSession(context.Background(), session, task); err != nil {
		logging.WithComponent("gateway").Warn("codex runtime session failed", slog.Any("error", err))
		_ = sendRuntimeError(session, err)
	}
}

func runRuntimeSession(ctx context.Context, session *Session, task runtimeTaskPayload) error {
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

	sandbox := codexruntime.SandboxReadOnly
	if task.Sandbox != "" {
		parsed, err := parseRuntimeSandbox(task.Sandbox)
		if err != nil {
			return err
		}
		sandbox = parsed
	}

	command := task.Command
	if command == "" {
		command = "codex"
	}

	client, err := codexruntime.Start(ctx, codexruntime.Config{
		Command: command,
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
		Model:              task.Model,
		ApprovalPolicy:     codexruntime.ApprovalNever,
		ApprovalsReviewer:  codexruntime.ApprovalsReviewerUser,
		Sandbox:            sandbox,
		Ephemeral:          &ephemeral,
		ThreadSource:       codexruntime.ThreadSourceUser,
		SessionStartSource: codexruntime.ThreadStartSourceStartup,
	})
	if err != nil {
		return err
	}

	if _, err := client.TurnStart(ctx, codexruntime.TurnStartParams{
		ThreadID:       thread.Thread.ID,
		Cwd:            absCwd,
		ApprovalPolicy: codexruntime.ApprovalNever,
		Model:          task.Model,
		Input:          []codexruntime.UserInput{codexruntime.TextUserInput(task.Prompt)},
	}); err != nil {
		return err
	}

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
				return nil
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
			if err := sendRuntimeApprovalRequest(session, req); err != nil {
				return err
			}
			if err := declineRuntimeServerRequest(client, req); err != nil {
				return err
			}
		case err := <-client.Errors():
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func sendRuntimeApprovalRequest(session *Session, req codexruntime.Message) error {
	requestID, _ := codexruntime.ParseID(req.ID)
	return sendRuntimePayload(session, runtimeProgressPayload{
		Source:    runtimeSource,
		Kind:      "approval_request",
		RequestID: requestID,
		Method:    req.Method,
		Params:    req.Params,
		Choices:   runtimeApprovalChoices(req.Method),
	})
}

func sendRuntimeError(session *Session, err error) error {
	return sendRuntimePayload(session, runtimeProgressPayload{
		Source: runtimeSource,
		Kind:   "error",
		Error:  err.Error(),
	})
}

func sendRuntimePayload(session *Session, payload runtimeProgressPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg, err := json.Marshal(Message{
		Type:    MessageTypeProgress,
		Payload: raw,
	})
	if err != nil {
		return err
	}
	return session.Send(msg)
}

func parseRuntimeSandbox(value string) (codexruntime.SandboxMode, error) {
	switch codexruntime.SandboxMode(value) {
	case codexruntime.SandboxReadOnly:
		return codexruntime.SandboxReadOnly, nil
	case codexruntime.SandboxWorkspaceWrite:
		return codexruntime.SandboxWorkspaceWrite, nil
	case codexruntime.SandboxDangerFull:
		return codexruntime.SandboxDangerFull, nil
	default:
		return "", fmt.Errorf("invalid sandbox %q", value)
	}
}

func declineRuntimeServerRequest(client *codexruntime.Client, req codexruntime.Message) error {
	switch req.Method {
	case "execCommandApproval", "applyPatchApproval":
		return client.RespondReviewApproval(req, codexruntime.ReviewDenied)
	case "item/commandExecution/requestApproval":
		return client.RespondCommandExecutionApproval(req, codexruntime.CommandExecutionDecline)
	case "item/fileChange/requestApproval":
		return client.RespondFileChangeApproval(req, codexruntime.FileChangeDecline)
	case "item/permissions/requestApproval":
		return client.RespondPermissionsApproval(req, codexruntime.PermissionsApprovalResponse{
			Permissions: codexruntime.GrantedPermissionProfile{},
			Scope:       codexruntime.PermissionGrantTurn,
		})
	default:
		return client.RespondError(req, -32601, "unsupported server request", nil)
	}
}

func runtimeApprovalChoices(method string) []string {
	switch method {
	case "execCommandApproval", "applyPatchApproval":
		return []string{"approved", "approved_for_session", "denied", "abort"}
	case "item/commandExecution/requestApproval":
		return []string{"accept", "acceptForSession", "decline", "cancel"}
	case "item/fileChange/requestApproval":
		return []string{"accept", "acceptForSession", "decline", "cancel"}
	case "item/permissions/requestApproval":
		return []string{"turn", "session"}
	default:
		return nil
	}
}
