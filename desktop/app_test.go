package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qf-studio/pilot/internal/dashboard"
)

var errTestGatewayStart = errors.New("gateway start failed")

// TestGetGitGraph_DefaultLimit verifies that passing limit=0 falls back to 100
// and that the returned GitGraphData mirrors dashboard.GitGraphState fields.
func TestGetGitGraph_DefaultLimit(t *testing.T) {
	// Use "." as project path (current dir is inside a git repo during tests).
	state := dashboard.FetchGitGraph(".", 100)
	if state == nil {
		t.Skip("git not available in test environment")
	}

	app := &App{}
	// limit=0 should default to 100 — same result as explicit 100.
	got := app.GetGitGraph(0)
	if got.TotalCount != state.TotalCount {
		t.Errorf("TotalCount mismatch: got %d, want %d", got.TotalCount, state.TotalCount)
	}
	if len(got.Lines) != len(state.Lines) {
		t.Errorf("Lines length mismatch: got %d, want %d", len(got.Lines), len(state.Lines))
	}
}

// TestGetGitGraph_LinesMapping verifies each GitGraphLine field is copied correctly.
func TestGetGitGraph_LinesMapping(t *testing.T) {
	state := dashboard.FetchGitGraph(".", 5)
	if state == nil || len(state.Lines) == 0 {
		t.Skip("no git commits available in test environment")
	}

	app := &App{}
	got := app.GetGitGraph(5)

	for i, want := range state.Lines {
		if i >= len(got.Lines) {
			t.Fatalf("missing line at index %d", i)
		}
		gl := got.Lines[i]
		if gl.GraphChars != want.GraphChars {
			t.Errorf("line[%d].GraphChars = %q, want %q", i, gl.GraphChars, want.GraphChars)
		}
		if gl.SHA != want.SHA {
			t.Errorf("line[%d].SHA = %q, want %q", i, gl.SHA, want.SHA)
		}
		if gl.Message != want.Message {
			t.Errorf("line[%d].Message = %q, want %q", i, gl.Message, want.Message)
		}
	}
}

func TestGetServerStatus_DaemonRunning(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"version": "1.40.1",
			"running": true,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	app := &App{
		httpClient: &http.Client{Timeout: 2 * time.Second},
		gatewayURL: srv.URL,
	}

	status := app.GetServerStatus()
	if !status.Running {
		t.Fatal("expected Running=true when daemon is healthy")
	}
	if status.Version != "1.40.1" {
		t.Fatalf("expected version 1.40.1, got %q", status.Version)
	}
	if status.GatewayURL != srv.URL {
		t.Fatalf("expected GatewayURL=%q, got %q", srv.URL, status.GatewayURL)
	}
}

func TestGetServerStatus_DaemonNotRunning(t *testing.T) {
	app := &App{
		httpClient: &http.Client{Timeout: 1 * time.Second},
		gatewayURL: "http://127.0.0.1:1", // nothing listening
	}

	status := app.GetServerStatus()
	if status.Running {
		t.Fatal("expected Running=false when daemon is unreachable")
	}
}

func TestGetServerStatus_EmptyGatewayURL(t *testing.T) {
	app := &App{
		httpClient: &http.Client{Timeout: 1 * time.Second},
		gatewayURL: "",
	}

	status := app.GetServerStatus()
	if status.Running {
		t.Fatal("expected Running=false when gatewayURL is empty")
	}
}

func TestEnsureGatewayRunning_DaemonAlreadyRunning(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	spawned := false
	app := &App{
		httpClient: &http.Client{Timeout: 2 * time.Second},
		gatewayURL: srv.URL,
		startGatewayProcess: func(string, string) (*exec.Cmd, error) {
			spawned = true
			return nil, nil
		},
		getwd: func() (string, error) { return ".", nil },
		sleep: func(time.Duration) {},
	}

	status := app.EnsureGatewayRunning()
	if !status.Running {
		t.Fatal("expected running gateway")
	}
	if spawned {
		t.Fatal("expected no process spawn when gateway is already running")
	}
}

func TestEnsureGatewayRunning_StartsDaemon(t *testing.T) {
	var healthy atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	spawns := 0
	app := &App{
		httpClient: &http.Client{Timeout: 2 * time.Second},
		gatewayURL: srv.URL,
		startGatewayProcess: func(configPath, projectPath string) (*exec.Cmd, error) {
			spawns++
			configBytes, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("managed gateway config missing: %v", err)
			}
			configText := string(configBytes)
			if strings.Contains(configText, "qf-studio/pilot") {
				t.Fatal("managed gateway config must not reference upstream")
			}
			for _, want := range []string{
				"enabled: false",
				"auto_create_issues: false",
				"source_enabled: false",
			} {
				if !strings.Contains(configText, want) {
					t.Fatalf("managed gateway config missing %q", want)
				}
			}
			if projectPath == "." {
				t.Fatal("managed gateway must not start against the caller cwd")
			}
			healthy.Store(true)
			return &exec.Cmd{}, nil
		},
		getwd: func() (string, error) { return ".", nil },
		sleep: func(time.Duration) {},
	}

	status := app.EnsureGatewayRunning()
	if !status.Running {
		t.Fatalf("expected running gateway, got error %q", status.Error)
	}
	if spawns != 1 {
		t.Fatalf("spawns = %d, want 1", spawns)
	}
}

