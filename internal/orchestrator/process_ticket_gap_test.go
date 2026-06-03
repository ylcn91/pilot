package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/executor"
)

// newStubPython writes an executable shell-script stub to a temp dir that
// ignores its arguments and prints a fixed payload on stdout. Pointing a
// Bridge's pythonPath at it makes Bridge.PlanTicket hermetic: runPython
// shells out to the stub instead of a real python3 interpreter, so the
// orchestrator's Process*Ticket happy paths can be exercised without the
// Python planner module being present. Returns the stub path and the exact
// payload the stub emits (which PlanTicket surfaces as TaskDocument.Markdown).
func newStubPython(t *testing.T, payload string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script python stub is POSIX-only")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "python-stub.sh")
	// `printf %s` avoids appending a trailing newline so the payload the test
	// expects matches exactly what PlanTicket returns.
	script := "#!/bin/sh\nprintf '%s' " + shellQuote(payload) + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return stub
}

// shellQuote wraps s in single quotes, escaping embedded single quotes, so it
// can be passed safely as a single shell word.
func shellQuote(s string) string {
	out := "'"
	for _, r := range s {
		if r == '\'' {
			out += `'\''`
			continue
		}
		out += string(r)
	}
	return out + "'"
}

// newTestOrchestrator builds an Orchestrator wired with a hermetic Bridge
// (backed by the stub python) and the minimal collaborators the Process*Ticket
// paths touch: a monitor (QueueTask registers against it) and a buffered
// taskQueue (QueueTask sends into it). No workers are started, so queued tasks
// stay in the channel for assertion. notifier is nil — the Process* methods do
// not touch it before queueing.
func newTestOrchestrator(t *testing.T, markdown string) *Orchestrator {
	t.Helper()
	return &Orchestrator{
		config: &Config{},
		bridge: &Bridge{
			pythonPath: newStubPython(t, markdown),
			scriptDir:  t.TempDir(), // never read: the stub ignores the script
		},
		monitor:   executor.NewMonitor(),
		taskQueue: make(chan *Task, 8),
		running:   make(map[string]bool),
	}
}

// drainOne returns the single task expected to be sitting in the queue,
// failing if the queue is empty.
func drainOne(t *testing.T, o *Orchestrator) *Task {
	t.Helper()
	select {
	case task := <-o.taskQueue:
		return task
	default:
		t.Fatal("expected a task to be queued, queue was empty")
		return nil
	}
}

// assertDocSaved verifies saveTaskDocument wrote the markdown to the
// conventional <projectPath>/.agent/tasks/<docID>.md location.
func assertDocSaved(t *testing.T, projectPath, docID, wantMarkdown string) {
	t.Helper()
	path := filepath.Join(projectPath, ".agent", "tasks", docID+".md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected task document at %s: %v", path, err)
	}
	if string(got) != wantMarkdown {
		t.Errorf("saved markdown = %q, want %q", string(got), wantMarkdown)
	}
}

func TestProcessTicket_LinearHappyPath(t *testing.T) {
	const markdown = "# Plan for ABC-42\n\nDo the thing."
	o := newTestOrchestrator(t, markdown)
	projectPath := t.TempDir()

	issue := &linear.Issue{
		ID:          "issue-uuid-1",
		Identifier:  "ABC-42",
		Title:       "Implement feature X",
		Description: "Detailed description",
		Priority:    3,
		Labels: []linear.Label{
			{ID: "l1", Name: "enhancement"},
			{ID: "l2", Name: "backend"},
		},
	}

	if err := o.ProcessTicket(context.Background(), issue, projectPath); err != nil {
		t.Fatalf("ProcessTicket: %v", err)
	}

	task := drainOne(t, o)
	if task.ID != "TASK-ABC-42" {
		t.Errorf("task.ID = %q, want TASK-ABC-42", task.ID)
	}
	if task.Branch != "pilot/ABC-42" {
		t.Errorf("task.Branch = %q, want pilot/ABC-42", task.Branch)
	}
	if task.Ticket != issue {
		t.Error("task.Ticket should reference the originating Linear issue")
	}
	if task.Document == nil || task.Document.Markdown != markdown {
		t.Errorf("task.Document.Markdown mismatch: got %+v", task.Document)
	}
	if task.Document.Title != "Implement feature X" {
		t.Errorf("task.Document.Title = %q, want %q", task.Document.Title, "Implement feature X")
	}
	assertDocSaved(t, projectPath, "TASK-ABC-42", markdown)
}

