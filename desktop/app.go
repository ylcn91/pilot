package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/dashboard"
	"github.com/ylcn91/pilot/internal/memory"
)

// App is the Wails application struct. Its exported methods are bound to the
// frontend and callable from JavaScript/TypeScript via the generated bindings.
type App struct {
	ctx                 context.Context
	store               *memory.Store
	httpClient          *http.Client
	gatewayURL          string // e.g. "http://127.0.0.1:9090"
	mu                  sync.Mutex
	gatewayCmd          *exec.Cmd
	gatewayDone         chan error
	gatewayStarting     bool
	gatewayStartedByApp bool
	gatewayConfigDir    string
	startGatewayProcess func(configPath, projectPath string) (*exec.Cmd, error)
	getwd               func() (string, error)
	sleep               func(time.Duration)
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{
		httpClient:          &http.Client{Timeout: 2 * time.Second},
		startGatewayProcess: startGatewayProcess,
		getwd:               os.Getwd,
		sleep:               time.Sleep,
	}
}

// startup is called when the app starts. Opens the SQLite database.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dataPath := filepath.Join(homeDir, ".pilot", "data")
	store, err := memory.NewStore(dataPath)
	if err != nil {
		// Dashboard degrades gracefully when SQLite is unavailable
		return
	}
	a.store = store

	// Load config to determine gateway address
	cfg, err := config.Load(config.DefaultConfigPath())
	if err == nil && cfg.Gateway != nil {
		a.gatewayURL = fmt.Sprintf("http://%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
	} else {
		a.gatewayURL = "http://127.0.0.1:9090"
	}
}

// shutdown is called when the app is about to quit.
func (a *App) shutdown(_ context.Context) {
	a.stopManagedGateway()
	if a.store != nil {
		_ = a.store.Close()
	}
}

func startGatewayProcess(configPath, projectPath string) (*exec.Cmd, error) {
	args := []string{"start", "--config", configPath}
	if projectPath != "" {
		args = append(args, "--project", projectPath)
	}
	cmd := exec.Command("pilot", args...)
	cmd.Env = managedGatewayEnv(os.Environ(), filepath.Dir(configPath))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (a *App) EnsureGatewayRunning() ServerStatus {
	status := a.GetServerStatus()
	if status.Running {
		return status
	}

	a.mu.Lock()
	if a.gatewayStarting {
		a.mu.Unlock()
		return a.waitForGateway(10 * time.Second)
	}
	if a.gatewayCmd != nil && a.gatewayStartedByApp {
		a.mu.Unlock()
		return a.waitForGateway(10 * time.Second)
	}
	a.gatewayStarting = true
	a.mu.Unlock()

	projectPath, err := a.getwd()
	if err != nil {
		projectPath = "."
	}
	configPath, runtimeProjectPath, configDir, err := a.prepareManagedGatewayConfig(projectPath)
	if err != nil {
		a.mu.Lock()
		a.gatewayStarting = false
		a.mu.Unlock()
		return ServerStatus{GatewayURL: a.gatewayURL, Error: err.Error()}
	}
	cmd, err := a.startGatewayProcess(configPath, runtimeProjectPath)

	a.mu.Lock()
	a.gatewayStarting = false
	if err != nil {
		a.mu.Unlock()
		_ = os.RemoveAll(configDir)
		return ServerStatus{GatewayURL: a.gatewayURL, Error: err.Error()}
	}
	done := make(chan error, 1)
	a.gatewayCmd = cmd
	a.gatewayDone = done
	a.gatewayStartedByApp = true
	a.gatewayConfigDir = configDir
	a.mu.Unlock()

	go a.waitManagedGateway(cmd, done)

	status = a.waitForGateway(10 * time.Second)
	if !status.Running && status.Error == "" {
		status.Error = "gateway did not become healthy"
	}
	return status
}

func (a *App) waitForGateway(timeout time.Duration) ServerStatus {
	deadline := time.Now().Add(timeout)
	for {
		status := a.GetServerStatus()
		if status.Running {
			a.mu.Lock()
			status.StartedByApp = a.gatewayStartedByApp
			a.mu.Unlock()
			return status
		}
		if time.Now().After(deadline) {
			status.Error = "gateway is not reachable"
			return status
		}
		a.sleep(250 * time.Millisecond)
	}
}

func (a *App) waitManagedGateway(cmd *exec.Cmd, done chan error) {
	err := cmd.Wait()
	done <- err
	close(done)

	a.mu.Lock()
	if a.gatewayCmd == cmd {
		a.gatewayCmd = nil
		a.gatewayDone = nil
		a.gatewayStartedByApp = false
		configDir := a.gatewayConfigDir
		a.gatewayConfigDir = ""
		if configDir != "" {
			_ = os.RemoveAll(configDir)
		}
	}
	a.mu.Unlock()
}

func (a *App) stopManagedGateway() {
	a.mu.Lock()
	cmd := a.gatewayCmd
	done := a.gatewayDone
	startedByApp := a.gatewayStartedByApp
	configDir := a.gatewayConfigDir
	a.gatewayCmd = nil
	a.gatewayDone = nil
	a.gatewayStartedByApp = false
	a.gatewayConfigDir = ""
	a.mu.Unlock()

	if !startedByApp || cmd == nil || cmd.Process == nil {
		return
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
	}

	if done == nil {
		return
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	if configDir != "" {
		_ = os.RemoveAll(configDir)
	}
}

func (a *App) prepareManagedGatewayConfig(_ string) (configPath, projectPath, configDir string, err error) {
	host, port := parseGatewayAddress(a.gatewayURL)
	configDir, err = os.MkdirTemp("", "pilot-desktop-runtime.")
	if err != nil {
		return "", "", "", err
	}
	projectPath = filepath.Join(configDir, "project")
	memoryPath := filepath.Join(configDir, "memory")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		_ = os.RemoveAll(configDir)
		return "", "", "", err
	}
	if err := os.MkdirAll(memoryPath, 0o755); err != nil {
		_ = os.RemoveAll(configDir)
		return "", "", "", err
	}
	if err := writeGHBlockShim(configDir); err != nil {
		_ = os.RemoveAll(configDir)
		return "", "", "", err
	}

	configPath = filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(managedGatewayConfigYAML(host, port, projectPath, memoryPath)), 0o600); err != nil {
		_ = os.RemoveAll(configDir)
		return "", "", "", err
	}
	return configPath, projectPath, configDir, nil
}