func TestPrepareManagedGatewayConfig_DisablesExternalWorkSources(t *testing.T) {
	app := &App{gatewayURL: "http://127.0.0.1:19091"}

	configPath, projectPath, configDir, err := app.prepareManagedGatewayConfig(".")
	if err != nil {
		t.Fatalf("prepareManagedGatewayConfig error: %v", err)
	}
	defer os.RemoveAll(configDir)

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	configText := string(configBytes)
	if strings.Contains(configText, "qf-studio/pilot") {
		t.Fatal("runtime config must not reference upstream")
	}
	for _, want := range []string{
		`host: "127.0.0.1"`,
		"port: 19091",
		"auto_create_issues: false",
		"source_enabled: false",
		"cross_project: false",
	} {
		if !strings.Contains(configText, want) {
			t.Fatalf("runtime config missing %q", want)
		}
	}
	if !strings.HasPrefix(projectPath, configDir) {
		t.Fatalf("projectPath = %q, want under %q", projectPath, configDir)
	}
	if _, err := os.Stat(configDir + "/bin/gh"); err != nil {
		t.Fatalf("gh block shim missing: %v", err)
	}
}

func TestManagedGatewayEnv_RemovesIssueTrackerCredentials(t *testing.T) {
	configDir := t.TempDir()
	env := managedGatewayEnv([]string{
		"PATH=/usr/bin",
		"GITHUB_TOKEN=secret",
		"GH_TOKEN=secret",
		"LINEAR_API_KEY=secret",
		"OPENAI_API_KEY=keep",
	}, configDir)

	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, blocked := range []string{"GITHUB_TOKEN=", "GH_TOKEN=", "LINEAR_API_KEY="} {
		if strings.Contains(joined, "\n"+blocked) {
			t.Fatalf("managed env leaked %s", blocked)
		}
	}
	if !strings.Contains(joined, "\nOPENAI_API_KEY=keep\n") {
		t.Fatal("managed env should preserve model/runtime credentials")
	}
	if !strings.Contains(joined, "\nGH_CONFIG_DIR="+configDir+"/gh-config\n") {
		t.Fatal("managed env should isolate gh config")
	}
	if !strings.Contains(joined, "\nPATH="+configDir+"/bin:") {
		t.Fatal("managed env should prepend gh block shim directory")
	}
}

func TestEnsureGatewayRunning_StartFailure(t *testing.T) {
	app := &App{
		httpClient: &http.Client{Timeout: 1 * time.Second},
		gatewayURL: "http://127.0.0.1:1",
		startGatewayProcess: func(string, string) (*exec.Cmd, error) {
			return nil, errTestGatewayStart
		},
		getwd: func() (string, error) { return ".", nil },
		sleep: func(time.Duration) {},
	}

	status := app.EnsureGatewayRunning()
	if status.Running {
		t.Fatal("expected offline status")
	}
	if status.Error == "" {
		t.Fatal("expected start error")
	}
}

func TestQueueTaskBetter(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)

	tests := []struct {
		name      string
		candidate QueueTask
		existing  QueueTask
		want      bool
	}{
		{
			name:      "running beats done",
			candidate: QueueTask{Status: "running", CreatedAt: earlier},
			existing:  QueueTask{Status: "done", CreatedAt: now},
			want:      true,
		},
		{
			name:      "done beats failed",
			candidate: QueueTask{Status: "done", CreatedAt: earlier},
			existing:  QueueTask{Status: "failed", CreatedAt: now},
			want:      true,
		},
		{
			name:      "failed does not beat done",
			candidate: QueueTask{Status: "failed", CreatedAt: now},
			existing:  QueueTask{Status: "done", CreatedAt: earlier},
			want:      false,
		},
		{
			name:      "same status newer wins",
			candidate: QueueTask{Status: "done", CreatedAt: now},
			existing:  QueueTask{Status: "done", CreatedAt: earlier},
			want:      true,
		},
		{
			name:      "same status older loses",
			candidate: QueueTask{Status: "done", CreatedAt: earlier},
			existing:  QueueTask{Status: "done", CreatedAt: now},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := queueTaskBetter(tt.candidate, tt.existing)
			if got != tt.want {
				t.Errorf("queueTaskBetter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHistoryEntryBetter(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)

	tests := []struct {
		name      string
		candidate HistoryEntry
		existing  HistoryEntry
		want      bool
	}{
		{
			name:      "completed beats failed",
			candidate: HistoryEntry{Status: "completed", CompletedAt: earlier},
			existing:  HistoryEntry{Status: "failed", CompletedAt: now},
			want:      true,
		},
		{
			name:      "failed does not beat completed",
			candidate: HistoryEntry{Status: "failed", CompletedAt: now},
			existing:  HistoryEntry{Status: "completed", CompletedAt: earlier},
			want:      false,
		},
		{
			name:      "with PR URL beats without",
			candidate: HistoryEntry{Status: "completed", PRURL: "https://pr/1", CompletedAt: earlier},
			existing:  HistoryEntry{Status: "completed", CompletedAt: now},
			want:      true,
		},
		{
			name:      "without PR URL does not beat with",
			candidate: HistoryEntry{Status: "completed", CompletedAt: now},
			existing:  HistoryEntry{Status: "completed", PRURL: "https://pr/1", CompletedAt: earlier},
			want:      false,
		},
		{
			name:      "same status same PR newer wins",
			candidate: HistoryEntry{Status: "failed", CompletedAt: now},
			existing:  HistoryEntry{Status: "failed", CompletedAt: earlier},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := historyEntryBetter(tt.candidate, tt.existing)
			if got != tt.want {
				t.Errorf("historyEntryBetter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetServerStatus_HealthOK_StatusUnauthorized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	app := &App{
		httpClient: &http.Client{Timeout: 2 * time.Second},
		gatewayURL: srv.URL,
	}

	status := app.GetServerStatus()
	if !status.Running {
		t.Fatal("expected Running=true even when /api/v1/status returns 401")
	}
	if status.Version != "" {
		t.Fatalf("expected empty version when status is unauthorized, got %q", status.Version)
	}
}
