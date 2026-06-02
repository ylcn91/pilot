package plane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestListStates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/workspaces/ws/projects/proj-1/states/"
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path: got %s, want %s", r.URL.Path, wantPath)
		}
		resp := statesResponse{Results: []State{
			{ID: "s-1", Name: "Backlog", Group: StateGroupBacklog},
			{ID: "s-2", Name: "In Progress", Group: StateGroupStarted},
			{ID: "s-3", Name: "Done", Group: StateGroupCompleted},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testutil.FakePlaneAPIKey)
	states, err := c.ListStates(context.Background(), "ws", "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(states) != 3 {
		t.Errorf("expected 3 states, got %d", len(states))
	}
	if states[0].Group != StateGroupBacklog {
		t.Errorf("expected first state group backlog, got %s", states[0].Group)
	}
}

func TestListLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/workspaces/ws/projects/proj-1/labels/"
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path: got %s, want %s", r.URL.Path, wantPath)
		}
		resp := labelsResponse{Results: []Label{
			{ID: "lbl-1", Name: "pilot", Color: "#ff0000"},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testutil.FakePlaneAPIKey)
	labels, err := c.ListLabels(context.Background(), "ws", "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(labels) != 1 {
		t.Errorf("expected 1 label, got %d", len(labels))
	}
	if labels[0].Name != "pilot" {
		t.Errorf("expected label name pilot, got %s", labels[0].Name)
	}
}

func TestPriorityName(t *testing.T) {
	tests := []struct {
		p    Priority
		want string
	}{
		{PriorityNone, "None"},
		{PriorityUrgent, "Urgent"},
		{PriorityHigh, "High"},
		{PriorityMedium, "Medium"},
		{PriorityLow, "Low"},
	}
	for _, tt := range tests {
		got := PriorityName(tt.p)
		if got != tt.want {
			t.Errorf("PriorityName(%d) = %s, want %s", tt.p, got, tt.want)
		}
	}
}

func TestUpdateIssueState(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testutil.FakePlaneAPIKey)
	err := c.UpdateIssueState(context.Background(), "ws", "proj-1", "wi-42", "state-started-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["state"] != "state-started-uuid" {
		t.Errorf("expected state field 'state-started-uuid', got %v", gotBody["state"])
	}
}

func TestResolveStateByGroup(t *testing.T) {
	tests := []struct {
		name      string
		group     StateGroup
		states    []State
		wantID    string
		wantEmpty bool
	}{
		{
			name:  "finds started state",
			group: StateGroupStarted,
			states: []State{
				{ID: "s-1", Name: "Backlog", Group: StateGroupBacklog},
				{ID: "s-2", Name: "In Progress", Group: StateGroupStarted},
				{ID: "s-3", Name: "Done", Group: StateGroupCompleted},
			},
			wantID: "s-2",
		},
		{
			name:  "finds completed state",
			group: StateGroupCompleted,
			states: []State{
				{ID: "s-1", Name: "Backlog", Group: StateGroupBacklog},
				{ID: "s-3", Name: "Done", Group: StateGroupCompleted},
			},
			wantID: "s-3",
		},
		{
			name:  "returns first match when multiple in same group",
			group: StateGroupStarted,
			states: []State{
				{ID: "s-2a", Name: "In Progress", Group: StateGroupStarted},
				{ID: "s-2b", Name: "In Review", Group: StateGroupStarted},
			},
			wantID: "s-2a",
		},
		{
			name:  "returns empty when no match",
			group: StateGroupCancelled,
			states: []State{
				{ID: "s-1", Name: "Backlog", Group: StateGroupBacklog},
			},
			wantID:    "",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := statesResponse{Results: tt.states}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, testutil.FakePlaneAPIKey)
			id, err := c.ResolveStateByGroup(context.Background(), "ws", "proj-1", tt.group)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tt.wantID {
				t.Errorf("got state ID %q, want %q", id, tt.wantID)
			}
		})
	}
}
