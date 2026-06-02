package autopilot

import (
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestCIAggregation_PendingBeatsFailure_B4 verifies the B4 debounce (TASK-345):
// a failure alongside a still-pending check yields CIPending (wait), not a
// premature CIFailure; a failure with all siblings terminal yields CIFailure.
func TestCIAggregation_PendingBeatsFailure_B4(t *testing.T) {
	m := &CIMonitor{}

	// aggregateStatus (map-based)
	if got := m.aggregateStatus(map[string]CIStatus{"a": CIFailure, "b": CIPending}); got != CIPending {
		t.Errorf("aggregateStatus failure+pending = %v, want CIPending", got)
	}
	if got := m.aggregateStatus(map[string]CIStatus{"a": CIFailure, "b": CISuccess}); got != CIFailure {
		t.Errorf("aggregateStatus failure+success = %v, want CIFailure", got)
	}

	// checkAllRuns (CheckRunsResponse-based)
	failPending := &github.CheckRunsResponse{TotalCount: 2, CheckRuns: []github.CheckRun{
		{Name: "a", Status: github.CheckRunCompleted, Conclusion: github.ConclusionFailure},
		{Name: "b", Status: github.CheckRunInProgress},
	}}
	if got := m.checkAllRuns(failPending); got != CIPending {
		t.Errorf("checkAllRuns failed+in_progress = %v, want CIPending", got)
	}
	failDone := &github.CheckRunsResponse{TotalCount: 2, CheckRuns: []github.CheckRun{
		{Name: "a", Status: github.CheckRunCompleted, Conclusion: github.ConclusionFailure},
		{Name: "b", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
	}}
	if got := m.checkAllRuns(failDone); got != CIFailure {
		t.Errorf("checkAllRuns failed+success = %v, want CIFailure", got)
	}
}

func TestNewCIMonitor(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	if monitor == nil {
		t.Fatal("NewCIMonitor returned nil")
	}
	if monitor.owner != "owner" {
		t.Errorf("owner = %s, want owner", monitor.owner)
	}
	if monitor.repo != "repo" {
		t.Errorf("repo = %s, want repo", monitor.repo)
	}
	if monitor.pollInterval != cfg.CIPollInterval {
		t.Errorf("pollInterval = %v, want %v", monitor.pollInterval, cfg.CIPollInterval)
	}
	if monitor.waitTimeout != cfg.CIWaitTimeout {
		t.Errorf("waitTimeout = %v, want %v", monitor.waitTimeout, cfg.CIWaitTimeout)
	}
}

func TestNewCIMonitor_DevCITimeout(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.DevCITimeout = 5 * time.Minute
	cfg.CIWaitTimeout = 30 * time.Minute

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	if monitor == nil {
		t.Fatal("NewCIMonitor returned nil")
	}
	// Dev environment should use DevCITimeout
	if monitor.waitTimeout != cfg.DevCITimeout {
		t.Errorf("waitTimeout = %v, want %v (DevCITimeout for dev env)", monitor.waitTimeout, cfg.DevCITimeout)
	}
}

func TestNewCIMonitor_StageProdUseCIWaitTimeout(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)

	// Test stage environment
	cfgStage := DefaultConfig()
	cfgStage.Environment = EnvStage
	cfgStage.DevCITimeout = 5 * time.Minute
	cfgStage.CIWaitTimeout = 30 * time.Minute

	monitorStage := NewCIMonitor(ghClient, "owner", "repo", cfgStage)
	if monitorStage.waitTimeout != cfgStage.CIWaitTimeout {
		t.Errorf("stage waitTimeout = %v, want %v (CIWaitTimeout)", monitorStage.waitTimeout, cfgStage.CIWaitTimeout)
	}

	// Test prod environment
	cfgProd := DefaultConfig()
	cfgProd.Environment = EnvProd
	cfgProd.DevCITimeout = 5 * time.Minute
	cfgProd.CIWaitTimeout = 30 * time.Minute

	monitorProd := NewCIMonitor(ghClient, "owner", "repo", cfgProd)
	if monitorProd.waitTimeout != cfgProd.CIWaitTimeout {
		t.Errorf("prod waitTimeout = %v, want %v (CIWaitTimeout)", monitorProd.waitTimeout, cfgProd.CIWaitTimeout)
	}
}

func TestCIMonitor_MapCheckStatus(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		conclusion string
		want       CIStatus
	}{
		{"queued", github.CheckRunQueued, "", CIRunning},
		{"in_progress", github.CheckRunInProgress, "", CIRunning},
		{"completed success", github.CheckRunCompleted, github.ConclusionSuccess, CISuccess},
		{"completed failure", github.CheckRunCompleted, github.ConclusionFailure, CIFailure},
		{"completed cancelled", github.CheckRunCompleted, github.ConclusionCancelled, CIFailure},
		{"completed timed_out", github.CheckRunCompleted, github.ConclusionTimedOut, CIFailure},
		{"completed skipped", github.CheckRunCompleted, github.ConclusionSkipped, CISuccess},
		{"completed neutral", github.CheckRunCompleted, github.ConclusionNeutral, CISuccess},
		{"completed unknown", github.CheckRunCompleted, "unknown", CIPending},
		{"unknown status", "unknown", "", CIPending},
	}

	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monitor.mapCheckStatus(tt.status, tt.conclusion)
			if got != tt.want {
				t.Errorf("mapCheckStatus(%s, %s) = %s, want %s", tt.status, tt.conclusion, got, tt.want)
			}
		})
	}
}

