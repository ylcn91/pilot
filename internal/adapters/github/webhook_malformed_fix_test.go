package github

import (
	"context"
	"testing"
)

// TestExtractIssueAndRepo_MalformedPayloads verifies that extractIssueAndRepo
// returns a descriptive error (and does NOT panic) when webhook payloads are
// partial or contain wrong-typed fields. Before the fix these payloads caused
// unchecked type assertions to panic.
func TestExtractIssueAndRepo_MalformedPayloads(t *testing.T) {
	validRepo := map[string]interface{}{
		"name":      "repo",
		"full_name": "org/repo",
		"html_url":  "https://github.com/org/repo",
		"owner": map[string]interface{}{
			"login": "org",
		},
	}
	validIssueFields := func() map[string]interface{} {
		return map[string]interface{}{
			"number":   float64(42),
			"title":    "Title",
			"state":    "open",
			"html_url": "https://github.com/org/repo/issues/42",
		}
	}

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name: "issue number is nil",
			payload: map[string]interface{}{
				"issue": map[string]interface{}{
					"number":   nil,
					"title":    "Title",
					"state":    "open",
					"html_url": "https://x",
				},
				"repository": validRepo,
			},
		},
		{
			name: "issue number is a string",
			payload: map[string]interface{}{
				"issue": map[string]interface{}{
					"number":   "42",
					"title":    "Title",
					"state":    "open",
					"html_url": "https://x",
				},
				"repository": validRepo,
			},
		},
		{
			name: "issue title missing",
			payload: map[string]interface{}{
				"issue": map[string]interface{}{
					"number":   float64(42),
					"state":    "open",
					"html_url": "https://x",
				},
				"repository": validRepo,
			},
		},
		{
			name: "issue state wrong type",
			payload: map[string]interface{}{
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Title",
					"state":    float64(1),
					"html_url": "https://x",
				},
				"repository": validRepo,
			},
		},
		{
			name: "issue html_url missing",
			payload: map[string]interface{}{
				"issue": map[string]interface{}{
					"number": float64(42),
					"title":  "Title",
					"state":  "open",
				},
				"repository": validRepo,
			},
		},
		{
			name: "label name missing",
			payload: map[string]interface{}{
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Title",
					"state":    "open",
					"html_url": "https://x",
					"labels": []interface{}{
						map[string]interface{}{
							"id": float64(123),
						},
					},
				},
				"repository": validRepo,
			},
		},
		{
			name: "label name wrong type",
			payload: map[string]interface{}{
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Title",
					"state":    "open",
					"html_url": "https://x",
					"labels": []interface{}{
						map[string]interface{}{
							"name": float64(7),
						},
					},
				},
				"repository": validRepo,
			},
		},
		{
			name: "repository name missing",
			payload: map[string]interface{}{
				"issue": validIssueFields(),
				"repository": map[string]interface{}{
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
		},
		{
			name: "repository full_name wrong type",
			payload: map[string]interface{}{
				"issue": validIssueFields(),
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": float64(1),
					"html_url":  "https://github.com/org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
		},
		{
			name: "repository html_url missing",
			payload: map[string]interface{}{
				"issue": validIssueFields(),
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
		},
		{
			name: "repository owner missing",
			payload: map[string]interface{}{
				"issue": validIssueFields(),
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
				},
			},
		},
		{
			name: "repository owner login missing",
			payload: map[string]interface{}{
				"issue": validIssueFields(),
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"owner":     map[string]interface{}{},
				},
			},
		},
		{
			name: "repository owner login wrong type",
			payload: map[string]interface{}{
				"issue": validIssueFields(),
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"owner": map[string]interface{}{
						"login": float64(99),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebhookHandler(nil, "", "pilot")

			// A panic would fail the test by crashing the goroutine; we recover
			// explicitly to convert any panic into a clear assertion failure.
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("extractIssueAndRepo panicked on malformed payload: %v", r)
				}
			}()

			issue, repo, err := h.extractIssueAndRepo(tt.payload)
			if err == nil {
				t.Fatalf("expected error for malformed payload, got issue=%+v repo=%+v", issue, repo)
			}
		})
	}
}

// TestHandlePRReview_MalformedPayloads verifies that handlePRReview returns an
// error (and does NOT panic) when the pull_request/repository objects exist but
// carry missing or wrong-typed fields.
func TestHandlePRReview_MalformedPayloads(t *testing.T) {
	validReview := map[string]interface{}{
		"state": "approved",
		"user": map[string]interface{}{
			"login": "alice",
		},
	}
	validRepo := map[string]interface{}{
		"name":      "repo",
		"full_name": "org/repo",
		"owner": map[string]interface{}{
			"login": "org",
		},
	}

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name: "pull_request number is nil",
			payload: map[string]interface{}{
				"action":       "submitted",
				"pull_request": map[string]interface{}{"number": nil},
				"review":       validReview,
				"repository":   validRepo,
			},
		},
		{
			name: "pull_request number is a string",
			payload: map[string]interface{}{
				"action":       "submitted",
				"pull_request": map[string]interface{}{"number": "99"},
				"review":       validReview,
				"repository":   validRepo,
			},
		},
		{
			name: "repository name missing",
			payload: map[string]interface{}{
				"action":       "submitted",
				"pull_request": map[string]interface{}{"number": float64(99)},
				"review":       validReview,
				"repository": map[string]interface{}{
					"full_name": "org/repo",
					"owner":     map[string]interface{}{"login": "org"},
				},
			},
		},
		{
			name: "repository owner missing",
			payload: map[string]interface{}{
				"action":       "submitted",
				"pull_request": map[string]interface{}{"number": float64(99)},
				"review":       validReview,
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
				},
			},
		},
		{
			name: "repository owner login wrong type",
			payload: map[string]interface{}{
				"action":       "submitted",
				"pull_request": map[string]interface{}{"number": float64(99)},
				"review":       validReview,
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"owner":     map[string]interface{}{"login": float64(1)},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewWebhookHandler(nil, "", "pilot")
			handler.OnPRReview(func(ctx context.Context, prNumber int, action, state, reviewer string, repo *Repository) error {
				return nil
			})

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handlePRReview panicked on malformed payload: %v", r)
				}
			}()

			err := handler.Handle(context.Background(), "pull_request_review", tt.payload)
			if err == nil {
				t.Fatalf("expected error for malformed PR review payload, got nil")
			}
		})
	}
}
