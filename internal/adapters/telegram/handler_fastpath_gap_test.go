package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFastpathFile is a small helper to materialize a file under a temp
// project root, creating parent dirs as needed.
func writeFastpathFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFastListTasks_BacklogTruncationAndRecentlyDone exercises the pagination
// branches in fastListTasks that the existing tests do not reach:
//   - more than 5 pending tasks => "+N more planned"
//   - more than 2 completed tasks => only the last 2 under "Recently done"
//   - the progress percentage reflects completed/total.
func TestFastListTasks_BacklogTruncationAndRecentlyDone(t *testing.T) {
	tmp := t.TempDir()

	// 7 pending tasks => backlog shows 5 + "+2 more planned".
	for i := 1; i <= 7; i++ {
		writeFastpathFile(t, tmp,
			filepath.Join(".agent", "tasks", fmt.Sprintf("TASK-%02d-pending.md", i)),
			fmt.Sprintf("# TASK-%02d: Pending %d\n**Status**: backlog\n", i, i))
	}
	// 3 completed tasks => recently-done shows only the last 2.
	for i := 20; i <= 22; i++ {
		writeFastpathFile(t, tmp,
			filepath.Join(".agent", "tasks", fmt.Sprintf("TASK-%02d-done.md", i)),
			fmt.Sprintf("# TASK-%02d: Done %d\n**Status**: complete\n", i, i))
	}

	h := &Handler{projectPath: tmp}
	result := h.fastListTasks()

	if result == "" {
		t.Fatal("expected non-empty task listing")
	}
	if !strings.Contains(result, "+2 more planned") {
		t.Errorf("expected '+2 more planned' for 7 pending tasks, got:\n%s", result)
	}
	if !strings.Contains(result, "Recently done") {
		t.Errorf("expected 'Recently done' section, got:\n%s", result)
	}
	// 3 done / 10 total = 30%.
	if !strings.Contains(result, "30%") {
		t.Errorf("expected progress '30%%' (3 done of 10), got:\n%s", result)
	}
}

// TestFastListTasks_FallsBackWhenOnlyNonTaskFiles confirms that directory
// entries without a TASK-NN number are skipped and, when none qualify, the
// function returns "" so the caller falls back to Claude.
func TestFastListTasks_FallsBackWhenOnlyNonTaskFiles(t *testing.T) {
	tmp := t.TempDir()
	writeFastpathFile(t, tmp, filepath.Join(".agent", "tasks", "README.md"), "# Not a task\n")
	writeFastpathFile(t, tmp, filepath.Join(".agent", "tasks", "notes.txt"), "ignored, not markdown\n")

	h := &Handler{projectPath: tmp}
	if got := h.fastListTasks(); got != "" {
		t.Errorf("expected empty result when no TASK-NN files present, got:\n%s", got)
	}
}

// TestFastGrepTodos_ScansPythonFiles verifies that fastGrepTodos scans .py
// files, not just .go. A lone python file (well under the 15-entry cap) is the
// only source containing a TODO.
func TestFastGrepTodos_ScansPythonFiles(t *testing.T) {
	tmp := t.TempDir()
	writeFastpathFile(t, tmp, filepath.Join("src", "app.py"),
		"# TODO: rewrite in rust\nprint('hi')\n")

	h := &Handler{projectPath: tmp}
	result := h.fastGrepTodos()

	if !strings.Contains(result, "TODOs & FIXMEs") {
		t.Fatalf("expected TODO header, got:\n%s", result)
	}
	if !strings.Contains(result, "src/app.py") {
		t.Errorf("expected python file to be scanned, got:\n%s", result)
	}
	if !strings.Contains(result, "rewrite in rust") {
		t.Errorf("expected TODO text from python file, got:\n%s", result)
	}
}

// TestFastGrepTodos_CapsAtFifteen verifies the 15-entry cap (filepath.SkipAll
// early-exit branch). A single Go file with 30 TODO lines must yield at most
// 15 rendered entries plus the "showing first 15" notice.
func TestFastGrepTodos_CapsAtFifteen(t *testing.T) {
	tmp := t.TempDir()

	var b strings.Builder
	b.WriteString("package thing\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "// TODO: item %d\n", i)
	}
	writeFastpathFile(t, tmp, filepath.Join("internal", "thing.go"), b.String())

	h := &Handler{projectPath: tmp}
	result := h.fastGrepTodos()

	if !strings.Contains(result, "showing first 15") {
		t.Errorf("expected 15-entry cap notice, got:\n%s", result)
	}

	var bullets int
	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(line, "• ") {
			bullets++
		}
	}
	if bullets > 15 {
		t.Errorf("expected at most 15 TODO entries, got %d:\n%s", bullets, result)
	}
}

