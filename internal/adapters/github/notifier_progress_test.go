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

func TestNotifyProgress(t *testing.T) {
	tests := []struct {
		name       string
		phase      string
		details    string
		statusCode int
		wantErr    bool
		wantEmoji  string
	}{
		{
			name:       "exploring phase",
			phase:      "exploring",
			details:    "Analyzing codebase structure",
			statusCode: http.StatusCreated,
			wantErr:    false,
			wantEmoji:  "",
		},
		{
			name:       "research phase",
			phase:      "research",
			details:    "Reading documentation",
			statusCode: http.StatusCreated,
			wantErr:    false,
			wantEmoji:  "",
		},
		{
			name:       "implementing phase",
			phase:      "implementing",
			details:    "Writing new feature code",
			statusCode: http.StatusCreated,
			wantErr:    false,
			wantEmoji:  "",
		},
		{
			name:       "testing phase",
			phase:      "testing",
			details:    "Running test suite",
			statusCode: http.StatusCreated,
			wantErr:    false,
			wantEmoji:  "",
		},
		{
			name:       "committing phase",
			phase:      "committing",
			details:    "Creating commit with changes",
			statusCode: http.StatusCreated,
			wantErr:    false,
			wantEmoji:  "",
		},
		{
			name:       "unknown phase",
			phase:      "unknown",
			details:    "Some other work",
			statusCode: http.StatusCreated,
			wantErr:    false,
			wantEmoji:  "",
		},
		{
			name:       "API error",
			phase:      "testing",
			details:    "Running tests",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
			wantEmoji:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}

				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)

				// Verify comment contains phase and details
				if !strings.Contains(body["body"], tt.phase) {
					t.Errorf("comment should contain phase: %s", tt.phase)
				}
				if !strings.Contains(body["body"], tt.details) {
					t.Errorf("comment should contain details: %s", tt.details)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode < 300 {
					_ = json.NewEncoder(w).Encode(Comment{ID: 123})
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			notifier := NewNotifier(client, "pilot")

			err := notifier.NotifyProgress(context.Background(), "owner", "repo", 42, tt.phase, tt.details)

			if (err != nil) != tt.wantErr {
				t.Errorf("NotifyProgress() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNotifyProgress_PhaseEmojis(t *testing.T) {
	phases := []struct {
		phase string
		emoji string
	}{
		{"exploring", ""},
		{"research", ""},
		{"implementing", ""},
		{"impl", ""},
		{"testing", ""},
		{"verify", ""},
		{"committing", ""},
		{"unknown", ""},
	}

	for _, p := range phases {
		t.Run(p.phase, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)

				// Just verify the phase name is in the comment
				if !strings.Contains(body["body"], p.phase) {
					t.Errorf("comment should contain phase: %s", p.phase)
				}

				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(Comment{ID: 123})
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			notifier := NewNotifier(client, "pilot")

			err := notifier.NotifyProgress(context.Background(), "owner", "repo", 42, p.phase, "details")
			if err != nil {
				t.Errorf("NotifyProgress() error = %v", err)
			}
		})
	}
}
