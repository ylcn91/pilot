package config

import (
	"strings"
	"testing"
)

func TestConfig_Validate_Projects(t *testing.T) {
	tests := []struct {
		name           string
		projects       []*ProjectConfig
		defaultProject string
		wantErr        bool
		errSubstr      string
	}{
		{
			name: "valid projects",
			projects: []*ProjectConfig{
				{Name: "pilot", Path: "/home/user/pilot"},
			},
			defaultProject: "pilot",
			wantErr:        false,
		},
		{
			name:           "no projects is allowed",
			projects:       nil,
			defaultProject: "",
			wantErr:        false,
		},
		{
			name: "default project not found",
			projects: []*ProjectConfig{
				{Name: "pilot", Path: "/home/user/pilot"},
			},
			defaultProject: "other",
			wantErr:        true,
			errSubstr:      "default_project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Projects = tt.projects
			cfg.DefaultProject = tt.defaultProject

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
