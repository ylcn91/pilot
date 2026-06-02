package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckDeprecations(t *testing.T) {
	tests := []struct {
		name         string
		config       *Config
		wantWarnings int
		wantContains string
	}{
		{
			name:         "NoDeprecatedFields",
			config:       DefaultConfig(),
			wantWarnings: 0,
		},
		{
			name: "DeprecatedTimeField",
			config: func() *Config {
				c := DefaultConfig()
				c.Orchestrator.DailyBrief.Time = "09:00"
				return c
			}(),
			wantWarnings: 1,
			wantContains: "daily_brief.time is deprecated",
		},
		{
			name: "DeprecatedTimeFieldWithSchedule",
			config: func() *Config {
				c := DefaultConfig()
				c.Orchestrator.DailyBrief.Time = "09:00"
				c.Orchestrator.DailyBrief.Schedule = "0 9 * * 1-5"
				return c
			}(),
			wantWarnings: 1,
			wantContains: "use schedule",
		},
		{
			name: "NilOrchestrator",
			config: func() *Config {
				c := DefaultConfig()
				c.Orchestrator = nil
				return c
			}(),
			wantWarnings: 0,
		},
		{
			name: "NilDailyBrief",
			config: func() *Config {
				c := DefaultConfig()
				c.Orchestrator.DailyBrief = nil
				return c
			}(),
			wantWarnings: 0,
		},
		{
			name: "EmptyTimeField",
			config: func() *Config {
				c := DefaultConfig()
				c.Orchestrator.DailyBrief.Time = ""
				return c
			}(),
			wantWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.config.CheckDeprecations()
			if len(warnings) != tt.wantWarnings {
				t.Errorf("CheckDeprecations() returned %d warnings, want %d", len(warnings), tt.wantWarnings)
			}
			if tt.wantContains != "" && len(warnings) > 0 {
				found := false
				for _, w := range warnings {
					if contains(w, tt.wantContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("CheckDeprecations() warnings %v should contain %q", warnings, tt.wantContains)
				}
			}
		})
	}
}

func TestLoadWithDeprecatedTimeField(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Config using deprecated time field
	configContent := `
version: "1.0"
orchestrator:
  daily_brief:
    enabled: true
    time: "09:00"
    timezone: "America/New_York"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify the deprecated field was loaded
	if config.Orchestrator.DailyBrief.Time != "09:00" {
		t.Errorf("DailyBrief.Time = %q, want %q", config.Orchestrator.DailyBrief.Time, "09:00")
	}

	// Verify deprecation warning is generated
	warnings := config.CheckDeprecations()
	if len(warnings) != 1 {
		t.Errorf("Expected 1 deprecation warning, got %d", len(warnings))
	}
}

func TestLoadTeamConfig(t *testing.T) {
	t.Run("team config from YAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `
version: "1.0"
team:
  enabled: true
  team_id: "my-team"
  member_email: "dev@example.com"
`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if cfg.Team == nil {
			t.Fatal("Team config should not be nil")
		}
		if !cfg.Team.Enabled {
			t.Error("Team.Enabled should be true")
		}
		if cfg.Team.TeamID != "my-team" {
			t.Errorf("Team.TeamID = %q, want %q", cfg.Team.TeamID, "my-team")
		}
		if cfg.Team.MemberEmail != "dev@example.com" {
			t.Errorf("Team.MemberEmail = %q, want %q", cfg.Team.MemberEmail, "dev@example.com")
		}
	})

	t.Run("team config absent defaults to nil", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: "1.0"`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if cfg.Team != nil {
			t.Errorf("Team config should be nil when not configured, got %+v", cfg.Team)
		}
	})

	t.Run("team config disabled", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `
version: "1.0"
team:
  enabled: false
  team_id: "my-team"
  member_email: "dev@example.com"
`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if cfg.Team == nil {
			t.Fatal("Team config should not be nil")
		}
		if cfg.Team.Enabled {
			t.Error("Team.Enabled should be false")
		}
	})
}
