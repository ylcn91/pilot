package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectConfigFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: "1.0"
projects:
  - name: "full-project"
    path: "/path/to/project"
    navigator: true
    default_branch: "main"
    github:
      owner: "myorg"
      repo: "myrepo"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(config.Projects) != 1 {
		t.Fatalf("Projects length = %d, want 1", len(config.Projects))
	}

	project := config.Projects[0]
	if project.Name != "full-project" {
		t.Errorf("Project.Name = %q, want %q", project.Name, "full-project")
	}
	if project.Path != "/path/to/project" {
		t.Errorf("Project.Path = %q, want %q", project.Path, "/path/to/project")
	}
	if project.Navigator != true {
		t.Error("Project.Navigator should be true")
	}
	if project.DefaultBranch != "main" {
		t.Errorf("Project.DefaultBranch = %q, want %q", project.DefaultBranch, "main")
	}
	if project.GitHub == nil {
		t.Fatal("Project.GitHub is nil")
	}
	if project.GitHub.Owner != "myorg" {
		t.Errorf("Project.GitHub.Owner = %q, want %q", project.GitHub.Owner, "myorg")
	}
	if project.GitHub.Repo != "myrepo" {
		t.Errorf("Project.GitHub.Repo = %q, want %q", project.GitHub.Repo, "myrepo")
	}
}

// TestProjectConfigBranchFromYAML verifies the branch_from alias deserializes
// alongside default_branch (GH-2290).
func TestProjectConfigBranchFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
version: "1.0"
projects:
  - name: "proj"
    path: "/p"
    default_branch: "main"
    branch_from: "dev"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := cfg.Projects[0]
	if p.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want main", p.DefaultBranch)
	}
	if p.BranchFrom != "dev" {
		t.Errorf("BranchFrom = %q, want dev", p.BranchFrom)
	}
	if got := p.ResolveBaseBranch(); got != "dev" {
		t.Errorf("ResolveBaseBranch() = %q, want dev", got)
	}
}

// TestProjectConfigResolveBaseBranch covers the BranchFrom / DefaultBranch
// precedence introduced in GH-2290.
func TestProjectConfigResolveBaseBranch(t *testing.T) {
	tests := []struct {
		name string
		p    *ProjectConfig
		want string
	}{
		{"nil receiver", nil, ""},
		{"both empty", &ProjectConfig{}, ""},
		{"default_branch only", &ProjectConfig{DefaultBranch: "dev"}, "dev"},
		{"branch_from only", &ProjectConfig{BranchFrom: "dev"}, "dev"},
		{
			"branch_from wins over default_branch",
			&ProjectConfig{DefaultBranch: "main", BranchFrom: "dev"},
			"dev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.ResolveBaseBranch(); got != tt.want {
				t.Errorf("ResolveBaseBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProjectConfigReviewersYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: "1.0"
projects:
  - name: "my-app"
    path: "/tmp/my-app"
    reviewers:
      - alice
      - bob
    team_reviewers:
      - backend-team
    github:
      owner: "my-org"
      repo: "my-app"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Projects) != 1 {
		t.Fatalf("Projects count = %d, want 1", len(cfg.Projects))
	}

	proj := cfg.Projects[0]
	if len(proj.Reviewers) != 2 || proj.Reviewers[0] != "alice" || proj.Reviewers[1] != "bob" {
		t.Errorf("Reviewers = %v, want [alice bob]", proj.Reviewers)
	}
	if len(proj.TeamReviewers) != 1 || proj.TeamReviewers[0] != "backend-team" {
		t.Errorf("TeamReviewers = %v, want [backend-team]", proj.TeamReviewers)
	}
}
