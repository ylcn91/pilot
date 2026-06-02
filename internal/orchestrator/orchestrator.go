package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// Config holds orchestrator configuration
type Config struct {
	Model         string
	MaxConcurrent int
	BackendConfig *executor.BackendConfig // GH-2286: pass executor config to runner
}

// Orchestrator coordinates ticket processing and task execution
type Orchestrator struct {
	config   *Config
	bridge   *Bridge
	runner   *executor.Runner
	monitor  *executor.Monitor
	notifier *slack.Notifier

	taskQueue             chan *Task
	running               map[string]bool
	progressCallback      func(taskID, phase string, progress int, message string)
	completionCallback    func(taskID, prURL string, success bool, errMsg string)
	qualityCheckerFactory executor.QualityCheckerFactory
	mu                    sync.Mutex
	wg                    sync.WaitGroup
	ctx                   context.Context
	cancel                context.CancelFunc
}

// Task represents a task to be processed
type Task struct {
	ID          string
	Ticket      *linear.Issue
	Document    *TaskDocument
	ProjectPath string
	Branch      string
	Priority    float64
}

// NewOrchestrator creates a new orchestrator
func NewOrchestrator(config *Config, notifier *slack.Notifier) (*Orchestrator, error) {
	bridge, err := NewBridge()
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// GH-2286: use executor config instead of hardcoded defaults
	backendCfg := config.BackendConfig
	if backendCfg == nil {
		backendCfg = executor.DefaultBackendConfig()
	}
	runner, runnerErr := executor.NewRunnerWithConfig(backendCfg)
	if runnerErr != nil {
		cancel()
		return nil, fmt.Errorf("failed to create runner: %w", runnerErr)
	}

	o := &Orchestrator{
		config:    config,
		bridge:    bridge,
		runner:    runner,
		monitor:   executor.NewMonitor(),
		notifier:  notifier,
		taskQueue: make(chan *Task, 100),
		running:   make(map[string]bool),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Set up progress callback
	o.runner.OnProgress(o.handleProgress)

	return o, nil
}

// Start starts the orchestrator workers
func (o *Orchestrator) Start() {
	maxWorkers := o.config.MaxConcurrent
	if maxWorkers <= 0 {
		maxWorkers = 2
	}

	for i := 0; i < maxWorkers; i++ {
		o.wg.Add(1)
		go o.worker(i)
	}

	logging.WithComponent("orchestrator").Info("Orchestrator started", slog.Int("workers", maxWorkers))
}

// Stop stops the orchestrator
func (o *Orchestrator) Stop() {
	o.cancel()
	close(o.taskQueue)
	// Terminate all running subprocesses (GH-883)
	o.runner.CancelAll()
	o.wg.Wait()
	logging.WithComponent("orchestrator").Info("Orchestrator stopped")
}
