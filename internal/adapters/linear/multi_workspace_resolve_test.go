package linear

import (
	"testing"
)

func TestMultiWorkspaceHandler_extractTeamID(t *testing.T) {
	handler := &MultiWorkspaceHandler{}

	tests := []struct {
		name    string
		payload map[string]interface{}
		want    string
	}{
		{
			name: "team.id in data",
			payload: map[string]interface{}{
				"data": map[string]interface{}{
					"team": map[string]interface{}{
						"id": "TEAM123",
					},
				},
			},
			want: "TEAM123",
		},
		{
			name: "teamId in data",
			payload: map[string]interface{}{
				"data": map[string]interface{}{
					"teamId": "TEAM456",
				},
			},
			want: "TEAM456",
		},
		{
			name: "no team info",
			payload: map[string]interface{}{
				"data": map[string]interface{}{
					"id": "issue-123",
				},
			},
			want: "",
		},
		{
			name:    "empty payload",
			payload: map[string]interface{}{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.extractTeamID(tt.payload)
			if got != tt.want {
				t.Errorf("extractTeamID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkspaceHandler_ResolvePilotProject(t *testing.T) {
	tests := []struct {
		name       string
		wsConfig   *WorkspaceConfig
		issue      *Issue
		wantResult string
	}{
		{
			name: "single project mapped",
			wsConfig: &WorkspaceConfig{
				Projects: []string{"pilot"},
			},
			issue:      &Issue{},
			wantResult: "pilot",
		},
		{
			name: "multiple projects - returns first",
			wsConfig: &WorkspaceConfig{
				Projects: []string{"aso-generator", "pilot"},
			},
			issue:      &Issue{},
			wantResult: "aso-generator",
		},
		{
			name: "match by project ID",
			wsConfig: &WorkspaceConfig{
				ProjectIDs: []string{"proj-abc"},
				Projects:   []string{"matched-project"},
			},
			issue: &Issue{
				Project: &Project{ID: "proj-abc", Name: "Linear Project"},
			},
			wantResult: "matched-project",
		},
		{
			name: "no projects mapped",
			wsConfig: &WorkspaceConfig{
				Projects: []string{},
			},
			issue:      &Issue{},
			wantResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &WorkspaceHandler{config: tt.wsConfig}
			got := ws.ResolvePilotProject(tt.issue)
			if got != tt.wantResult {
				t.Errorf("ResolvePilotProject() = %q, want %q", got, tt.wantResult)
			}
		})
	}
}
