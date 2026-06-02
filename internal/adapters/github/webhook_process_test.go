package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestProcessIssue_CallbackError(t *testing.T) {
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

	expectedErr := errors.New("callback error")
	handler.OnIssue(func(ctx context.Context, issue *Issue, repo *Repository) error {
		return expectedErr
	})

	payload := map[string]interface{}{
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
	}

	err := handler.Handle(context.Background(), "issues", payload)

	if err == nil {
		t.Error("expected error from callback")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}
}

func TestProcessIssue_NoCallback(t *testing.T) {
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
	// No callback set

	payload := map[string]interface{}{
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
	}

	err := handler.Handle(context.Background(), "issues", payload)

	if err != nil {
		t.Errorf("Handle() without callback should not error, got %v", err)
	}
}

func TestProcessIssue_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "Internal Server Error"}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	handler := NewWebhookHandler(client, "", "pilot")

	handler.OnIssue(func(ctx context.Context, issue *Issue, repo *Repository) error {
		return nil
	})

	payload := map[string]interface{}{
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
	}

	err := handler.Handle(context.Background(), "issues", payload)

	if err == nil {
		t.Error("expected error from API failure")
	}
}
