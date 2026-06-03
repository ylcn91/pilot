package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/pilotapi"
	"github.com/ylcn91/pilot/internal/testutil"
)

type recordingArchitectCreator struct {
	titles []string
	bodies []string
}

func (r *recordingArchitectCreator) CreatePilotIssue(_ context.Context, _, _, title, body string, _ []string) (*github.Issue, error) {
	r.titles = append(r.titles, title)
	r.bodies = append(r.bodies, body)
	return &github.Issue{Number: len(r.titles), Title: title}, nil
}

func TestEmitRefactorPlanToLinearSurface_PreservesPlanOrder(t *testing.T) {
	plan := architect.RefactorPlan{
		PRs: []architect.PlannedPR{
			{
				Title:     "Split leaf",
				Class:     "move-only",
				Order:     1,
				PilotSafe: true,
				Risk:      pilotapi.RiskLow,
				Files:     []string{"internal/leaf/a.go"},
			},
			{
				Title:        "Break core cycle",
				Class:        "behavior",
				Order:        2,
				DependsOn:    []int{1},
				PilotSafe:    false,
				ManualReason: "high blast radius",
				Risk:         pilotapi.RiskHigh,
				Files:        []string{"internal/core/a.go"},
			},
		},
	}
	creator := &recordingArchitectCreator{}

	created, skipped, err := emitRefactorPlan(context.Background(), plan, creator, nil, nil, 0)
	if err != nil {
		t.Fatalf("emitRefactorPlan: %v", err)
	}
	if created != 2 || skipped != 0 {
		t.Fatalf("created=%d skipped=%d, want 2/0", created, skipped)
	}
	if got := strings.Join(creator.titles, " | "); got != "refactor(internal): Split leaf | refactor(internal): Break core cycle" {
		t.Fatalf("Linear export must preserve plan order, got %q", got)
	}
	if !strings.Contains(creator.bodies[1], "Order: PR 2") || !strings.Contains(creator.bodies[1], "Land after PR 1") {
		t.Fatalf("second body missing lineage/order detail:\n%s", creator.bodies[1])
	}
}

func TestRunArchitectRFC_LinearCreateRequiresExistingParent(t *testing.T) {
	dir := t.TempDir()
	writeMinimalGoModule(t, dir)
	cfg := rfcTestConfig()
	cfg.Adapters.Linear.APIKey = testutil.FakeLinearAPIKey

	f := &architectFlags{
		dryRun: false,
		lens:   "rfc",
		export: "linear",
		// Force backend enrichment to fail before any subprocess is spawned; the
		// Linear parent validation remains the behavior under test.
		backend: "definitely-not-a-real-backend",
	}
	err := runArchitectRFC(context.Background(), cfg, dir, f)
	if err == nil {
		t.Fatal("RFC Linear export without --linear-parent must error")
	}
	if !strings.Contains(err.Error(), "linear-parent") {
		t.Fatalf("error should mention linear-parent, got %q", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, architect.ADRDir)); !os.IsNotExist(statErr) {
		t.Fatalf("Linear export must not write an ADR when parent validation fails: %v", statErr)
	}
}

func TestBuildArchitectRunConfig_LinearExportDryRunNeedsNoAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true, Export: config.ArchitectExportLinear}
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"
	cfg.Adapters.Linear.APIKey = ""
	t.Setenv("LINEAR_API_KEY", "")

	rc, err := buildArchitectRunConfig(cfg, t.TempDir(), &architectFlags{dryRun: true})
	if err != nil {
		t.Fatalf("dry-run Linear export must not require Linear API key: %v", err)
	}
	if rc.Creator != nil || rc.Searcher != nil {
		t.Fatalf("dry-run Linear export must not construct creator/searcher: %+v", rc)
	}
}