func parseGatewayAddress(gatewayURL string) (string, int) {
	if gatewayURL == "" {
		return "127.0.0.1", 9090
	}
	parsed, err := url.Parse(gatewayURL)
	if err != nil {
		return "127.0.0.1", 9090
	}
	host := parsed.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port := 9090
	if parsed.Port() != "" {
		if parsedPort, err := strconv.Atoi(parsed.Port()); err == nil && parsedPort > 0 {
			port = parsedPort
		}
	}
	return host, port
}

func managedGatewayConfigYAML(host string, port int, projectPath, memoryPath string) string {
	return fmt.Sprintf(`version: "1.0"
gateway:
  host: %q
  port: %d
auth:
  type: "claude-code"
adapters:
  github:
    enabled: false
    token: ""
    repo: ""
    pilot_label: "pilot-disabled"
    polling:
      enabled: false
      interval: 1h
      label: "pilot-disabled"
    stale_label_cleanup:
      enabled: false
    project_board:
      enabled: false
      source_enabled: false
  linear:
    enabled: false
    polling:
      enabled: false
  slack:
    enabled: false
    socket_mode: false
  telegram:
    enabled: false
    polling: false
  gitlab:
    enabled: false
    polling:
      enabled: false
  azure_devops:
    enabled: false
    polling:
      enabled: false
  jira:
    enabled: false
    polling:
      enabled: false
  asana:
    enabled: false
    polling:
      enabled: false
  plane:
    enabled: false
    polling:
      enabled: false
  discord:
    enabled: false
orchestrator:
  model: "codex-runtime"
  max_concurrent: 1
  daily_brief:
    enabled: false
  execution:
    mode: "sequential"
    wait_for_merge: false
    poll_interval: 1h
    pr_timeout: 1m
  autopilot:
    enabled: false
    auto_review: false
    auto_merge: false
    auto_create_issues: false
    notify_on_failure: false
    review_feedback:
      enabled: false
memory:
  path: %q
  cross_project: false
  learning:
    enabled: false
    min_confidence: 1
    max_patterns: 0
    include_anti: false
projects:
  - name: "desktop-runtime"
    path: %q
    navigator: false
    default_branch: "main"
default_project: "desktop-runtime"
alerts:
  enabled: false
`, host, port, memoryPath, projectPath)
}

