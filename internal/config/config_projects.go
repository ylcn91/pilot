package config

import (
	"fmt"
	"strings"
)

// ProjectConfig holds configuration for a registered project.
type ProjectConfig struct {
	Name          string `yaml:"name"`
	Path          string `yaml:"path"`
	Navigator     bool   `yaml:"navigator"`
	DefaultBranch string `yaml:"default_branch"`
	// BranchFrom is an alias for DefaultBranch. When both are set, BranchFrom wins.
	// Lets users express "branch from (and PR target) this branch" more intuitively
	// in workflows like main → dev → feature, where dev is the integration branch (GH-2290).
	BranchFrom    string               `yaml:"branch_from,omitempty"`
	Reviewers     []string             `yaml:"reviewers,omitempty"`
	TeamReviewers []string             `yaml:"team_reviewers,omitempty"`
	GitHub        *ProjectGitHubConfig `yaml:"github,omitempty"`
	Linear        *ProjectLinearConfig `yaml:"linear,omitempty"`
}

// ResolveBaseBranch returns the branch that Pilot should branch from and
// target for PRs/MRs. BranchFrom takes precedence over DefaultBranch; both
// may be empty (caller must fall back to git's default branch).
func (p *ProjectConfig) ResolveBaseBranch() string {
	if p == nil {
		return ""
	}
	if p.BranchFrom != "" {
		return p.BranchFrom
	}
	return p.DefaultBranch
}

// ProjectGitHubConfig holds GitHub-specific project configuration for PR creation and issue tracking.
type ProjectGitHubConfig struct {
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`
}

// ProjectLinearConfig holds Linear-specific project configuration for project pairing.
type ProjectLinearConfig struct {
	ProjectID string `yaml:"project_id"`
}

// FindProjectByRepo returns the ProjectConfig whose GitHub owner/repo matches
// the given "owner/repo" string, or nil if no match is found.
func (c *Config) FindProjectByRepo(ownerRepo string) *ProjectConfig {
	for _, p := range c.Projects {
		if p.GitHub != nil {
			if fmt.Sprintf("%s/%s", p.GitHub.Owner, p.GitHub.Repo) == ownerRepo {
				return p
			}
		}
	}
	return nil
}

// FindProjectByPath returns the ProjectConfig whose Path matches the given
// absolute path, or nil if no match is found. Used by adapters that don't
// know the source repo (e.g. GitLab) to look up per-project settings like
// the configured default branch (GH-2290).
func (c *Config) FindProjectByPath(path string) *ProjectConfig {
	if path == "" {
		return nil
	}
	for _, p := range c.Projects {
		if p.Path == path {
			return p
		}
	}
	return nil
}

// GetProject returns the project configuration for a given filesystem path.
// Returns nil if no project is configured for that path.
func (c *Config) GetProject(path string) *ProjectConfig {
	for _, project := range c.Projects {
		if project.Path == path {
			return project
		}
	}
	return nil
}

// GetProjectByName returns the project configuration matching the given name.
// The comparison is case-insensitive. Returns nil if no matching project is found.
func (c *Config) GetProjectByName(name string) *ProjectConfig {
	nameLower := strings.ToLower(name)
	for _, project := range c.Projects {
		if strings.ToLower(project.Name) == nameLower {
			return project
		}
	}
	return nil
}

// GetProjectByLinearID returns the project matching a Linear project UUID.
// Returns nil if no project has a matching linear.project_id configured.
func (c *Config) GetProjectByLinearID(linearProjectID string) *ProjectConfig {
	for _, project := range c.Projects {
		if project.Linear != nil && project.Linear.ProjectID == linearProjectID {
			return project
		}
	}
	return nil
}

// GetDefaultProject returns the default project configuration.
// It first checks the DefaultProject setting by name, then falls back to the first project.
// Returns nil if no projects are configured.
func (c *Config) GetDefaultProject() *ProjectConfig {
	if c.DefaultProject != "" {
		if proj := c.GetProjectByName(c.DefaultProject); proj != nil {
			return proj
		}
	}
	if len(c.Projects) > 0 {
		return c.Projects[0]
	}
	return nil
}
