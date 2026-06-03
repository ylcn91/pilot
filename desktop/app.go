package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/memory"
)

// App is the Wails application struct. Its exported methods are bound to the
// frontend and callable from JavaScript/TypeScript via the generated bindings.
type App struct {
	ctx                 context.Context
	store               *memory.Store
	httpClient          *http.Client
	gatewayURL          string // e.g. "http://127.0.0.1:9090"
	issueRepo           string // "owner/repo" used to build GitHub issue URLs
	mu                  sync.Mutex
	gatewayCmd          *exec.Cmd
	gatewayDone         chan error
	gatewayStarting     bool
	gatewayStartedByApp bool
	gatewayConfigDir    string
	gatewayProjectPath  string
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
	if err == nil && cfg.Adapters != nil && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Repo != "" {
		a.issueRepo = cfg.Adapters.GitHub.Repo
	}
	if cfg != nil {
		a.gatewayProjectPath = resolveConfiguredProjectPath(cfg)
	}
	if a.gatewayProjectPath == "" {
		if cwd, err := a.getwd(); err == nil {
			a.gatewayProjectPath = cwd
		}
	}
}

// shutdown is called when the app is about to quit.
func (a *App) shutdown(_ context.Context) {
	a.stopManagedGateway()
	if a.store != nil {
		_ = a.store.Close()
	}
}