func writeGHBlockShim(configDir string) error {
	binDir := filepath.Join(configDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	shim := "#!/usr/bin/env sh\n" +
		"echo \"blocked gh invocation from managed desktop runtime: gh $*\" >&2\n" +
		"exit 88\n"
	return os.WriteFile(filepath.Join(binDir, "gh"), []byte(shim), 0o755)
}

func managedGatewayEnv(base []string, configDir string) []string {
	blocked := map[string]struct{}{
		"GITHUB_TOKEN":           {},
		"GH_TOKEN":               {},
		"GITHUB_APP_ID":          {},
		"GITHUB_APP_PRIVATE_KEY": {},
		"GITHUB_WEBHOOK_SECRET":  {},
		"GITLAB_TOKEN":           {},
		"AZURE_DEVOPS_PAT":       {},
		"JIRA_API_TOKEN":         {},
		"ASANA_ACCESS_TOKEN":     {},
		"PLANE_API_KEY":          {},
		"DISCORD_BOT_TOKEN":      {},
		"LINEAR_API_KEY":         {},
		"SLACK_BOT_TOKEN":        {},
		"SLACK_APP_TOKEN":        {},
		"TELEGRAM_BOT_TOKEN":     {},
		"GH_CONFIG_DIR":          {},
	}
	env := make([]string, 0, len(base)+2)
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			if _, remove := blocked[key]; remove {
				continue
			}
		}
		env = append(env, item)
	}
	env = append(env, "GH_CONFIG_DIR="+filepath.Join(configDir, "gh-config"))
	env = append(env, "PATH="+filepath.Join(configDir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	return env
}

// GetMetrics returns aggregated lifetime metrics and 7-day sparkline data.
func (a *App) GetMetrics() DashboardMetrics {
	if a.store == nil {
		return DashboardMetrics{}
	}

	lt, err := a.store.GetLifetimeTokens()
	if err != nil {
		lt = &memory.LifetimeTokens{}
	}

	tc, err := a.store.GetLifetimeTaskCounts()
	if err != nil {
		tc = &memory.LifetimeTaskCounts{}
	}

	// Build 7-day sparklines
	now := time.Now().UTC()
	weekAgo := now.AddDate(0, 0, -7)
	query := memory.MetricsQuery{Start: weekAgo, End: now.AddDate(0, 0, 1)}

	dailyMetrics, _ := a.store.GetDailyMetrics(query)

	// Index by date string for O(1) lookup
	byDate := make(map[string]*memory.DailyMetrics, len(dailyMetrics))
	for _, dm := range dailyMetrics {
		byDate[dm.Date.Format("2006-01-02")] = dm
	}

	tokenSparkline := make([]int64, 7)
	costSparkline := make([]float64, 7)
	queueSparkline := make([]int, 7)

	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		idx := 6 - i
		if dm, ok := byDate[day]; ok {
			tokenSparkline[idx] = dm.TotalTokens
			costSparkline[idx] = dm.TotalCostUSD
			queueSparkline[idx] = dm.ExecutionCount
		}
	}

	return DashboardMetrics{
		TotalTokens:    lt.TotalTokens,
		InputTokens:    lt.InputTokens,
		OutputTokens:   lt.OutputTokens,
		TotalCostUSD:   lt.TotalCostUSD,
		TotalTasks:     tc.Total,
		SucceededTasks: tc.Succeeded,
		FailedTasks:    tc.Failed,
		TokenSparkline: tokenSparkline,
		CostSparkline:  costSparkline,
		QueueSparkline: queueSparkline,
	}
}

// GetQueueTasks returns the current task queue (active + queued + pending + recent completed).
func (a *App) GetQueueTasks() []QueueTask {
	if a.store == nil {
		return nil
	}

	execs, err := a.store.GetRecentExecutions(50)
	if err != nil {
		return nil
	}

	// Deduplicate by TaskID: keep the best execution per issue.
	// Priority: running > completed > failed; then most recent.
	best := make(map[string]QueueTask)
	for _, exec := range execs {
		qt := QueueTask{
			ID:          exec.ID,
			IssueID:     issueIDFromTaskID(exec.TaskID),
			Title:       exec.TaskTitle,
			Status:      normalizeStatus(exec.Status),
			PRURL:       exec.PRUrl,
			IssueURL:    issueURL(exec.TaskID),
			ProjectPath: exec.ProjectPath,
			CreatedAt:   exec.CreatedAt,
		}

		switch exec.Status {
		case "running":
			qt.Progress = 0.5
		case "completed":
			qt.Progress = 1.0
		case "failed":
			qt.Progress = 0.0
		}

		key := exec.TaskID
		existing, exists := best[key]
		if !exists || queueTaskBetter(qt, existing) {
			best[key] = qt
		}
	}

	tasks := make([]QueueTask, 0, len(best))
	for _, qt := range best {
		tasks = append(tasks, qt)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	return tasks
}

// GetHistory returns the last N completed executions, grouped by epic if applicable.
func (a *App) GetHistory(limit int) []HistoryEntry {
	if a.store == nil {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}

	execs, err := a.store.GetRecentExecutions(limit)
	if err != nil {
		return nil
	}

	// Deduplicate by TaskID: keep the best execution per issue.
	// Priority: completed > failed; prefer entry with PR URL; then most recent.
	best := make(map[string]HistoryEntry)
	for _, exec := range execs {
		if exec.Status != "completed" && exec.Status != "failed" {
			continue
		}
		he := HistoryEntry{
			ID:          exec.ID,
			IssueID:     issueIDFromTaskID(exec.TaskID),
			Title:       exec.TaskTitle,
			Status:      exec.Status,
			PRURL:       exec.PRUrl,
			ProjectPath: exec.ProjectPath,
			DurationMs:  exec.DurationMs,
		}
		if exec.CompletedAt != nil {
			he.CompletedAt = *exec.CompletedAt
		}

		key := exec.TaskID
		existing, exists := best[key]
		if !exists || historyEntryBetter(he, existing) {
			best[key] = he
		}
	}

	entries := make([]HistoryEntry, 0, len(best))
	for _, he := range best {
		entries = append(entries, he)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CompletedAt.After(entries[j].CompletedAt)
	})
	return entries
}

