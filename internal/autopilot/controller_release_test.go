package autopilot

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// GH-1566: Test parseAutopilotIteration function.
func TestController_ShouldTriggerRelease(t *testing.T) {
	tests := []struct {
		name          string
		globalRelease *ReleaseConfig
		envRelease    *ReleaseConfig
		envName       string
		want          bool
	}{
		{
			name:          "global only enabled",
			globalRelease: &ReleaseConfig{Enabled: true, Trigger: "on_merge"},
			want:          true,
		},
		{
			name:          "global only disabled",
			globalRelease: &ReleaseConfig{Enabled: false, Trigger: "on_merge"},
			want:          false,
		},
		{
			name:       "env only enabled",
			envRelease: &ReleaseConfig{Enabled: true, Trigger: "on_merge"},
			envName:    "prod",
			want:       true,
		},
		{
			name:       "env only disabled",
			envRelease: &ReleaseConfig{Enabled: false, Trigger: "on_merge"},
			envName:    "prod",
			want:       false,
		},
		{
			name:          "env overrides global - env enabled",
			globalRelease: &ReleaseConfig{Enabled: false, Trigger: "on_merge"},
			envRelease:    &ReleaseConfig{Enabled: true, Trigger: "on_merge"},
			envName:       "prod",
			want:          true,
		},
		{
			name:          "env overrides global - env disabled",
			globalRelease: &ReleaseConfig{Enabled: true, Trigger: "on_merge"},
			envRelease:    &ReleaseConfig{Enabled: false, Trigger: "on_merge"},
			envName:       "prod",
			want:          false,
		},
		{
			name: "both nil",
			want: false,
		},
		{
			name:          "global enabled but trigger manual",
			globalRelease: &ReleaseConfig{Enabled: true, Trigger: "manual"},
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ghClient := github.NewClient(testutil.FakeGitHubToken)
			cfg := DefaultConfig()
			cfg.Release = tt.globalRelease

			if tt.envName != "" && tt.envRelease != nil {
				cfg.Environments[tt.envName] = &EnvironmentConfig{
					Release: tt.envRelease,
				}
				if err := cfg.SetActiveEnvironment(tt.envName); err != nil {
					t.Fatalf("SetActiveEnvironment: %v", err)
				}
			}

			c := NewController(cfg, ghClient, nil, "owner", "repo")
			got := c.shouldTriggerRelease()
			if got != tt.want {
				t.Errorf("shouldTriggerRelease() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestController_ResolvedRelease(t *testing.T) {
	tests := []struct {
		name          string
		globalRelease *ReleaseConfig
		envRelease    *ReleaseConfig
		envName       string
		wantTagPrefix string
		wantNil       bool
	}{
		{
			name:          "env release takes precedence",
			globalRelease: &ReleaseConfig{TagPrefix: "global-v"},
			envRelease:    &ReleaseConfig{TagPrefix: "env-v"},
			envName:       "prod",
			wantTagPrefix: "env-v",
		},
		{
			name:          "falls back to global when env has no release",
			globalRelease: &ReleaseConfig{TagPrefix: "global-v"},
			envName:       "dev",
			wantTagPrefix: "global-v",
		},
		{
			name:    "both nil returns nil",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ghClient := github.NewClient(testutil.FakeGitHubToken)
			cfg := DefaultConfig()
			cfg.Release = tt.globalRelease

			if tt.envName != "" {
				env := cfg.Environments[tt.envName]
				if env == nil {
					env = &EnvironmentConfig{}
					cfg.Environments[tt.envName] = env
				}
				env.Release = tt.envRelease
				if err := cfg.SetActiveEnvironment(tt.envName); err != nil {
					t.Fatalf("SetActiveEnvironment: %v", err)
				}
			}

			c := NewController(cfg, ghClient, nil, "owner", "repo")
			got := c.resolvedRelease()
			if tt.wantNil {
				if got != nil {
					t.Errorf("resolvedRelease() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("resolvedRelease() = nil, want non-nil")
			}
			if got.TagPrefix != tt.wantTagPrefix {
				t.Errorf("resolvedRelease().TagPrefix = %q, want %q", got.TagPrefix, tt.wantTagPrefix)
			}
		})
	}
}

func TestParseAutopilotIteration(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"iteration 1", "<!-- autopilot-meta branch:pilot/GH-10 pr:42 iteration:1 -->", 1},
		{"iteration 3", "<!-- autopilot-meta branch:pilot/GH-10 pr:42 iteration:3 -->", 3},
		{"iteration 0", "<!-- autopilot-meta branch:pilot/GH-10 pr:42 iteration:0 -->", 0},
		{"no iteration", "<!-- autopilot-meta branch:pilot/GH-10 pr:42 -->", 0},
		{"no metadata", "just a normal issue body", 0},
		{"empty body", "", 0},
		{"embedded in body", "# Fix\n\n## Context\nstuff\n\n<!-- autopilot-meta branch:pilot/GH-10 pr:42 iteration:5 -->\n", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAutopilotIteration(tt.body)
			if got != tt.want {
				t.Errorf("parseAutopilotIteration() = %d, want %d", got, tt.want)
			}
		})
	}
}
