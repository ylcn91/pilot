package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrPythonNotFound is returned when the configured python interpreter cannot
// be located/executed at call time. It is surfaced fast (before the context
// deadline elapses) so a missing interpreter does not hang queue workers.
var ErrPythonNotFound = errors.New("python interpreter not found")

// ErrHealthCheckFailed is returned when the interpreter is present and runs but
// the probe does not produce the expected sentinel output (e.g. a broken or
// incompatible interpreter). It lets callers distinguish "no interpreter" from
// "interpreter exists but is unusable" and degrade accordingly.
var ErrHealthCheckFailed = errors.New("python health check failed")

// healthCheckSentinel is the marker a healthy interpreter must echo back. It is
// deliberately unlikely to appear by accident in unrelated output.
const healthCheckSentinel = "pilot-bridge-ok"

// pythonTimeout bounds a single subprocess invocation. Without it a slow or
// hung python process would block the calling queue worker indefinitely.
const pythonTimeout = 30 * time.Second

// Bridge handles communication between Go and Python orchestrator
type Bridge struct {
	pythonPath string
	scriptDir  string
}

// NewBridge creates a new Python bridge
func NewBridge() (*Bridge, error) {
	// Find Python executable
	pythonPath, err := findPython()
	if err != nil {
		return nil, fmt.Errorf("failed to find Python: %w", err)
	}

	// Get script directory (relative to this package)
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("failed to get caller info")
	}

	// Navigate from internal/orchestrator to orchestrator/
	scriptDir := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filename))), "orchestrator")

	return &Bridge{
		pythonPath: pythonPath,
		scriptDir:  scriptDir,
	}, nil
}

// findPython finds the Python executable
func findPython() (string, error) {
	// Try python3 first, then python
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("python not found in PATH")
}

// HealthCheck probes the configured interpreter to confirm it can actually
// execute, not merely that its path resolves. It runs a trivial script that
// echoes a sentinel and verifies the output. A missing/unexecutable
// interpreter surfaces as ErrPythonNotFound (fast, before the deadline); an
// interpreter that runs but does not return the sentinel — broken, wrong
// runtime, or a non-zero exit — surfaces as ErrHealthCheckFailed. Callers can
// use this at startup to degrade gracefully instead of failing mid-queue.
func (b *Bridge) HealthCheck(ctx context.Context) error {
	script := fmt.Sprintf("print(%q)", healthCheckSentinel)

	output, err := b.runPython(ctx, script)
	if err != nil {
		// A missing interpreter is already a distinct sentinel from runPython;
		// pass it through unwrapped so errors.Is(err, ErrPythonNotFound) holds.
		if errors.Is(err, ErrPythonNotFound) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrHealthCheckFailed, err)
	}

	if !strings.Contains(output, healthCheckSentinel) {
		return fmt.Errorf("%w: unexpected probe output %q", ErrHealthCheckFailed, strings.TrimSpace(output))
	}

	return nil
}

// TicketData represents ticket data for planning
type TicketData struct {
	ID          string   `json:"id"`
	Identifier  string   `json:"identifier"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    int      `json:"priority"`
	Labels      []string `json:"labels"`
	Project     string   `json:"project,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
}

// TaskDocument represents the generated task document
type TaskDocument struct {
	ID       string
	Title    string
	Markdown string
}

// PlanTicket converts a ticket to a task document
func (b *Bridge) PlanTicket(ctx context.Context, ticket *TicketData) (*TaskDocument, error) {
	input, err := json.Marshal(ticket)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ticket: %w", err)
	}

	script := fmt.Sprintf(`
import sys
import json
sys.path.insert(0, %q)
from planner import plan_ticket

ticket = json.loads(%q)
result = plan_ticket(ticket)
print(result)
`, b.scriptDir, string(input))

	output, err := b.runPython(ctx, script)
	if err != nil {
		return nil, err
	}

	return &TaskDocument{
		ID:       fmt.Sprintf("TASK-%s", ticket.Identifier),
		Title:    ticket.Title,
		Markdown: output,
	}, nil
}

// ScoredTask represents a scored task from priority scoring
type ScoredTask struct {
	TaskID      string             `json:"task_id"`
	Title       string             `json:"title"`
	RawPriority int                `json:"raw_priority"`
	Score       float64            `json:"score"`
	Factors     map[string]float64 `json:"factors"`
}

// ScoreTasks scores and prioritizes tasks
func (b *Bridge) ScoreTasks(ctx context.Context, tasks []map[string]interface{}) ([]ScoredTask, error) {
	input, err := json.Marshal(tasks)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tasks: %w", err)
	}

	script := fmt.Sprintf(`
import sys
import json
sys.path.insert(0, %q)
from priority import score_tasks

tasks = json.loads(%q)
result = score_tasks(tasks)
print(json.dumps(result))
`, b.scriptDir, string(input))

	output, err := b.runPython(ctx, script)
	if err != nil {
		return nil, err
	}

	var scored []ScoredTask
	if err := json.Unmarshal([]byte(output), &scored); err != nil {
		return nil, fmt.Errorf("failed to parse scored tasks: %w", err)
	}

	return scored, nil
}

// GenerateBrief generates a daily brief
func (b *Bridge) GenerateBrief(ctx context.Context, tasks []map[string]interface{}, format string) (string, error) {
	input, err := json.Marshal(tasks)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tasks: %w", err)
	}

	script := fmt.Sprintf(`
import sys
import json
sys.path.insert(0, %q)
from briefing import generate_brief

tasks = json.loads(%q)
result = generate_brief(tasks, %q)
print(result)
`, b.scriptDir, string(input), format)

	return b.runPython(ctx, script)
}

// runPython executes a Python script and returns output. It bounds the
// subprocess with pythonTimeout and fails fast with ErrPythonNotFound when the
// interpreter cannot be executed, so a slow or missing python never hangs the
// caller longer than the deadline.
func (b *Bridge) runPython(ctx context.Context, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pythonTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, b.pythonPath, "-c", script)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// A missing/unexecutable interpreter is reported by exec without ever
		// reaching the deadline; surface it as a distinct, fast error.
		if isExecStartError(err) {
			return "", fmt.Errorf("%w (%s): %v", ErrPythonNotFound, b.pythonPath, err)
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("python timed out after %s: %w", pythonTimeout, ctx.Err())
		}
		return "", fmt.Errorf("python error: %v: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// isExecStartError reports whether err is a failure to start the process
// (e.g. the binary does not exist or is not executable), as opposed to the
// process running and exiting non-zero.
func isExecStartError(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// The process started and exited non-zero — not a start failure.
		return false
	}
	return errors.Is(err, exec.ErrNotFound) || os.IsNotExist(err) || errors.Is(err, os.ErrPermission)
}
