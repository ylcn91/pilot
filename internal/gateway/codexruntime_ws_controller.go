package gateway

import (
	"context"
	"errors"

	"github.com/ylcn91/pilot/internal/codexruntime"
)

func newRuntimeSessionRegistry() *runtimeSessionRegistry {
	return &runtimeSessionRegistry{
		sessions: make(map[string]*runtimeSessionController),
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
	controller, ok := s.codex.sessions.get(sessionID)
	if !ok {
		return errors.New("runtime session is not active")
	}
	return controller.enqueueTurn(prompt)
}
