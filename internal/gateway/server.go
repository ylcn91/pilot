package gateway

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// ReadinessChecker is an interface for components that can report their readiness.
// Implement this interface to register health checks with the gateway server.
type ReadinessChecker interface {
	// Name returns a unique identifier for this checker.
	Name() string
	// Ready returns true if the component is ready to accept traffic.
	Ready() bool
}

// livenessState tracks metrics for liveness checks.
type livenessState struct {
	panicCount      atomic.Int64
	lastPanicTime   atomic.Int64 // Unix timestamp
	lastHeartbeat   atomic.Int64 // Unix timestamp
	maxGoroutines   int
	panicWindowSecs int64
}

// AutopilotPRState holds the state of a single PR tracked by autopilot.
// Used by AutopilotProvider to decouple gateway from autopilot package.
type AutopilotPRState struct {
	PRNumber   int
	PRURL      string
	Stage      string
	CIStatus   string
	Error      string
	BranchName string
}

// AutopilotProvider exposes autopilot state to the gateway API.
type AutopilotProvider interface {
	GetEnvironment() string
	GetActivePRs() []*AutopilotPRState
	GetFailureCount() int
	IsAutoReleaseEnabled() bool
}

// ArchitectProvider exposes Architect findings to the gateway API.
// Findings originate from the Architect family (e.g. Radar, Dependency-Doctor)
// and are surfaced read-only via /api/v1/architect. The provider is injected
// as an interface so the gateway never imports internal/architect; it depends
// only on the leaf package internal/pilotapi for the Finding shape.
type ArchitectProvider interface {
	Findings() []pilotapi.Finding
}

// authState groups the request-authentication concerns: WebSocket/session
// management plus the per-adapter webhook signature material. All of these
// are set once at construction (or via Set* before Start) and then read
// without locking.
type authState struct {
	auth     *Authenticator
	sessions *SessionManager
	// githubWebhookSecret is the secret for GitHub webhook signature validation.
	githubWebhookSecret string
	// linearWebhookPublicKey is the Ed25519 public key for Linear webhook
	// signature validation (TASK-295). Nil = verification disabled.
	linearWebhookPublicKey ed25519.PublicKey
}

// providerState groups the read-only observability providers surfaced over the
// REST/metrics API: Prometheus export, alert metrics, autopilot state and
// Architect findings. Every field is guarded by Server.mu.
type providerState struct {
	prometheusExporter *PrometheusExporter
	alertsSource       AlertMetricsSource
	autopilot          AutopilotProvider
	architect          ArchitectProvider
}

// dashboardState groups everything backing the dashboard surface: the metrics
// store, the log-stream store, the git-graph fetcher and the embedded React
// frontend. Every field is guarded by Server.mu.
type dashboardState struct {
	store           DashboardStore
	logStreamStore  LogStreamStore
	gitGraphPath    string          // Project path for git graph API (defaults to ".")
	gitGraphFetcher GitGraphFetcher // Injected to avoid import cycle with internal/dashboard
	fs              fs.FS           // Embedded React frontend (nil if not embedded)
}

// codexRuntimeState groups the Codex app-server runtime registries used by the
// gateway WebSocket sessions. Both are set once at construction and read
// without locking.
type codexRuntimeState struct {
	approvals *runtimeApprovalRegistry
	sessions  *runtimeSessionRegistry
}

// Server is the main gateway server handling WebSocket and HTTP connections.
// It provides a control plane for managing Pilot via WebSocket, receives webhooks
// from external services (Linear, GitHub, Jira, Asana), and exposes REST APIs for status
// and task management. Server is safe for concurrent use.
type Server struct {
	config            *Config
	router            *Router
	upgrader          websocket.Upgrader
	server            *http.Server
	mu                sync.RWMutex
	running           bool
	customHandlers    map[string]http.Handler
	readinessCheckers []ReadinessChecker
	liveness          *livenessState

	authn     authState
	providers providerState
	dashboard dashboardState
	codex     codexRuntimeState
}

// Config holds gateway server configuration including network binding options.
type Config struct {
	// Host is the network interface to bind to (e.g., "127.0.0.1" or "0.0.0.0").
	Host string `yaml:"host"`
	// Port is the TCP port number to listen on.
	Port int `yaml:"port"`
	// CodexRuntime configures the Codex app-server runtime used by gateway WebSocket sessions.
	CodexRuntime *CodexRuntimeConfig `yaml:"codex_runtime,omitempty"`
	// GithubWebhookSecret is the secret for GitHub webhook signature validation.
	// If set, incoming GitHub webhooks must have valid HMAC-SHA256 signatures.
	GithubWebhookSecret string `yaml:"-"` // Set programmatically from adapters config
	// LinearWebhookPublicKey is the Ed25519 public key for Linear webhook
	// signature validation (TASK-295). If non-nil, incoming Linear webhooks
	// without a valid linear-signature header are rejected with 401. If nil,
	// signature verification is skipped and a startup warning is logged.
	LinearWebhookPublicKey ed25519.PublicKey `yaml:"-"` // Set programmatically from adapters config
}

// localhostPrefixes are the allowed origin prefixes for localhost connections.
// Origins must match exactly or be followed by a port (e.g., ":3000").
var localhostPrefixes = []string{
	"http://localhost",
	"http://127.0.0.1",
	"https://localhost",
	"https://127.0.0.1",
}

