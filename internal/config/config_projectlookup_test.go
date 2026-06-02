package config

import (
	"testing"
)

func TestGetDefaultProject(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		wantName string
		wantNil  bool
	}{
		{
			name: "DefaultProjectSet",
			config: func() *Config {
				c := DefaultConfig()
				c.Projects = []*ProjectConfig{
					{Name: "first", Path: "/first"},
					{Name: "second", Path: "/second"},
				}
				c.DefaultProject = "second"
				return c
			}(),
			wantName: "second",
			wantNil:  false,
		},
		{
			name: "DefaultProjectCaseInsensitive",
			config: func() *Config {
				c := DefaultConfig()
				c.Projects = []*ProjectConfig{
					{Name: "MyProject", Path: "/myproject"},
				}
				c.DefaultProject = "myproject" // lowercase
				return c
			}(),
			wantName: "MyProject",
			wantNil:  false,
		},
		{
			name: "NoDefaultProjectFallsBackToFirst",
			config: func() *Config {
				c := DefaultConfig()
				c.Projects = []*ProjectConfig{
					{Name: "first", Path: "/first"},
					{Name: "second", Path: "/second"},
				}
				c.DefaultProject = ""
				return c
			}(),
			wantName: "first",
			wantNil:  false,
		},
		{
			name: "DefaultProjectNotFound",
			config: func() *Config {
				c := DefaultConfig()
				c.Projects = []*ProjectConfig{
					{Name: "first", Path: "/first"},
				}
				c.DefaultProject = "nonexistent"
				return c
			}(),
			wantName: "first", // Falls back to first project
			wantNil:  false,
		},
		{
			name: "NoProjects",
			config: func() *Config {
				c := DefaultConfig()
				c.Projects = []*ProjectConfig{}
				c.DefaultProject = ""
				return c
			}(),
			wantNil: true,
		},
		{
			name: "NilProjects",
			config: func() *Config {
				c := DefaultConfig()
				c.Projects = nil
				c.DefaultProject = ""
				return c
			}(),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := tt.config.GetDefaultProject()
			if tt.wantNil {
				if project != nil {
					t.Errorf("GetDefaultProject() = %+v, want nil", project)
				}
			} else {
				if project == nil {
					t.Fatal("GetDefaultProject() = nil, want project")
				}
				if project.Name != tt.wantName {
					t.Errorf("GetDefaultProject().Name = %q, want %q", project.Name, tt.wantName)
				}
			}
		})
	}
}

func TestFindProjectByRepo(t *testing.T) {
	cfg := &Config{
		Projects: []*ProjectConfig{
			{
				Name:          "app-one",
				Reviewers:     []string{"alice", "bob"},
				TeamReviewers: []string{"backend-team"},
				GitHub: &ProjectGitHubConfig{
					Owner: "my-org",
					Repo:  "app-one",
				},
			},
			{
				Name: "app-two",
				GitHub: &ProjectGitHubConfig{
					Owner: "my-org",
					Repo:  "app-two",
				},
			},
			{
				Name: "no-github",
			},
		},
	}

	t.Run("found with reviewers", func(t *testing.T) {
		proj := cfg.FindProjectByRepo("my-org/app-one")
		if proj == nil {
			t.Fatal("expected project, got nil")
		}
		if proj.Name != "app-one" {
			t.Errorf("Name = %s, want app-one", proj.Name)
		}
		if len(proj.Reviewers) != 2 {
			t.Errorf("Reviewers count = %d, want 2", len(proj.Reviewers))
		}
		if len(proj.TeamReviewers) != 1 {
			t.Errorf("TeamReviewers count = %d, want 1", len(proj.TeamReviewers))
		}
	})

	t.Run("found without reviewers", func(t *testing.T) {
		proj := cfg.FindProjectByRepo("my-org/app-two")
		if proj == nil {
			t.Fatal("expected project, got nil")
		}
		if len(proj.Reviewers) != 0 {
			t.Errorf("Reviewers count = %d, want 0", len(proj.Reviewers))
		}
	})

	t.Run("not found", func(t *testing.T) {
		proj := cfg.FindProjectByRepo("other-org/other-repo")
		if proj != nil {
			t.Errorf("expected nil, got %v", proj)
		}
	})

	t.Run("empty config", func(t *testing.T) {
		empty := &Config{}
		proj := empty.FindProjectByRepo("my-org/app-one")
		if proj != nil {
			t.Errorf("expected nil, got %v", proj)
		}
	})
}

func TestFindProjectByPath(t *testing.T) {
	cfg := &Config{
		Projects: []*ProjectConfig{
			{Name: "a", Path: "/tmp/a", DefaultBranch: "dev"},
			{Name: "b", Path: "/tmp/b"},
		},
	}
	if p := cfg.FindProjectByPath("/tmp/a"); p == nil || p.Name != "a" {
		t.Errorf("FindProjectByPath(/tmp/a) = %v, want project a", p)
	}
	if p := cfg.FindProjectByPath("/tmp/missing"); p != nil {
		t.Errorf("FindProjectByPath(missing) = %v, want nil", p)
	}
	if p := cfg.FindProjectByPath(""); p != nil {
		t.Errorf("FindProjectByPath(\"\") = %v, want nil", p)
	}
}
