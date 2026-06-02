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

func newRuntimeApprovalRegistry() *runtimeApprovalRegistry {
	return &runtimeApprovalRegistry{
		pending: make(map[runtimeApprovalKey]chan runtimeApprovalResponsePayload),
	}
}

func newRuntimeSessionRegistry() *runtimeSessionRegistry {
	return &runtimeSessionRegistry{
		sessions: make(map[string]*runtimeSessionController),
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

	sandbox := codexruntime.SandboxReadOnly
	if task.Sandbox != "" {
		parsed, err := parseRuntimeSandbox(task.Sandbox)
		if err != nil {
			return err
		}
		sandbox = parsed
	}

	client, err := codexruntime.Start(ctx, codexruntime.Config{
		Command: "codex",
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

	controller := newRuntimeSessionController(client, thread.Thread.ID, absCwd, task.Model)
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

func newRuntimeSessionController(client *codexruntime.Client, threadID, cwd, model string) *runtimeSessionController {
	return &runtimeSessionController{
		client:   client,
		threadID: threadID,
		cwd:      cwd,
		model:    model,
		turns:    make(chan runtimeTurnRequest, 8),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (c *runtimeSessionController) setRunning(running bool) {
	c.mu.Lock()
	c.running = running
	c.mu.Unlock()
}

func (c *runtimeSessionController) startTurn(ctx context.Context, prompt string) error {
	if prompt == "" {
		return errors.New("prompt is required")
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("runtime session is closed")
	}
	if c.running {
		c.mu.Unlock()
		return errors.New("runtime turn is already running")
	}
	c.running = true
	c.mu.Unlock()

	if _, err := c.client.TurnStart(ctx, codexruntime.TurnStartParams{
		ThreadID:       c.threadID,
		Cwd:            c.cwd,
		ApprovalPolicy: codexruntime.ApprovalNever,
		Model:          c.model,
		Input:          []codexruntime.UserInput{codexruntime.TextUserInput(prompt)},
	}); err != nil {
		c.setRunning(false)
		return err
	}
	return nil
}

func (c *runtimeSessionController) enqueueTurn(prompt string) error {
	if prompt == "" {
		return errors.New("prompt is required")
	}

	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return errors.New("runtime session is closed")
	}

	errCh := make(chan error, 1)
	select {
	case c.turns <- runtimeTurnRequest{prompt: prompt, err: errCh}:
	case <-c.done:
		return errors.New("runtime session is closed")
	default:
		return errors.New("runtime turn queue is full")
	}

	select {
	case err := <-errCh:
		return err
	case <-c.done:
		return errors.New("runtime session is closed")
	}
}

func (c *runtimeSessionController) close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	c.stopOnce.Do(func() {
		close(c.stop)
	})
	if c.client != nil {
		_ = c.client.Close()
	}
}

func (c *runtimeSessionController) finish() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	c.doneOnce.Do(func() {
		close(c.done)
	})
}

func (r *runtimeSessionRegistry) replace(sessionID string, controller *runtimeSessionController) {
	r.mu.Lock()
	previous := r.sessions[sessionID]
	r.sessions[sessionID] = controller
	r.mu.Unlock()

	if previous != nil {
		previous.close()
	}
}

func (r *runtimeSessionRegistry) get(sessionID string) (*runtimeSessionController, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	controller, ok := r.sessions[sessionID]
	return controller, ok
}

func (r *runtimeSessionRegistry) remove(sessionID string) {
	r.mu.Lock()
	delete(r.sessions, sessionID)
	r.mu.Unlock()
}

func (r *runtimeSessionRegistry) removeIf(sessionID string, controller *runtimeSessionController) {
	r.mu.Lock()
	if r.sessions[sessionID] == controller {
		delete(r.sessions, sessionID)
	}
	r.mu.Unlock()
}

func (r *runtimeSessionRegistry) close(sessionID string) {
	r.mu.Lock()
	controller := r.sessions[sessionID]
	delete(r.sessions, sessionID)
	r.mu.Unlock()

	if controller != nil {
		controller.close()
	}
}

func (s *Server) startRuntimeTurn(sessionID, prompt string) error {
	controller, ok := s.runtimeSessions.get(sessionID)
	if !ok {
		return errors.New("runtime session is not active")
	}
	return controller.enqueueTurn(prompt)
}

func (s *Server) awaitRuntimeApproval(ctx context.Context, session *Session, req codexruntime.Message) (runtimeApprovalResponsePayload, error) {
	requestID, err := runtimeRequestID(req)
	if err != nil {
		return runtimeApprovalResponsePayload{}, err
	}

	ch, cancel, err := s.runtimeApprovals.register(session.ID, requestID)
	if err != nil {
		return runtimeApprovalResponsePayload{}, err
	}
	defer cancel()

	if err := sendRuntimeApprovalRequest(session, req); err != nil {
		return runtimeApprovalResponsePayload{}, err
	}

	timer := time.NewTimer(runtimeApprovalTimeout)
	defer timer.Stop()

	select {
	case response := <-ch:
		return response, nil
	case <-timer.C:
		return defaultRuntimeApprovalResponse(req.Method, requestID), nil
	case <-ctx.Done():
		return runtimeApprovalResponsePayload{}, ctx.Err()
	}
}

func sendRuntimeApprovalRequest(session *Session, req codexruntime.Message) error {
	requestID, _ := runtimeRequestID(req)
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

func (r *runtimeApprovalRegistry) register(sessionID string, requestID int) (<-chan runtimeApprovalResponsePayload, func(), error) {
	key := runtimeApprovalKey{sessionID: sessionID, requestID: requestID}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.pending[key]; ok {
		return nil, nil, fmt.Errorf("runtime approval request %d is already pending", requestID)
	}

	ch := make(chan runtimeApprovalResponsePayload, 1)
	r.pending[key] = ch
	cancel := func() {
		r.mu.Lock()
		delete(r.pending, key)
		r.mu.Unlock()
	}

	return ch, cancel, nil
}

func (r *runtimeApprovalRegistry) resolve(sessionID string, response runtimeApprovalResponsePayload) error {
	key := runtimeApprovalKey{sessionID: sessionID, requestID: response.RequestID}

	r.mu.Lock()
	ch, ok := r.pending[key]
	if ok {
		delete(r.pending, key)
	}
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("runtime approval request %d is not pending", response.RequestID)
	}

	ch <- response
	return nil
}

func parseRuntimeApprovalResponse(task runtimeTaskPayload) (runtimeApprovalResponsePayload, error) {
	if len(task.RequestID) == 0 {
		return runtimeApprovalResponsePayload{}, errors.New("requestId is required")
	}
	requestID, err := codexruntime.ParseID(task.RequestID)
	if err != nil {
		return runtimeApprovalResponsePayload{}, fmt.Errorf("invalid requestId: %w", err)
	}
	if task.Decision == "" && task.Scope == "" {
		return runtimeApprovalResponsePayload{}, errors.New("decision or scope is required")
	}
	return runtimeApprovalResponsePayload{
		RequestID: requestID,
		Decision:  task.Decision,
		Scope:     task.Scope,
	}, nil
}

func runtimeRequestID(req codexruntime.Message) (int, error) {
	if len(req.ID) == 0 {
		return 0, errors.New("request id is required")
	}
	return codexruntime.ParseID(req.ID)
}

func defaultRuntimeApprovalResponse(method string, requestID int) runtimeApprovalResponsePayload {
	switch method {
	case "execCommandApproval", "applyPatchApproval":
		return runtimeApprovalResponsePayload{RequestID: requestID, Decision: string(codexruntime.ReviewDenied)}
	case "item/commandExecution/requestApproval":
		return runtimeApprovalResponsePayload{RequestID: requestID, Decision: string(codexruntime.CommandExecutionDecline)}
	case "item/fileChange/requestApproval":
		return runtimeApprovalResponsePayload{RequestID: requestID, Decision: string(codexruntime.FileChangeDecline)}
	case "item/permissions/requestApproval":
		return runtimeApprovalResponsePayload{RequestID: requestID, Scope: string(codexruntime.PermissionGrantTurn)}
	default:
		return runtimeApprovalResponsePayload{RequestID: requestID}
	}
}

func respondRuntimeServerRequest(client *codexruntime.Client, req codexruntime.Message, response runtimeApprovalResponsePayload) error {
	switch req.Method {
	case "execCommandApproval", "applyPatchApproval":
		return client.RespondReviewApproval(req, codexruntime.ReviewDecision(response.Decision))
	case "item/commandExecution/requestApproval":
		return client.RespondCommandExecutionApproval(req, codexruntime.CommandExecutionApprovalDecision(response.Decision))
	case "item/fileChange/requestApproval":
		return client.RespondFileChangeApproval(req, codexruntime.FileChangeApprovalDecision(response.Decision))
	case "item/permissions/requestApproval":
		scope := response.Scope
		if scope == "" {
			scope = response.Decision
		}
		return client.RespondPermissionsApproval(req, codexruntime.PermissionsApprovalResponse{
			Permissions: codexruntime.GrantedPermissionProfile{},
			Scope:       codexruntime.PermissionGrantScope(scope),
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
