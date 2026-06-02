package config

import (
	"testing"
)

func TestGetProject(t *testing.T) {
	config := DefaultConfig()
	config.Projects = []*ProjectConfig{
		{Name: "project1", Path: "/path/to/project1"},
		{Name: "project2", Path: "/path/to/project2"},
		{Name: "project3", Path: "/path/to/project3"},
	}

	tests := []struct {
		name     string
		path     string
		wantName string
		wantNil  bool
	}{
		{
			name:     "ExistingProject",
			path:     "/path/to/project1",
			wantName: "project1",
			wantNil:  false,
		},
		{
			name:     "SecondProject",
			path:     "/path/to/project2",
			wantName: "project2",
			wantNil:  false,
		},
		{
			name:    "NonexistentProject",
			path:    "/path/to/nonexistent",
			wantNil: true,
		},
		{
			name:    "EmptyPath",
			path:    "",
			wantNil: true,
		},
		{
			name:    "PartialPathMatch",
			path:    "/path/to/project", // Should not match project1
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := config.GetProject(tt.path)
			if tt.wantNil {
				if project != nil {
					t.Errorf("GetProject(%q) = %+v, want nil", tt.path, project)
				}
			} else {
				if project == nil {
					t.Fatalf("GetProject(%q) = nil, want project", tt.path)
				}
				if project.Name != tt.wantName {
					t.Errorf("GetProject(%q).Name = %q, want %q", tt.path, project.Name, tt.wantName)
				}
			}
		})
	}
}

func TestGetProjectByName(t *testing.T) {
	config := DefaultConfig()
	config.Projects = []*ProjectConfig{
		{Name: "MyProject", Path: "/path/to/myproject"},
		{Name: "Another-Project", Path: "/path/to/another"},
		{Name: "UPPERCASE", Path: "/path/to/upper"},
	}

	tests := []struct {
		name     string
		projName string
		wantPath string
		wantNil  bool
	}{
		{
			name:     "ExactMatch",
			projName: "MyProject",
			wantPath: "/path/to/myproject",
			wantNil:  false,
		},
		{
			name:     "LowercaseMatch",
			projName: "myproject",
			wantPath: "/path/to/myproject",
			wantNil:  false,
		},
		{
			name:     "UppercaseMatch",
			projName: "MYPROJECT",
			wantPath: "/path/to/myproject",
			wantNil:  false,
		},
		{
			name:     "MixedCaseMatch",
			projName: "uppercase",
			wantPath: "/path/to/upper",
			wantNil:  false,
		},
		{
			name:     "HyphenatedName",
			projName: "another-project",
			wantPath: "/path/to/another",
			wantNil:  false,
		},
		{
			name:     "NonexistentProject",
			projName: "nonexistent",
			wantNil:  true,
		},
		{
			name:     "EmptyName",
			projName: "",
			wantNil:  true,
		},
		{
			name:     "PartialNameMatch",
			projName: "My", // Should not match MyProject
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := config.GetProjectByName(tt.projName)
			if tt.wantNil {
				if project != nil {
					t.Errorf("GetProjectByName(%q) = %+v, want nil", tt.projName, project)
				}
			} else {
				if project == nil {
					t.Fatalf("GetProjectByName(%q) = nil, want project", tt.projName)
				}
				if project.Path != tt.wantPath {
					t.Errorf("GetProjectByName(%q).Path = %q, want %q", tt.projName, project.Path, tt.wantPath)
				}
			}
		})
	}
}

func TestGetProjectByLinearID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Projects = []*ProjectConfig{
		{Name: "aso-generator", Path: "/path/to/aso", Linear: &ProjectLinearConfig{ProjectID: "proj-abc123"}},
		{Name: "pilot", Path: "/path/to/pilot", Linear: &ProjectLinearConfig{ProjectID: "proj-def456"}},
		{Name: "no-linear", Path: "/path/to/other"},
	}

	tests := []struct {
		name     string
		linearID string
		wantPath string
		wantNil  bool
	}{
		{
			name:     "match first project",
			linearID: "proj-abc123",
			wantPath: "/path/to/aso",
		},
		{
			name:     "match second project",
			linearID: "proj-def456",
			wantPath: "/path/to/pilot",
		},
		{
			name:     "no match",
			linearID: "proj-unknown",
			wantNil:  true,
		},
		{
			name:     "empty ID",
			linearID: "",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := cfg.GetProjectByLinearID(tt.linearID)
			if tt.wantNil {
				if project != nil {
					t.Errorf("GetProjectByLinearID(%q) = %+v, want nil", tt.linearID, project)
				}
			} else {
				if project == nil {
					t.Fatalf("GetProjectByLinearID(%q) = nil, want project", tt.linearID)
				}
				if project.Path != tt.wantPath {
					t.Errorf("GetProjectByLinearID(%q).Path = %q, want %q", tt.linearID, project.Path, tt.wantPath)
				}
			}
		})
	}
}
