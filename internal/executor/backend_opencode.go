package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
)

// OpenCodeBackend implements Backend for OpenCode server.
// OpenCode uses a client/server architecture where the server runs locally
// and clients communicate via HTTP/SSE.
type OpenCodeBackend struct {
	config     *OpenCodeConfig
	log        *slog.Logger
	httpClient *http.Client
	serverCmd  *exec.Cmd
	serverMu   sync.Mutex
}

// NewOpenCodeBackend creates a new OpenCode backend.
func NewOpenCodeBackend(config *OpenCodeConfig) *OpenCodeBackend {
	if config == nil {
		config = &OpenCodeConfig{
			ServerURL:       "http://127.0.0.1:4096",
			Model:           "anthropic/claude-sonnet-4",
			Provider:        "anthropic",
			AutoStartServer: true,
			ServerCommand:   "opencode serve",
		}
	}
	if config.ServerURL == "" {
		config.ServerURL = "http://127.0.0.1:4096"
	}
	if config.Model == "" {
		config.Model = "anthropic/claude-sonnet-4"
	}

	return &OpenCodeBackend{
		config: config,
		log:    logging.WithComponent("executor.opencode"),
		httpClient: &http.Client{
			Timeout: config.EffectiveRequestTimeout(),
		},
	}
}

// Name returns the backend identifier.
func (b *OpenCodeBackend) Name() string {
	return BackendTypeOpenCode
}

// IsAvailable checks if OpenCode server is running or can be started.
func (b *OpenCodeBackend) IsAvailable() bool {
	// Check if server is already running
	if b.isServerRunning() {
		return true
	}

	// Check if opencode CLI is installed
	_, err := exec.LookPath("opencode")
	return err == nil
}

// isServerRunning checks if the OpenCode server is responding.
func (b *OpenCodeBackend) isServerRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", b.config.ServerURL+"/global/health", nil)
	if err != nil {
		return false
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}

// startServer starts the OpenCode server if configured.
func (b *OpenCodeBackend) startServer(ctx context.Context) error {
	b.serverMu.Lock()
	defer b.serverMu.Unlock()

	// Already running
	if b.isServerRunning() {
		return nil
	}

	if !b.config.AutoStartServer {
		return fmt.Errorf("OpenCode server not running and auto-start disabled")
	}

	b.log.Info("Starting OpenCode server", slog.String("command", b.config.ServerCommand))

	// Parse server command
	parts := strings.Fields(b.config.ServerCommand)
	if len(parts) == 0 {
		parts = []string{"opencode", "serve"}
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start OpenCode server: %w", err)
	}

	b.serverCmd = cmd

	// Wait for server to be ready
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if b.isServerRunning() {
			b.log.Info("OpenCode server ready")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("OpenCode server failed to start within timeout")
}

// Execute runs a prompt through OpenCode server.
func (b *OpenCodeBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	// Ensure server is running
	if err := b.startServer(ctx); err != nil {
		return nil, err
	}

	// Create a new session
	sessionID, err := b.createSession(ctx, opts.ProjectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	b.log.Debug("Created OpenCode session", slog.String("session_id", sessionID))

	// Send the message and stream response
	result, err := b.sendMessage(ctx, sessionID, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	return result, nil
}

// createSession creates a new OpenCode session.
func (b *OpenCodeBackend) createSession(ctx context.Context, projectPath string) (string, error) {
	// OpenCode session creation payload
	payload := map[string]interface{}{
		"path": projectPath,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.config.ServerURL+"/session", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// OpenCode attached mode resolves the project directory from the
	// `x-opencode-directory` header (or `directory` query param), not from the
	// JSON `path` field. Without it, sessions are created in the server's cwd
	// rather than the target project. GH-2415.
	if projectPath != "" {
		req.Header.Set("X-OpenCode-Directory", url.QueryEscape(projectPath))
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("session creation failed: %s", string(body))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.ID, nil
}

// sendMessage sends a prompt to an OpenCode session and streams the response.
func (b *OpenCodeBackend) sendMessage(ctx context.Context, sessionID string, opts ExecuteOptions) (*BackendResult, error) {
	result := &BackendResult{}

	// Build message payload
	payload := map[string]interface{}{
		"parts": []map[string]interface{}{
			{
				"type": "text",
				"text": opts.Prompt,
			},
		},
	}

	// OpenCode v1.4.x's Hono+Zod validator requires `model` to be either
	// {providerID, modelID} or omitted. Sending a plain string fails with
	// HTTP 400 "invalid_type" before the handler runs (GH-2413). See
	// https://github.com/anomalyco/opencode/blob/v1.4.6/packages/opencode/src/session/prompt.ts
	if ref := b.resolveModelRef(); ref != nil {
		payload["model"] = ref
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// Use async endpoint for streaming
	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/session/%s/message", b.config.ServerURL, sessionID),
		bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	// See createSession: attached-mode directory resolution requires this
	// header on the message endpoint too. GH-2415.
	if opts.ProjectPath != "" {
		req.Header.Set("X-OpenCode-Directory", url.QueryEscape(opts.ProjectPath))
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("message failed: %s", string(body))
	}

	// Check if streaming response
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		// Parse SSE stream
		if err := b.parseSSEStream(resp.Body, opts, result); err != nil {
			return nil, err
		}
	} else {
		// OpenCode v1.4.x POST /session/:id/message returns application/json
		// with the shape {info: AssistantMessage, parts: Part[]}, regardless of
		// the Accept: text/event-stream request header. Parse that shape and
		// project it onto BackendResult / BackendEvent. (GH-2409)
		if err := b.parseAssistantResponse(resp.Body, opts, result); err != nil {
			return nil, err
		}
	}

	// If no error set, mark as successful
	if result.Error == "" {
		result.Success = true
	}

	return result, nil
}

// resolveModelRef builds the OpenCode model reference from config.
// Returns nil when no model is configured, signalling the caller to omit
// the field so the server falls back to its default model.
//
// Resolution rules:
//   - "providerID/modelID" → split on first "/"
//   - bare "modelID" + config.Provider → use config.Provider as providerID
//   - bare "modelID" with no provider → empty providerID (server may reject;
//     we still send what we have rather than silently dropping the model)
func (b *OpenCodeBackend) resolveModelRef() *ocModelRef {
	model := strings.TrimSpace(b.config.Model)
	if model == "" {
		return nil
	}
	if i := strings.Index(model, "/"); i > 0 && i < len(model)-1 {
		return &ocModelRef{
			ProviderID: model[:i],
			ModelID:    model[i+1:],
		}
	}
	return &ocModelRef{
		ProviderID: strings.TrimSpace(b.config.Provider),
		ModelID:    model,
	}
}

// StopServer stops the managed OpenCode server if running.
func (b *OpenCodeBackend) StopServer() error {
	b.serverMu.Lock()
	defer b.serverMu.Unlock()

	if b.serverCmd != nil && b.serverCmd.Process != nil {
		b.log.Info("Stopping OpenCode server")
		return b.serverCmd.Process.Kill()
	}
	return nil
}
