package executor

import (
	"context"
	"testing"
)

func TestResolveBaseBranch(t *testing.T) {
	tests := []struct {
		name       string
		baseBranch string
		want       string
	}{
		{"explicit branch", "dev", "dev"},
		{"explicit with origin prefix stripped", "origin/release", "release"},
		{"empty falls back to main outside a git repo", "", "main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A non-git temp dir makes GetDefaultBranch fail, exercising the
			// "main" fallback for the empty case without a real repo.
			g := NewGitOperations(t.TempDir()).WithBaseBranch(tt.baseBranch)
			if got := g.resolveBaseBranch(context.Background()); got != tt.want {
				t.Errorf("resolveBaseBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetDefaultBranchUsesCurrentUpstream(t *testing.T) {
	localRepo, _ := setupSyncTestRepos(t, "dev")
	runGit(t, localRepo, "update-ref", "-d", "refs/remotes/origin/HEAD")

	got, err := NewGitOperations(localRepo).GetDefaultBranch(context.Background())
	if err != nil {
		t.Fatalf("GetDefaultBranch: %v", err)
	}
	if got != "dev" {
		t.Fatalf("GetDefaultBranch() = %q, want dev", got)
	}
}