// GetAutopilotStatus returns autopilot state by querying the running daemon's gateway API.
// Falls back to SQLite metrics when the daemon is not reachable.
func (a *App) GetAutopilotStatus() AutopilotStatus {
	// Try live daemon API first (GH-1585)
	if status, ok := a.fetchAutopilotFromDaemon(); ok {
		return status
	}

	// Fallback: read from SQLite metrics snapshot
	if a.store == nil {
		return AutopilotStatus{}
	}
	rows, err := a.store.GetRecentAutopilotMetrics(1)
	if err != nil || len(rows) == 0 {
		return AutopilotStatus{}
	}
	r := rows[0]
	return AutopilotStatus{
		Enabled:      r.ActivePRs > 0 || r.PRsMerged > 0,
		FailureCount: r.PRsFailed,
		ActivePRs:    []ActivePR{},
	}
}

// GetLogs returns recent execution log entries for the logs panel.
func (a *App) GetLogs(limit int) []LogEntry {
	if a.store == nil {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}

	entries, err := a.store.GetRecentLogs(limit)
	if err != nil {
		return nil
	}

	result := make([]LogEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, LogEntry{
			Ts:        e.Timestamp.Format("15:04:05"),
			Level:     e.Level,
			Message:   e.Message,
			Component: e.Component,
		})
	}

	// Reverse so oldest is first (panel auto-scrolls to bottom)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// fetchAutopilotFromDaemon queries the running daemon's /api/v1/autopilot endpoint.
