package gateway

import "net/http"

// buildHandler constructs the gateway's HTTP routing mux: WebSocket control
// plane, public health endpoints, auth-gated /api/v1 REST routes, adapter
// webhooks, any registered custom handlers, and the embedded dashboard.
//
// It is the single source of truth for route wiring. Start() serves the
// returned handler via ListenAndServe; tests can wrap it in httptest.NewServer
// to exercise the real end-to-end web data path (auth middleware included)
// without binding a fixed port.
func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()

	// WebSocket endpoint for control plane
	mux.HandleFunc("/ws", s.handleWebSocket)

	// WebSocket endpoint for dashboard log streaming
	mux.HandleFunc("/ws/dashboard", s.handleDashboardWebSocket)

	// Public endpoints (no auth required)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/live", s.handleLive)
	mux.HandleFunc("/metrics", s.handleMetrics)

	// Protected API endpoints (auth required when configured)
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/v1/status", s.handleStatus)
	apiMux.HandleFunc("/api/v1/tasks", s.handleTasks)
	apiMux.HandleFunc("/api/v1/autopilot", s.handleAutopilot)
	apiMux.HandleFunc("/api/v1/architect", s.handleArchitectFindings)
	apiMux.HandleFunc("/api/v1/metrics", s.handleDashboardMetrics)
	apiMux.HandleFunc("/api/v1/queue", s.handleDashboardQueue)
	apiMux.HandleFunc("/api/v1/history", s.handleDashboardHistory)
	apiMux.HandleFunc("/api/v1/logs", s.handleDashboardLogs)
	apiMux.HandleFunc("/api/v1/gitgraph", s.handleGitGraph)

	// Apply auth middleware to API routes
	if s.authn.auth != nil {
		mux.Handle("/api/v1/", s.authn.auth.Middleware(apiMux))
	} else {
		mux.Handle("/api/v1/", apiMux)
	}

	// Webhook endpoints for adapters (use signature validation, not bearer tokens)
	mux.HandleFunc("/webhooks/linear", s.handleLinearWebhook)
	mux.HandleFunc("/webhooks/github", s.handleGithubWebhook)
	mux.HandleFunc("/webhooks/gitlab", s.handleGitlabWebhook)
	mux.HandleFunc("/webhooks/bitbucket", s.handleBitbucketWebhook)
	mux.HandleFunc("/webhooks/jira", s.handleJiraWebhook)
	mux.HandleFunc("/webhooks/asana", s.handleAsanaWebhook)
	mux.HandleFunc("/webhooks/azuredevops", s.handleAzureDevOpsWebhook)
	mux.HandleFunc("/webhooks/plane", s.handlePlaneWebhook)

	// Register custom handlers
	s.mu.RLock()
	for path, handler := range s.customHandlers {
		mux.Handle(path, handler)
	}
	s.mu.RUnlock()

	// Serve embedded React dashboard at /dashboard/ if available
	s.serveDashboard(mux)

	return mux
}