// isLocalhost checks if the origin is a valid localhost origin.
// Returns true for origins like "http://localhost", "http://localhost:3000",
// but false for "http://localhost.evil.com" (subdomain attack).
func isLocalhost(origin string) bool {
	for _, prefix := range localhostPrefixes {
		if origin == prefix {
			return true
		}
		// Check for port suffix (must start with ":")
		if strings.HasPrefix(origin, prefix+":") {
			return true
		}
	}
	return false
}

// NewServer creates a new gateway server with the given configuration.
// The server is not started until Start is called.
func NewServer(config *Config) *Server {
	return NewServerWithAuth(config, nil)
}

// NewServerWithAuth creates a new gateway server with the given configuration
// and authentication config. Protected API routes will require authentication.
func NewServerWithAuth(config *Config, authConfig *AuthConfig) *Server {
	var auth *Authenticator
	if authConfig != nil {
		auth = NewAuthenticator(authConfig)
	}

	s := &Server{
		config:            config,
		router:            NewRouter(),
		customHandlers:    make(map[string]http.Handler),
		readinessCheckers: make([]ReadinessChecker, 0),
		authn: authState{
			auth:                   auth,
			sessions:               NewSessionManager(),
			githubWebhookSecret:    config.GithubWebhookSecret,
			linearWebhookPublicKey: config.LinearWebhookPublicKey,
		},
		codex: codexRuntimeState{
			approvals: newRuntimeApprovalRegistry(),
			sessions:  newRuntimeSessionRegistry(),
		},
		liveness: &livenessState{
			maxGoroutines:   1000,
			panicWindowSecs: 300, // 5 minutes
		},
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				// Allow requests with no origin (same-origin, CLI tools, etc.)
				if origin == "" {
					return true
				}
				// Allow localhost origins for development
				// Check for exact match or with port (e.g., :3000)
				if isLocalhost(origin) {
					return true
				}
				// Reject all non-localhost origins for security
				// External sites cannot establish WebSocket connections
				return false
			},
		},
	}
	// Initialize heartbeat
	s.liveness.lastHeartbeat.Store(time.Now().Unix())
	s.registerRuntimeHandlers()
	return s
}

// Start starts the gateway server and blocks until the context is cancelled
// or an error occurs. It sets up WebSocket, REST API, and webhook endpoints.
// Returns an error if the server fails to start or is already running.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.mu.Unlock()

	mux := s.buildHandler()

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logging.WithComponent("gateway").Info("Gateway starting", slog.String("addr", addr))

	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.Shutdown()
	}
}

// RegisterHandler registers a custom HTTP handler for a path.
// Must be called before Start(). The handler will be registered when the server starts.
func (s *Server) RegisterHandler(path string, handler http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.customHandlers[path] = handler
}

// SetMetricsSource sets the metrics source for the Prometheus /metrics endpoint.
// Must be called before Start().
func (s *Server) SetMetricsSource(source MetricsSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers.prometheusExporter = NewPrometheusExporter(source)
	if s.providers.alertsSource != nil {
		s.providers.prometheusExporter.SetAlertsSource(s.providers.alertsSource)
	}
}

// SetAlertsMetricsSource wires an alert metrics source into the Prometheus exporter.
// Safe to call before or after SetMetricsSource.
func (s *Server) SetAlertsMetricsSource(source AlertMetricsSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers.alertsSource = source
	if s.providers.prometheusExporter != nil {
		s.providers.prometheusExporter.SetAlertsSource(source)
	}
}

// SetAutopilotProvider sets the autopilot provider for the /api/v1/autopilot endpoint.
// Must be called before Start().
func (s *Server) SetAutopilotProvider(p AutopilotProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers.autopilot = p
}

// SetArchitectProvider sets the architect provider for the /api/v1/architect endpoint.
// Must be called before Start().
func (s *Server) SetArchitectProvider(p ArchitectProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers.architect = p
}

// SetGitGraphPath sets the project path used by the /api/v1/gitgraph endpoint.
// Defaults to "." if not set.
func (s *Server) SetGitGraphPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dashboard.gitGraphPath = path
}

// SetGitGraphFetcher sets the function used to fetch git graph data.
// Must be called before Start() for the /api/v1/gitgraph endpoint to work.
func (s *Server) SetGitGraphFetcher(f GitGraphFetcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dashboard.gitGraphFetcher = f
}

// Shutdown gracefully shuts down the server with a 30-second timeout.
// It waits for active connections to complete before returning.
func (s *Server) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.running = false
	return s.server.Shutdown(ctx)
}

// handleWebSocket handles WebSocket connections for the control plane
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.WithComponent("gateway").Error("WebSocket upgrade error", slog.Any("error", err))
		return
	}

	session := s.authn.sessions.Create(conn)
	defer s.authn.sessions.Remove(session.ID)
	defer s.codex.sessions.close(session.ID)

	logging.WithComponent("gateway").Info("New WebSocket session", slog.String("session_id", session.ID))

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logging.WithComponent("gateway").Warn("WebSocket error", slog.Any("error", err))
			}
			break
		}

		s.router.HandleMessage(session, message)
	}
}

// Router returns the server's message router for registering handlers.
func (s *Server) Router() *Router {
	return s.router
}
