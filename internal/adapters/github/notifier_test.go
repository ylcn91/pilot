package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewNotifier(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	notifier := NewNotifier(client, "pilot")

	if notifier == nil {
		t.Fatal("NewNotifier returned nil")
	}
	if notifier.client != client {
		t.Error("notifier.client not set correctly")
	}
	if notifier.pilotLabel != "pilot" {
		t.Errorf("notifier.pilotLabel = %s, want 'pilot'", notifier.pilotLabel)
	}
}

func TestNotifyTaskStarted(t *testing.T) {
	tests := []struct {
		name          string
		labelStatus   int
		commentStatus int
		wantErr       bool
	}{
		{
			name:          "success",
			labelStatus:   http.StatusOK,
			commentStatus: http.StatusCreated,
			wantErr:       false,
		},
		{
			name:          "label add fails",
			labelStatus:   http.StatusInternalServerError,
			commentStatus: http.StatusCreated,
			wantErr:       true,
		},
		{
			name:          "comment add fails",
			labelStatus:   http.StatusOK,
			commentStatus: http.StatusInternalServerError,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Distinguish between label and comment endpoints
				if strings.HasSuffix(r.URL.Path, "/labels") {
					var body map[string][]string
					_ = json.NewDecoder(r.Body).Decode(&body)

					// Verify in-progress label is being added
					labels := body["labels"]
					found := false
					for _, l := range labels {
						if l == LabelInProgress {
							found = true
							break
						}
					}
					if !found {
						t.Error("expected pilot-in-progress label to be added")
					}

					w.WriteHeader(tt.labelStatus)
				} else if strings.HasSuffix(r.URL.Path, "/comments") {
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)

					// Verify comment contains task ID
					if !strings.Contains(body["body"], "TASK-123") {
						t.Error("comment should contain task ID")
					}
					if !strings.Contains(body["body"], "Pilot started") {
						t.Error("comment should indicate pilot started")
					}

					w.WriteHeader(tt.commentStatus)
					if tt.commentStatus < 300 {
						_ = json.NewEncoder(w).Encode(Comment{ID: 123, Body: body["body"]})
					}
				} else {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			notifier := NewNotifier(client, "pilot")

			err := notifier.NotifyTaskStarted(context.Background(), "owner", "repo", 42, "TASK-123")

			if (err != nil) != tt.wantErr {
				t.Errorf("NotifyTaskStarted() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
