package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
)

// writeMinimalGoModule lays down a tiny buildable Go module under dir so the rfc
// scan + graph load run against a real (if trivial) project.
func writeMinimalGoModule(t *testing.T, dir string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/rfctest\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\n// TODO: tidy this up later\nfunc main() {}\n")
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func rfcTestConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"
	return cfg
}

func TestIsRFCLensFlagRouting(t *testing.T) {
	for _, name := range []string{"rfc", "RFC", "  Rfc "} {
		if !architect.IsRFCLens(name) {
			t.Fatalf("IsRFCLens(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "core", "refactor", "depdoctor"} {
		if architect.IsRFCLens(name) {
			t.Fatalf("IsRFCLens(%q) = true, want false", name)
		}
	}
}

// TestRunArchitectRFC_DryRunWritesNothing proves the rfc lens in dry-run prints
// the document and leaves .agent/system untouched.
func TestRunArchitectRFC_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeMinimalGoModule(t, dir)
	cfg := rfcTestConfig()

	f := &architectFlags{dryRun: true, lens: "rfc"}
	if err := runArchitectRFC(context.Background(), cfg, dir, f); err != nil {
		t.Fatalf("runArchitectRFC dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, architect.ADRDir)); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not create %s: %v", architect.ADRDir, err)
	}
}

// TestRunArchitectRFC_CreateWritesADR proves --create-issues (dryRun=false)
// writes a single rfc_<slug>.md under .agent/system with the full document.
func TestRunArchitectRFC_CreateWritesADR(t *testing.T) {
	dir := t.TempDir()
	writeMinimalGoModule(t, dir)
	cfg := rfcTestConfig()

	// Force the backend-enrichment attempt to fail closed (no real backend) so
	// the offline document is what gets written; an unreachable backend type
	// makes Propose error and enrichRFCWithBackend fall back.
	f := &architectFlags{dryRun: false, lens: "rfc", backend: "definitely-not-a-real-backend"}
	if err := runArchitectRFC(context.Background(), cfg, dir, f); err != nil {
		t.Fatalf("runArchitectRFC create: %v", err)
	}

	sysDir := filepath.Join(dir, architect.ADRDir)
	entries, err := os.ReadDir(sysDir)
	if err != nil {
		t.Fatalf("read %s: %v", sysDir, err)
	}
	var rfcFiles []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "rfc_") && strings.HasSuffix(e.Name(), ".md") {
			rfcFiles = append(rfcFiles, e.Name())
		}
	}
	if len(rfcFiles) != 1 {
		t.Fatalf("expected exactly one rfc_*.md, got %v", rfcFiles)
	}
	body, err := os.ReadFile(filepath.Join(sysDir, rfcFiles[0]))
	if err != nil {
		t.Fatalf("read rfc: %v", err)
	}
	for _, must := range []string{"## Problem Statement", "## Decision", "## Risk & Rollback", "## Tiny-PR Sequence"} {
		if !strings.Contains(string(body), must) {
			t.Fatalf("written RFC missing section %q", must)
		}
	}
}
