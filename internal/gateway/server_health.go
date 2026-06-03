package gateway

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// handleReady returns readiness status for Kubernetes readiness probes.
// Returns 200 when all registered readiness checks pass, 503 otherwise.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Update heartbeat to show main loop is responsive
	s.liveness.lastHeartbeat.Store(time.Now().Unix())

	checks := make(map[string]bool)
	allReady := true

	s.mu.RLock()
	for _, checker := range s.readinessCheckers {
		ready := checker.Ready()
		checks[checker.Name()] = ready
		if !ready {
			allReady = false
		}
	}
	s.mu.RUnlock()

	response := map[string]interface{}{
		"ready":  allReady,
		"checks": checks,
	}

	if !allReady {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(response)
}

// handleLive returns liveness status for Kubernetes liveness probes.
// Returns 200 when the process is alive and not deadlocked, 503 otherwise.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	checks := make(map[string]interface{})
	alive := true

	// Check 1: Goroutine count
	goroutineCount := runtime.NumGoroutine()
	goroutineOK := goroutineCount < s.liveness.maxGoroutines
	checks["goroutines"] = map[string]interface{}{
		"count": goroutineCount,
		"max":   s.liveness.maxGoroutines,
		"ok":    goroutineOK,
	}
	if !goroutineOK {
		alive = false
	}

	// Check 2: Recent panics
	now := time.Now().Unix()
	panicCount := s.liveness.panicCount.Load()
	lastPanic := s.liveness.lastPanicTime.Load()
	recentPanics := lastPanic > 0 && (now-lastPanic) < s.liveness.panicWindowSecs
	panicOK := !recentPanics
	checks["panics"] = map[string]interface{}{
		"count":          panicCount,
		"recent":         recentPanics,
		"window_seconds": s.liveness.panicWindowSecs,
		"ok":             panicOK,
	}
	if !panicOK {
		alive = false
	}

	// Check 3: Main loop responsiveness (heartbeat freshness)
	lastHeartbeat := s.liveness.lastHeartbeat.Load()
	heartbeatAge := now - lastHeartbeat
	heartbeatOK := heartbeatAge < 60 // Less than 60 seconds old
	checks["heartbeat"] = map[string]interface{}{
		"last_seconds_ago": heartbeatAge,
		"ok":               heartbeatOK,
	}
	if !heartbeatOK {
		alive = false
	}

	response := map[string]interface{}{
		"alive":  alive,
		"checks": checks,
	}

	if !alive {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(response)
}

// RegisterReadinessChecker adds a readiness checker to the server.
// Readiness checks are evaluated when /ready endpoint is called.
func (s *Server) RegisterReadinessChecker(checker ReadinessChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readinessCheckers = append(s.readinessCheckers, checker)
}

// RecordPanic records a panic event for liveness tracking.
// Call this from panic recovery handlers to track system health.
func (s *Server) RecordPanic() {
	s.liveness.panicCount.Add(1)
	s.liveness.lastPanicTime.Store(time.Now().Unix())
}

// Heartbeat updates the heartbeat timestamp to show the main loop is responsive.
// Call this periodically from long-running loops.
func (s *Server) Heartbeat() {
	s.liveness.lastHeartbeat.Store(time.Now().Unix())
}

// handleStatus returns current Pilot status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"version":  "0.1.0",
		"running":  s.running,
		"sessions": s.sessions.Count(),
	})
}

// handleTasks returns current tasks
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Return placeholder for now - tasks would come from executor/memory integration
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": []interface{}{},
	})
}

// handleAutopilot returns current autopilot state including active PRs.
func (s *Server) handleAutopilot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	provider := s.autopilotProvider
	s.mu.RUnlock()

	if provider == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled":      false,
			"environment":  "",
			"autoRelease":  false,
			"activePRs":    []interface{}{},
			"failureCount": 0,
		})
		return
	}

	prs := provider.GetActivePRs()
	activePRs := make([]map[string]interface{}, 0, len(prs))
	for _, pr := range prs {
		activePRs = append(activePRs, map[string]interface{}{
			"number":     pr.PRNumber,
			"url":        pr.PRURL,
			"stage":      pr.Stage,
			"ciStatus":   pr.CIStatus,
			"error":      pr.Error,
			"branchName": pr.BranchName,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled":      true,
		"environment":  provider.GetEnvironment(),
		"autoRelease":  provider.IsAutoReleaseEnabled(),
		"activePRs":    activePRs,
		"failureCount": provider.GetFailureCount(),
	})
}

// handleArchitectFindings returns the current Architect findings as JSON.
// Findings are read-only observations/proposals emitted by the Architect
// family (Radar, Dependency-Doctor, …). When no provider is wired the
// response is an empty list with count 0, mirroring handleAutopilot's
// nil-provider behaviour so the dashboard always receives a valid payload.
func (s *Server) handleArchitectFindings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	provider := s.architectProvider
	s.mu.RUnlock()

	var findings []pilotapi.Finding
	if provider != nil {
		findings = provider.Findings()
	}
	// Guarantee a JSON array (never null) for an absent or empty result.
	if findings == nil {
		findings = []pilotapi.Finding{}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"findings": findings,
		"count":    len(findings),
	})
}

// handleMetrics returns metrics in Prometheus text format
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	exporter := s.prometheusExporter
	s.mu.RUnlock()

	if exporter == nil {
		http.Error(w, "Metrics not configured", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if err := exporter.WritePrometheus(w); err != nil {
		http.Error(w, "Failed to write metrics", http.StatusInternalServerError)
		return
	}
}