// Returns the parsed status and true if the daemon is reachable, or zero value and false otherwise.
func (a *App) fetchAutopilotFromDaemon() (AutopilotStatus, bool) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(a.gatewayURL + "/api/v1/autopilot")
	if err != nil {
		return AutopilotStatus{}, false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return AutopilotStatus{}, false
	}

	var data struct {
		Enabled      bool   `json:"enabled"`
		Environment  string `json:"environment"`
		AutoRelease  bool   `json:"autoRelease"`
		FailureCount int    `json:"failureCount"`
		ActivePRs    []struct {
			Number     int    `json:"number"`
			URL        string `json:"url"`
			Stage      string `json:"stage"`
			CIStatus   string `json:"ciStatus"`
			Error      string `json:"error"`
			BranchName string `json:"branchName"`
		} `json:"activePRs"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return AutopilotStatus{}, false
	}

	prs := make([]ActivePR, 0, len(data.ActivePRs))
	for _, pr := range data.ActivePRs {
		prs = append(prs, ActivePR{
			Number:     pr.Number,
			URL:        pr.URL,
			Stage:      pr.Stage,
			CIStatus:   pr.CIStatus,
			Error:      pr.Error,
			BranchName: pr.BranchName,
		})
	}

	return AutopilotStatus{
		Enabled:      data.Enabled,
		Environment:  data.Environment,
		AutoRelease:  data.AutoRelease,
		ActivePRs:    prs,
		FailureCount: data.FailureCount,
	}, true
}

// GetServerStatus checks whether the pilot daemon gateway is reachable.
// It hits the unauthenticated /health endpoint and, on success, fetches
// version info from /api/v1/status.
func (a *App) GetServerStatus() ServerStatus {
	if a.gatewayURL == "" {
		return ServerStatus{Running: false}
	}

	// Health check (unauthenticated)
	resp, err := a.httpClient.Get(a.gatewayURL + "/health")
	if err != nil {
		return ServerStatus{Running: false}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ServerStatus{Running: false}
	}

	status := ServerStatus{
		Running:    true,
		GatewayURL: a.gatewayURL,
	}
	a.mu.Lock()
	status.StartedByApp = a.gatewayStartedByApp
	a.mu.Unlock()

	// Try to get version from /api/v1/status (best-effort, may require auth)
	if vResp, err := a.httpClient.Get(a.gatewayURL + "/api/v1/status"); err == nil {
		defer func() { _ = vResp.Body.Close() }()
		if vResp.StatusCode == http.StatusOK {
			var body struct {
				Version string `json:"version"`
			}
			if json.NewDecoder(vResp.Body).Decode(&body) == nil && body.Version != "" {
				status.Version = body.Version
			}
		}
	}

	return status
}

// OpenInBrowser opens the given URL in the system default browser.
func (a *App) OpenInBrowser(url string) {
	if a.ctx == nil || url == "" {
		return
	}
	wailsruntime.BrowserOpenURL(a.ctx, url)
}

// issueIDFromTaskID extracts the short issue ID from a task ID string.
// Task IDs typically look like "GH-123" or "LINEAR-456".
func issueIDFromTaskID(taskID string) string {
	parts := strings.SplitN(taskID, "/", 2)
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return taskID
}

// issueURL constructs the GitHub issue URL from a task ID.
func issueURL(taskID string) string {
	id := issueIDFromTaskID(taskID)
	if strings.HasPrefix(id, "GH-") {
		num := strings.TrimPrefix(id, "GH-")
		return fmt.Sprintf("https://github.com/ylcn91/pilot/issues/%s", num)
	}
	return ""
}

// GetGitGraph returns the git commit graph for the configured project.
// limit controls how many commits to fetch; defaults to 100 when 0 is passed.
func (a *App) GetGitGraph(limit int) GitGraphData {
	if limit <= 0 {
		limit = 100
	}

	// Resolve project path from config (same pattern as startup()).
	projectPath := "."
	cfg, err := config.Load(config.DefaultConfigPath())
	if err == nil && len(cfg.Projects) > 0 {
		projectPath = cfg.Projects[0].Path
	}

	state := dashboard.FetchGitGraph(projectPath, limit)
	if state == nil {
		return GitGraphData{}
	}

	lines := make([]GitGraphLine, 0, len(state.Lines))
	for _, l := range state.Lines {
		lines = append(lines, GitGraphLine{
			GraphChars: l.GraphChars,
			Refs:       l.Refs,
			Message:    l.Message,
			Author:     l.Author,
			SHA:        l.SHA,
		})
	}

	return GitGraphData{
		Lines:       lines,
		TotalCount:  state.TotalCount,
		Error:       state.Error,
		LastRefresh: state.LastRefresh,
	}
}

// queueStatusPriority returns a numeric priority for queue task statuses.
// Higher value = better status to keep when deduplicating.
func queueStatusPriority(status string) int {
	switch status {
	case "running":
		return 3
	case "done":
		return 2
	case "queued", "pending":
		return 1
	default: // failed
		return 0
	}
}

// queueTaskBetter returns true if candidate should replace existing in dedup.
func queueTaskBetter(candidate, existing QueueTask) bool {
	cp, ep := queueStatusPriority(candidate.Status), queueStatusPriority(existing.Status)
	if cp != ep {
		return cp > ep
	}
	return candidate.CreatedAt.After(existing.CreatedAt)
}

// historyEntryBetter returns true if candidate should replace existing in dedup.
func historyEntryBetter(candidate, existing HistoryEntry) bool {
	// completed beats failed
	if candidate.Status != existing.Status {
		if candidate.Status == "completed" {
			return true
		}
		if existing.Status == "completed" {
			return false
		}
	}
	// prefer entry with PR URL
	if candidate.PRURL != "" && existing.PRURL == "" {
		return true
	}
	if candidate.PRURL == "" && existing.PRURL != "" {
		return false
	}
	// most recent wins
	return candidate.CompletedAt.After(existing.CompletedAt)
}

// normalizeStatus maps internal execution statuses to frontend-friendly names.
func normalizeStatus(status string) string {
	switch status {
	case "completed":
		return "done"
	case "running":
		return "running"
	case "queued":
		return "queued"
	case "pending":
		return "pending"
	case "failed":
		return "failed"
	default:
		return status
	}
}
