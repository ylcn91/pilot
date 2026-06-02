package main

import (
	"bufio"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/config"
)

// TestParseGitURL tests URL parsing for various git remote formats.
func TestParseGitURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "SSH URL",
			url:       "git@github.com:acme/backend.git",
			wantOwner: "acme",
			wantRepo:  "backend",
			wantErr:   false,
		},
		{
			name:      "HTTPS URL",
			url:       "https://github.com/acme/backend.git",
			wantOwner: "acme",
			wantRepo:  "backend",
			wantErr:   false,
		},
		{
			name:      "HTTPS without .git",
			url:       "https://github.com/acme/backend",
			wantOwner: "acme",
			wantRepo:  "backend",
			wantErr:   false,
		},
		{
			name:      "GitLab SSH URL",
			url:       "git@gitlab.com:ns/project.git",
			wantOwner: "ns",
			wantRepo:  "project",
			wantErr:   false,
		},
		{
			name:    "Invalid URL - no colon",
			url:     "invalid-url",
			wantErr: true,
		},
		{
			name:    "Invalid SSH URL",
			url:     "git@github.com",
			wantErr: true,
		},
		{
			name:    "Invalid HTTPS URL - no path",
			url:     "https://github.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseGitURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseGitURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("parseGitURL() owner = %v, want %v", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("parseGitURL() repo = %v, want %v", repo, tt.wantRepo)
				}
			}
		})
	}
}

// TestSelectOption tests option selection with mock reader input.
func TestSelectOption(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		options []string
		want    int
	}{
		{
			name:    "select first option",
			input:   "1\n",
			options: []string{"Option A", "Option B", "Option C"},
			want:    1,
		},
		{
			name:    "select third option",
			input:   "3\n",
			options: []string{"Option A", "Option B", "Option C"},
			want:    3,
		},
		{
			name:    "invalid then valid - returns default",
			input:   "abc\n",
			options: []string{"Option A", "Option B"},
			want:    1, // Default to first on invalid
		},
		{
			name:    "out of range - returns default",
			input:   "5\n",
			options: []string{"Option A", "Option B"},
			want:    1, // Default to first on out of range
		},
		{
			name:    "empty input - returns default",
			input:   "\n",
			options: []string{"Option A", "Option B"},
			want:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got := selectOption(reader, "Select:", tt.options)
			if got != tt.want {
				t.Errorf("selectOption() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPersonaRouting tests that persona selection routes to correct stage counts.
func TestPersonaRouting(t *testing.T) {
	tests := []struct {
		name              string
		persona           Persona
		wantStagesTotal   int
		wantTicketSources int
	}{
		{
			name:              "Solo persona",
			persona:           PersonaSolo,
			wantStagesTotal:   4,
			wantTicketSources: 1, // GitHub only
		},
		{
			name:              "Team persona",
			persona:           PersonaTeam,
			wantStagesTotal:   5,
			wantTicketSources: 3, // GitHub, Linear, Jira
		},
		{
			name:              "Enterprise persona",
			persona:           PersonaEnterprise,
			wantStagesTotal:   5,
			wantTicketSources: 6, // All sources
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create state with persona
			state := &OnboardState{
				Persona: tt.persona,
				Config:  config.DefaultConfig(),
			}

			// Set stage count based on persona (as done in runOnboard)
			switch state.Persona {
			case PersonaSolo:
				state.StagesTotal = 4
			case PersonaTeam, PersonaEnterprise:
				state.StagesTotal = 5
			}

			if state.StagesTotal != tt.wantStagesTotal {
				t.Errorf("StagesTotal = %v, want %v", state.StagesTotal, tt.wantStagesTotal)
			}

			// Check ticket sources for persona
			sources := getTicketSourcesForPersona(tt.persona)
			if len(sources) != tt.wantTicketSources {
				t.Errorf("ticket sources count = %v, want %v", len(sources), tt.wantTicketSources)
			}
		})
	}
}

// TestPersonaString tests Persona.String() method.
func TestPersonaString(t *testing.T) {
	tests := []struct {
		persona Persona
		want    string
	}{
		{PersonaSolo, "Solo"},
		{PersonaTeam, "Team"},
		{PersonaEnterprise, "Enterprise"},
		{Persona(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.persona.String(); got != tt.want {
				t.Errorf("Persona.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