func TestProcessGithubTicket_HappyPath(t *testing.T) {
	const markdown = "# GH plan\n\nimplement."
	o := newTestOrchestrator(t, markdown)
	projectPath := t.TempDir()

	gh := &github.TaskInfo{
		ID:          "GH-42",
		Title:       "Fix the bug",
		Description: "bug detail",
		Priority:    github.Priority(2),
		Labels:      []string{"bug", "pilot"},
	}

	if err := o.ProcessGithubTicket(context.Background(), gh, projectPath); err != nil {
		t.Fatalf("ProcessGithubTicket: %v", err)
	}

	task := drainOne(t, o)
	if task.ID != "TASK-GH-42" {
		t.Errorf("task.ID = %q, want TASK-GH-42", task.ID)
	}
	if task.Branch != "pilot/GH-42" {
		t.Errorf("task.Branch = %q, want pilot/GH-42", task.Branch)
	}
	if task.Ticket != nil {
		t.Error("task.Ticket must be nil for GitHub-sourced tasks")
	}
	if task.Priority != 2 {
		t.Errorf("task.Priority = %v, want 2", task.Priority)
	}
	if task.Document.Title != "Fix the bug" {
		t.Errorf("task.Document.Title = %q, want %q", task.Document.Title, "Fix the bug")
	}
	assertDocSaved(t, projectPath, "TASK-GH-42", markdown)
}

func TestProcessGitlabTicket_HappyPath(t *testing.T) {
	const markdown = "# GL plan"
	o := newTestOrchestrator(t, markdown)
	projectPath := t.TempDir()

	gl := &gitlab.TaskInfo{
		ID:          "GL-7",
		Title:       "GitLab task",
		Description: "detail",
		Priority:    gitlab.Priority(1),
		Labels:      []string{"feature"},
	}

	if err := o.ProcessGitlabTicket(context.Background(), gl, projectPath); err != nil {
		t.Fatalf("ProcessGitlabTicket: %v", err)
	}

	task := drainOne(t, o)
	if task.ID != "TASK-GL-7" {
		t.Errorf("task.ID = %q, want TASK-GL-7", task.ID)
	}
	if task.Branch != "pilot/GL-7" {
		t.Errorf("task.Branch = %q, want pilot/GL-7", task.Branch)
	}
	if task.Ticket != nil {
		t.Error("task.Ticket must be nil for GitLab-sourced tasks")
	}
	if task.Priority != 1 {
		t.Errorf("task.Priority = %v, want 1", task.Priority)
	}
	assertDocSaved(t, projectPath, "TASK-GL-7", markdown)
}

func TestProcessJiraTicket_HappyPath(t *testing.T) {
	const markdown = "# Jira plan"
	o := newTestOrchestrator(t, markdown)
	projectPath := t.TempDir()

	jr := &jira.TaskInfo{
		ID:          "jira-internal-id",
		IssueKey:    "PROJ-123",
		Title:       "Jira task",
		Description: "detail",
		Priority:    jira.Priority(4),
		Labels:      []string{"chore"},
	}

	if err := o.ProcessJiraTicket(context.Background(), jr, projectPath); err != nil {
		t.Fatalf("ProcessJiraTicket: %v", err)
	}

	task := drainOne(t, o)
	// Document.ID is derived from Identifier (IssueKey), not the internal ID.
	if task.ID != "TASK-PROJ-123" {
		t.Errorf("task.ID = %q, want TASK-PROJ-123", task.ID)
	}
	if task.Branch != "pilot/PROJ-123" {
		t.Errorf("task.Branch = %q, want pilot/PROJ-123", task.Branch)
	}
	if task.Ticket != nil {
		t.Error("task.Ticket must be nil for Jira-sourced tasks")
	}
	if task.Priority != 4 {
		t.Errorf("task.Priority = %v, want 4", task.Priority)
	}
	assertDocSaved(t, projectPath, "TASK-PROJ-123", markdown)
}

