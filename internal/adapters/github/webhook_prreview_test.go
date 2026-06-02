package github

import (
	"context"
	"errors"
	"testing"
)

func TestHandlePRReview(t *testing.T) {
	tests := []struct {
		name         string
		payload      map[string]interface{}
		wantCalled   bool
		wantPR       int
		wantAction   string
		wantState    string
		wantReviewer string
		wantRepo     string
		wantErr      bool
	}{
		{
			name: "submitted review with changes_requested",
			payload: map[string]interface{}{
				"action": "submitted",
				"pull_request": map[string]interface{}{
					"number": float64(99),
				},
				"review": map[string]interface{}{
					"state": "changes_requested",
					"user": map[string]interface{}{
						"login": "alice",
					},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			wantCalled:   true,
			wantPR:       99,
			wantAction:   "submitted",
			wantState:    "changes_requested",
			wantReviewer: "alice",
			wantRepo:     "org/repo",
			wantErr:      false,
		},
		{
			name: "submitted review with approved",
			payload: map[string]interface{}{
				"action": "submitted",
				"pull_request": map[string]interface{}{
					"number": float64(50),
				},
				"review": map[string]interface{}{
					"state": "approved",
					"user": map[string]interface{}{
						"login": "bob",
					},
				},
				"repository": map[string]interface{}{
					"name":      "myrepo",
					"full_name": "team/myrepo",
					"owner": map[string]interface{}{
						"login": "team",
					},
				},
			},
			wantCalled:   true,
			wantPR:       50,
			wantAction:   "submitted",
			wantState:    "approved",
			wantReviewer: "bob",
			wantRepo:     "team/myrepo",
			wantErr:      false,
		},
		{
			name: "dismissed review",
			payload: map[string]interface{}{
				"action": "dismissed",
				"pull_request": map[string]interface{}{
					"number": float64(77),
				},
				"review": map[string]interface{}{
					"state": "dismissed",
					"user": map[string]interface{}{
						"login": "charlie",
					},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			wantCalled:   true,
			wantPR:       77,
			wantAction:   "dismissed",
			wantState:    "dismissed",
			wantReviewer: "charlie",
			wantRepo:     "org/repo",
			wantErr:      false,
		},
		{
			name: "missing pull_request data - no callback",
			payload: map[string]interface{}{
				"action": "submitted",
				"review": map[string]interface{}{
					"state": "approved",
					"user": map[string]interface{}{
						"login": "alice",
					},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			wantCalled: false,
			wantErr:    false,
		},
		{
			name: "missing review data - no callback",
			payload: map[string]interface{}{
				"action": "submitted",
				"pull_request": map[string]interface{}{
					"number": float64(99),
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			wantCalled: false,
			wantErr:    false,
		},
		{
			name: "missing repository data - no callback",
			payload: map[string]interface{}{
				"action": "submitted",
				"pull_request": map[string]interface{}{
					"number": float64(99),
				},
				"review": map[string]interface{}{
					"state": "approved",
					"user": map[string]interface{}{
						"login": "alice",
					},
				},
			},
			wantCalled: false,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewWebhookHandler(nil, "", "pilot")

			var called bool
			var gotPR int
			var gotAction, gotState, gotReviewer, gotRepo string

			handler.OnPRReview(func(ctx context.Context, prNumber int, action, state, reviewer string, repo *Repository) error {
				called = true
				gotPR = prNumber
				gotAction = action
				gotState = state
				gotReviewer = reviewer
				gotRepo = repo.FullName
				return nil
			})

			err := handler.Handle(context.Background(), "pull_request_review", tt.payload)

			if (err != nil) != tt.wantErr {
				t.Errorf("Handle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if called != tt.wantCalled {
				t.Errorf("callback called = %v, want %v", called, tt.wantCalled)
			}
			if tt.wantCalled {
				if gotPR != tt.wantPR {
					t.Errorf("prNumber = %d, want %d", gotPR, tt.wantPR)
				}
				if gotAction != tt.wantAction {
					t.Errorf("action = %q, want %q", gotAction, tt.wantAction)
				}
				if gotState != tt.wantState {
					t.Errorf("state = %q, want %q", gotState, tt.wantState)
				}
				if gotReviewer != tt.wantReviewer {
					t.Errorf("reviewer = %q, want %q", gotReviewer, tt.wantReviewer)
				}
				if gotRepo != tt.wantRepo {
					t.Errorf("repo = %q, want %q", gotRepo, tt.wantRepo)
				}
			}
		})
	}
}

func TestHandlePRReview_NoCallback(t *testing.T) {
	handler := NewWebhookHandler(nil, "", "pilot")
	// No OnPRReview callback set

	payload := map[string]interface{}{
		"action": "submitted",
		"pull_request": map[string]interface{}{
			"number": float64(99),
		},
		"review": map[string]interface{}{
			"state": "approved",
			"user": map[string]interface{}{
				"login": "alice",
			},
		},
		"repository": map[string]interface{}{
			"name":      "repo",
			"full_name": "org/repo",
			"owner": map[string]interface{}{
				"login": "org",
			},
		},
	}

	err := handler.Handle(context.Background(), "pull_request_review", payload)
	if err != nil {
		t.Errorf("Handle() without callback should not error, got %v", err)
	}
}

func TestHandlePRReview_CallbackError(t *testing.T) {
	handler := NewWebhookHandler(nil, "", "pilot")

	expectedErr := errors.New("review callback error")
	handler.OnPRReview(func(ctx context.Context, prNumber int, action, state, reviewer string, repo *Repository) error {
		return expectedErr
	})

	payload := map[string]interface{}{
		"action": "submitted",
		"pull_request": map[string]interface{}{
			"number": float64(99),
		},
		"review": map[string]interface{}{
			"state": "approved",
			"user": map[string]interface{}{
				"login": "alice",
			},
		},
		"repository": map[string]interface{}{
			"name":      "repo",
			"full_name": "org/repo",
			"owner": map[string]interface{}{
				"login": "org",
			},
		},
	}

	err := handler.Handle(context.Background(), "pull_request_review", payload)
	if err == nil {
		t.Error("expected error from callback")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}
}