// TestFastGrepTodos_SkipsNonSourceDirsAndExtensions ensures that files outside
// the known source dirs, and non-.go/.py files inside them, are ignored.
func TestFastGrepTodos_SkipsNonSourceDirsAndExtensions(t *testing.T) {
	tmp := t.TempDir()

	// TODO in a markdown file inside a scanned dir => ignored (wrong extension).
	writeFastpathFile(t, tmp, filepath.Join("internal", "notes.md"), "TODO: not a source file\n")
	// TODO in a dir that is not in the scan list => ignored.
	writeFastpathFile(t, tmp, filepath.Join("docs", "guide.go"), "// TODO: ignored dir\n")

	h := &Handler{projectPath: tmp}
	result := h.fastGrepTodos()

	if !strings.Contains(result, "No TODOs or FIXMEs found") {
		t.Errorf("expected 'No TODOs' fallback, got:\n%s", result)
	}
}

// TestTryFastAnswer_ReturnsContentForSeededProject drives tryFastAnswer through
// each routed branch against a real temp project that actually contains the
// data each fast path reads, asserting it returns substantive (non-empty)
// answers rather than falling back to Claude.
func TestTryFastAnswer_ReturnsContentForSeededProject(t *testing.T) {
	tmp := t.TempDir()

	writeFastpathFile(t, tmp,
		filepath.Join(".agent", "tasks", "TASK-01-feature.md"),
		"# TASK-01: Ship feature\n**Status**: backlog\n")

	writeFastpathFile(t, tmp,
		filepath.Join(".agent", "DEVELOPMENT-README.md"),
		"# Dev README\n\n## Current State\n\n- Gateway online\n- Adapter wired\n")

	writeFastpathFile(t, tmp,
		filepath.Join("cmd", "main.go"),
		"package main\n\n// TODO: handle shutdown\nfunc main() {}\n")

	h := &Handler{projectPath: tmp}

	tests := []struct {
		name        string
		question    string
		wantSubstrs []string
	}{
		{
			name:        "tasks routes to fastListTasks",
			question:    "what tasks are in the backlog?",
			wantSubstrs: []string{"Backlog", "Progress:"},
		},
		{
			name:        "status routes to fastReadStatus",
			question:    "what is the current state?",
			wantSubstrs: []string{"Project Status", "Current State"},
		},
		{
			name:        "todos routes to fastGrepTodos",
			question:    "list the todos please",
			wantSubstrs: []string{"TODOs & FIXMEs", "handle shutdown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.tryFastAnswer(tt.question)
			if got == "" {
				t.Fatalf("tryFastAnswer(%q) returned empty, expected fast answer", tt.question)
			}
			for _, sub := range tt.wantSubstrs {
				if !strings.Contains(got, sub) {
					t.Errorf("tryFastAnswer(%q) missing %q, got:\n%s", tt.question, sub, got)
				}
			}
		})
	}
}

// TestTryFastAnswer_RoutingPriority documents that the switch in tryFastAnswer
// evaluates the "tasks"-family case before the "todos"-family case. A question
// containing both "todo list" and "todos" hits the first case (fastListTasks),
// which returns "" when no tasks dir exists rather than the "No TODOs" message.
func TestTryFastAnswer_RoutingPriority(t *testing.T) {
	tmp := t.TempDir() // empty project: no .agent/tasks
	h := &Handler{projectPath: tmp}

	// "todo list" matches the first case (tasks); with no tasks dir it returns "".
	if got := h.tryFastAnswer("show me the todo list"); got != "" {
		t.Errorf("expected empty (tasks case wins, no tasks dir), got:\n%s", got)
	}

	// A pure "todos" question (no "todo list") falls to the todos case, which
	// always returns the friendly no-todos message.
	got := h.tryFastAnswer("any todos around?")
	if !strings.Contains(got, "No TODOs or FIXMEs found") {
		t.Errorf("expected 'No TODOs' message for todos question, got:\n%s", got)
	}
}