func TestProcessAsanaTicket_HappyPath(t *testing.T) {
	const markdown = "# Asana plan"
	o := newTestOrchestrator(t, markdown)
	projectPath := t.TempDir()

	as := &asana.TaskInfo{
		ID:          "ASANA-999",
		Title:       "Asana task",
		Description: "detail",
		Priority:    asana.Priority(2),
		Labels:      []string{"design"},
		ProjectName: "Main Project",
	}

	if err := o.ProcessAsanaTicket(context.Background(), as, projectPath); err != nil {
		t.Fatalf("ProcessAsanaTicket: %v", err)
	}

	task := drainOne(t, o)
	if task.ID != "TASK-ASANA-999" {
		t.Errorf("task.ID = %q, want TASK-ASANA-999", task.ID)
	}
	if task.Branch != "pilot/ASANA-999" {
		t.Errorf("task.Branch = %q, want pilot/ASANA-999", task.Branch)
	}
	if task.Ticket != nil {
		t.Error("task.Ticket must be nil for Asana-sourced tasks")
	}
	if task.Priority != 2 {
		t.Errorf("task.Priority = %v, want 2", task.Priority)
	}
	assertDocSaved(t, projectPath, "TASK-ASANA-999", markdown)
}

func TestProcessPlaneTicket_HappyPath(t *testing.T) {
	const markdown = "# Plane plan"
	o := newTestOrchestrator(t, markdown)
	projectPath := t.TempDir()

	item := &plane.WebhookWorkItemData{
		ID:         "plane-uuid",
		Name:       "Plane work item",
		SequenceID: 17,
	}

	if err := o.ProcessPlaneTicket(context.Background(), item, projectPath); err != nil {
		t.Fatalf("ProcessPlaneTicket: %v", err)
	}

	task := drainOne(t, o)
	if task.ID != "TASK-PLANE-17" {
		t.Errorf("task.ID = %q, want TASK-PLANE-17", task.ID)
	}
	if task.Branch != "pilot/PLANE-17" {
		t.Errorf("task.Branch = %q, want pilot/PLANE-17", task.Branch)
	}
	if task.Document.Title != "Plane work item" {
		t.Errorf("task.Document.Title = %q, want %q", task.Document.Title, "Plane work item")
	}
	assertDocSaved(t, projectPath, "TASK-PLANE-17", markdown)
}

// TestProcessTicket_PlanError verifies that a PlanTicket failure (stub exits
// non-zero) surfaces as an error and the task is neither saved nor queued.
func TestProcessTicket_PlanError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script python stub is POSIX-only")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "python-fail.sh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write failing stub: %v", err)
	}

	o := &Orchestrator{
		config:    &Config{},
		bridge:    &Bridge{pythonPath: stub, scriptDir: t.TempDir()},
		monitor:   executor.NewMonitor(),
		taskQueue: make(chan *Task, 8),
		running:   make(map[string]bool),
	}
	projectPath := t.TempDir()

	issue := &linear.Issue{ID: "x", Identifier: "ABC-1", Title: "t"}
	err := o.ProcessTicket(context.Background(), issue, projectPath)
	if err == nil {
		t.Fatal("expected ProcessTicket to return an error when planning fails")
	}

	select {
	case <-o.taskQueue:
		t.Error("no task should be queued when planning fails")
	default:
	}
	if _, statErr := os.Stat(filepath.Join(projectPath, ".agent", "tasks")); statErr == nil {
		t.Error("no task document directory should be created when planning fails")
	}
}