func TestCIMonitor_AggregateStatus(t *testing.T) {
	tests := []struct {
		name     string
		statuses map[string]CIStatus
		want     CIStatus
	}{
		{
			name:     "all success",
			statuses: map[string]CIStatus{"build": CISuccess, "test": CISuccess},
			want:     CISuccess,
		},
		{
			name:     "one failure",
			statuses: map[string]CIStatus{"build": CISuccess, "test": CIFailure},
			want:     CIFailure,
		},
		{
			name:     "one pending",
			statuses: map[string]CIStatus{"build": CISuccess, "test": CIPending},
			want:     CIPending,
		},
		{
			name:     "one running",
			statuses: map[string]CIStatus{"build": CISuccess, "test": CIRunning},
			want:     CIPending,
		},
		{
			// B4 (TASK-345): a failure while a sibling is still pending must NOT
			// declare CIFailure — wait for the suite to finish (premature-close fix).
			name:     "pending defers failure (B4 debounce)",
			statuses: map[string]CIStatus{"build": CIFailure, "test": CIPending},
			want:     CIPending,
		},
		{
			name:     "failure with all siblings terminal",
			statuses: map[string]CIStatus{"build": CIFailure, "test": CISuccess},
			want:     CIFailure,
		},
		{
			name:     "empty statuses",
			statuses: map[string]CIStatus{},
			want:     CISuccess,
		},
	}

	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monitor.aggregateStatus(tt.statuses)
			if got != tt.want {
				t.Errorf("aggregateStatus() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCIMonitor_CheckAllRuns(t *testing.T) {
	tests := []struct {
		name      string
		checkRuns *github.CheckRunsResponse
		want      CIStatus
	}{
		{
			name: "all success",
			checkRuns: &github.CheckRunsResponse{
				TotalCount: 2,
				CheckRuns: []github.CheckRun{
					{Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
					{Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				},
			},
			want: CISuccess,
		},
		{
			name: "one failure",
			checkRuns: &github.CheckRunsResponse{
				TotalCount: 2,
				CheckRuns: []github.CheckRun{
					{Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
					{Status: github.CheckRunCompleted, Conclusion: github.ConclusionFailure},
				},
			},
			want: CIFailure,
		},
		{
			name: "one pending",
			checkRuns: &github.CheckRunsResponse{
				TotalCount: 2,
				CheckRuns: []github.CheckRun{
					{Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
					{Status: github.CheckRunInProgress, Conclusion: ""},
				},
			},
			want: CIPending,
		},
		{
			name: "no checks",
			checkRuns: &github.CheckRunsResponse{
				TotalCount: 0,
				CheckRuns:  []github.CheckRun{},
			},
			want: CIPending,
		},
	}

	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monitor.checkAllRuns(tt.checkRuns)
			if got != tt.want {
				t.Errorf("checkAllRuns() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCIMonitor_LegacyRequiredChecksUsesManualMode(t *testing.T) {
	// Test that legacy RequiredChecks config is converted to manual mode
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.RequiredChecks = []string{"build", "test"}
	cfg.CIChecks = nil // No new-style config

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	if monitor.ciChecks == nil {
		t.Fatal("ciChecks should be set")
	}
	if monitor.ciChecks.Mode != "manual" {
		t.Errorf("ciChecks.Mode = %s, want manual", monitor.ciChecks.Mode)
	}
	if len(monitor.requiredChecks) != 2 {
		t.Errorf("requiredChecks = %v, want [build, test]", monitor.requiredChecks)
	}
}

func TestCIMonitor_NewConfigUsesManualMode(t *testing.T) {
	// Test that CIChecks with manual mode uses Required list
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.RequiredChecks = nil // Ignored when CIChecks is set
	cfg.CIChecks = &CIChecksConfig{
		Mode:     "manual",
		Required: []string{"ci", "deploy"},
	}

	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	if monitor.ciChecks.Mode != "manual" {
		t.Errorf("ciChecks.Mode = %s, want manual", monitor.ciChecks.Mode)
	}
	if len(monitor.requiredChecks) != 2 {
		t.Errorf("requiredChecks = %v, want [ci, deploy]", monitor.requiredChecks)
	}
}

func TestCIMonitor_MapCombinedStatus(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

	tests := []struct {
		name       string
		combined   *github.CombinedStatus
		wantStatus CIStatus
	}{
		{"failure state", &github.CombinedStatus{State: github.StatusFailure, TotalCount: 2}, CIFailure},
		{"error state", &github.CombinedStatus{State: github.StatusError, TotalCount: 1}, CIFailure},
		{"pending state", &github.CombinedStatus{State: github.StatusPending, TotalCount: 1}, CIPending},
		{"success state", &github.CombinedStatus{State: github.StatusSuccess, TotalCount: 2}, CISuccess},
		{"zero total count (no statuses)", &github.CombinedStatus{State: github.StatusPending, TotalCount: 0}, CISuccess},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monitor.mapCombinedStatus(tt.combined)
			if got != tt.wantStatus {
				t.Errorf("mapCombinedStatus(%+v) = %s, want %s", tt.combined, got, tt.wantStatus)
			}
		})
	}
}
