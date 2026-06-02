package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var errTestGatewayStart = errors.New("gateway start failed")

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
	projectRoot := t.TempDir()
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
			if strings.Contains(configText, "ylcn91/pilot") {
				t.Fatal("managed gateway config must not reference upstream")
			}
			for _, want := range []string{
				"enabled: false",
				"auto_create_issues: false",
				"source_enabled: false",
				"codex_runtime:",
				`sandbox: "read-only"`,
			} {
				if !strings.Contains(configText, want) {
					t.Fatalf("managed gateway config missing %q", want)
				}
			}
			if projectPath != projectRoot {
				t.Fatalf("projectPath = %q, want %q", projectPath, projectRoot)
			}
			healthy.Store(true)
			return &exec.Cmd{}, nil
		},
		getwd: func() (string, error) { return projectRoot, nil },
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
	projectRoot := t.TempDir()

	configPath, projectPath, configDir, err := app.prepareManagedGatewayConfig(projectRoot)
	if err != nil {
		t.Fatalf("prepareManagedGatewayConfig error: %v", err)
	}
	defer os.RemoveAll(configDir)

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	configText := string(configBytes)
	if strings.Contains(configText, "ylcn91/pilot") {
		t.Fatal("runtime config must not reference upstream")
	}
	for _, want := range []string{
		`host: "127.0.0.1"`,
		"port: 19091",
		"auto_create_issues: false",
		"source_enabled: false",
		"cross_project: false",
		"codex_runtime:",
		`sandbox: "read-only"`,
	} {
		if !strings.Contains(configText, want) {
			t.Fatalf("runtime config missing %q", want)
		}
	}
	if projectPath != projectRoot {
		t.Fatalf("projectPath = %q, want %q", projectPath, projectRoot)
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
