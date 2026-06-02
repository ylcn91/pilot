package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestHandle(t *testing.T) {
	tests := []struct {
		name        string
		eventType   string
		payload     map[string]interface{}
		wantProcess bool
		wantErr     bool
	}{
		{
			name:        "non-issue event",
			eventType:   "push",
			payload:     map[string]interface{}{"action": "push"},
			wantProcess: false,
			wantErr:     false,
		},
		{
			name:        "pull_request event - ignored",
			eventType:   "pull_request",
			payload:     map[string]interface{}{"action": "opened"},
			wantProcess: false,
			wantErr:     false,
		},
		{
			name:      "issues event - edited action - ignored",
			eventType: "issues",
			payload: map[string]interface{}{
				"action": "edited",
				"issue": map[string]interface{}{
					"number": float64(42),
				},
			},
			wantProcess: false,
			wantErr:     false,
		},
		{
			name:      "issues event - closed action - ignored",
			eventType: "issues",
			payload: map[string]interface{}{
				"action": "closed",
				"issue": map[string]interface{}{
					"number": float64(42),
				},
			},
			wantProcess: false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewWebhookHandler(nil, "", "pilot")

			processed := false
			handler.OnIssue(func(ctx context.Context, issue *Issue, repo *Repository) error {
				processed = true
				return nil
			})

			err := handler.Handle(context.Background(), tt.eventType, tt.payload)

			if (err != nil) != tt.wantErr {
				t.Errorf("Handle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if processed != tt.wantProcess {
				t.Errorf("processed = %v, want %v", processed, tt.wantProcess)
			}
		})
	}
}

func TestHandleIssueOpened(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]interface{}
		hasPilot    bool
		wantProcess bool
		wantErr     bool
	}{
		{
			name: "issue with pilot label",
			payload: map[string]interface{}{
				"action": "opened",
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Test Issue",
					"body":     "Issue body",
					"state":    "open",
					"html_url": "https://github.com/org/repo/issues/42",
					"labels": []interface{}{
						map[string]interface{}{
							"id":   float64(123),
							"name": "pilot",
						},
					},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			hasPilot:    true,
			wantProcess: true,
			wantErr:     false,
		},
		{
			name: "issue without pilot label",
			payload: map[string]interface{}{
				"action": "opened",
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Test Issue",
					"body":     "Issue body",
					"state":    "open",
					"html_url": "https://github.com/org/repo/issues/42",
					"labels": []interface{}{
						map[string]interface{}{
							"id":   float64(123),
							"name": "bug",
						},
					},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			hasPilot:    false,
			wantProcess: false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock server to return issue details
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				issue := Issue{
					Number:  42,
					Title:   "Test Issue",
					Body:    "Issue body",
					State:   "open",
					HTMLURL: "https://github.com/org/repo/issues/42",
				}
				if tt.hasPilot {
					issue.Labels = []Label{{Name: "pilot"}}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(issue)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			handler := NewWebhookHandler(client, "", "pilot")

			processed := false
			handler.OnIssue(func(ctx context.Context, issue *Issue, repo *Repository) error {
				processed = true
				return nil
			})

			err := handler.Handle(context.Background(), "issues", tt.payload)

			if (err != nil) != tt.wantErr {
				t.Errorf("Handle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if processed != tt.wantProcess {
				t.Errorf("processed = %v, want %v", processed, tt.wantProcess)
			}
		})
	}
}

func TestHandleIssueLabeled(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]interface{}
		wantProcess bool
		wantErr     bool
	}{
		{
			name: "labeled with pilot label",
			payload: map[string]interface{}{
				"action": "labeled",
				"label": map[string]interface{}{
					"id":   float64(456),
					"name": "pilot",
				},
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Test Issue",
					"body":     "Issue body",
					"state":    "open",
					"html_url": "https://github.com/org/repo/issues/42",
					"labels": []interface{}{
						map[string]interface{}{
							"id":   float64(456),
							"name": "pilot",
						},
					},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			wantProcess: true,
			wantErr:     false,
		},
		{
			name: "labeled with non-pilot label",
			payload: map[string]interface{}{
				"action": "labeled",
				"label": map[string]interface{}{
					"id":   float64(789),
					"name": "bug",
				},
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Test Issue",
					"body":     "Issue body",
					"state":    "open",
					"html_url": "https://github.com/org/repo/issues/42",
					"labels": []interface{}{
						map[string]interface{}{
							"id":   float64(789),
							"name": "bug",
						},
					},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			wantProcess: false,
			wantErr:     false,
		},
		{
			name: "labeled event missing label data",
			payload: map[string]interface{}{
				"action": "labeled",
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Test Issue",
					"state":    "open",
					"html_url": "https://github.com/org/repo/issues/42",
					"labels":   []interface{}{},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			wantProcess: false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock server to return issue details
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				issue := Issue{
					Number:  42,
					Title:   "Test Issue",
					Body:    "Issue body",
					State:   "open",
					HTMLURL: "https://github.com/org/repo/issues/42",
					Labels:  []Label{{Name: "pilot"}},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(issue)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			handler := NewWebhookHandler(client, "", "pilot")

			processed := false
			handler.OnIssue(func(ctx context.Context, issue *Issue, repo *Repository) error {
				processed = true
				return nil
			})

			err := handler.Handle(context.Background(), "issues", tt.payload)

			if (err != nil) != tt.wantErr {
				t.Errorf("Handle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if processed != tt.wantProcess {
				t.Errorf("processed = %v, want %v", processed, tt.wantProcess)
			}
		})
	}
}

func TestHandleIssueLabeled_CustomLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issue := Issue{
			Number:  42,
			Title:   "Test Issue",
			Body:    "Issue body",
			State:   "open",
			HTMLURL: "https://github.com/org/repo/issues/42",
			Labels:  []Label{{Name: "ai-assist"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issue)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	handler := NewWebhookHandler(client, "", "ai-assist") // Custom pilot label

	processed := false
	handler.OnIssue(func(ctx context.Context, issue *Issue, repo *Repository) error {
		processed = true
		return nil
	})

	payload := map[string]interface{}{
		"action": "labeled",
		"label": map[string]interface{}{
			"id":   float64(456),
			"name": "ai-assist",
		},
		"issue": map[string]interface{}{
			"number":   float64(42),
			"title":    "Test Issue",
			"body":     "Issue body",
			"state":    "open",
			"html_url": "https://github.com/org/repo/issues/42",
			"labels": []interface{}{
				map[string]interface{}{
					"id":   float64(456),
					"name": "ai-assist",
				},
			},
		},
		"repository": map[string]interface{}{
			"name":      "repo",
			"full_name": "org/repo",
			"html_url":  "https://github.com/org/repo",
			"owner": map[string]interface{}{
				"login": "org",
			},
		},
	}

	err := handler.Handle(context.Background(), "issues", payload)

	if err != nil {
		t.Errorf("Handle() error = %v", err)
	}
	if !processed {
		t.Error("expected issue to be processed with custom label")
	}
}
