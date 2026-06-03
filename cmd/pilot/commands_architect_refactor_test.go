package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
)

// captureArchitectStdout runs fn with os.Stdout redirected to a pipe and returns
// everything it wrote. It restores os.Stdout before returning so a failure in fn
// never leaks the redirect into later tests.
func captureArchitectStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = orig
	return <-done
}

// refactorTestModule lays down a tiny buildable Go module with two packages
// (one a leaf, one depended-on) plus oversized + TODO signals so the refactor
// scan + offline plan produce a real, non-empty ordered sequence.
func refactorTestModule(t *testing.T, dir string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/reftest\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(dir, "main.go"),
		"package main\n\nimport _ \"example.com/reftest/internal/leaf\"\n\n// TODO: tidy main\nfunc main() {}\n")
	mustWrite(t, filepath.Join(dir, "internal", "leaf", "leaf.go"),
		"package leaf\n\n// TODO: revisit leaf\nfunc Leaf() {}\n")
}

// TestRunArchitectRefactor_DryRunOrderedSequence is the headline proof: the
// refactor lens dry-run emits the ordered PR-sequence banner + numbered lines,
// and touches no disk (no .agent/system, no issues).
func TestRunArchitectRefactor_DryRunOrderedSequence(t *testing.T) {
	dir := t.TempDir()
	refactorTestModule(t, dir)
	cfg := rfcTestConfig()

	f := &architectFlags{dryRun: true, lens: "refactor"}
	var runErr error
	out := captureArchitectStdout(t, func() {
		runErr = runArchitectRefactor(context.Background(), cfg, dir, f)
	})
	if runErr != nil {
		t.Fatalf("runArchitectRefactor dry-run: %v", runErr)
	}

	for _, must := range []string{
		"ordered refactor PR sequence",
		"Refactor plan:",
		"ordered PR(s)",
		"blast-radius ordered, leaves first",
		"(dry-run) nothing created",
	} {
		if !strings.Contains(out, must) {
			t.Fatalf("output missing %q\ngot:\n%s", must, out)
		}
	}
	// At least one numbered PR line must be present.
	if !strings.Contains(out, "1. ") {
		t.Fatalf("expected at least one numbered PR line, got:\n%s", out)
	}
	// Dry-run must not create the ADR dir or any file.
	if _, err := os.Stat(filepath.Join(dir, architect.ADRDir)); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not create %s: %v", architect.ADRDir, err)
	}
}

// TestRunArchitectRefactor_RoutedFromRunArchitect proves the routing in
// runArchitect sends --lens refactor --dry-run to the ordered-sequence path
// (not the generic flat finding printer) by asserting the sequence banner.
func TestRunArchitectRefactor_RoutedFromRunArchitect(t *testing.T) {
	dir := t.TempDir()
	refactorTestModule(t, dir)

	cfgPath := filepath.Join(dir, "pilot.yaml")
	mustWrite(t, cfgPath, "executor:\n  type: claude-code\narchitect:\n  enabled: true\n")

	// runArchitect resolves the project root from CWD; point it at the module.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origCfgFile := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = origCfgFile })

	f := &architectFlags{dryRun: true, lens: "refactor"}
	var runErr error
	out := captureArchitectStdout(t, func() {
		runErr = runArchitect(context.Background(), f)
	})
	if runErr != nil {
		t.Fatalf("runArchitect refactor dry-run: %v", runErr)
	}
	if !strings.Contains(out, "ordered refactor PR sequence") {
		t.Fatalf("refactor dry-run must route to the ordered sequence printer, got:\n%s", out)
	}
	// It must NOT fall through to the generic "would create N issue(s)" tail.
	if strings.Contains(out, "would create") {
		t.Fatalf("refactor dry-run must not use the generic finding printer, got:\n%s", out)
	}
}

// TestSignalFiles_DistinctOrdered proves the ownership-probe input is the
// distinct, first-seen-ordered set of signal files (empties dropped).
func TestSignalFiles_DistinctOrdered(t *testing.T) {
	signals := []architect.Signal{
		{File: "a.go"},
		{File: ""},
		{File: "b.go"},
		{File: "a.go"},
		{File: "c.go"},
		{File: "b.go"},
	}
	got := signalFiles(signals)
	want := []string{"a.go", "b.go", "c.go"}
	if len(got) != len(want) {
		t.Fatalf("signalFiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("signalFiles[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestSignalFiles_EmptyInput(t *testing.T) {
	if got := signalFiles(nil); len(got) != 0 {
		t.Fatalf("signalFiles(nil) = %v, want empty", got)
	}
	if got := signalFiles([]architect.Signal{{File: ""}, {File: ""}}); len(got) != 0 {
		t.Fatalf("signalFiles(all-empty) = %v, want empty", got)
	}
}
