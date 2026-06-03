package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestHasChangesRequested_FilterLogic exercises the hasChangesRequested filter
// directly across the discriminating cases the existing negative-only tests
// (TestController_HandleReviewRequested_IgnoresSelfReview and
// TestController_HasChangesRequested_FilterByTime) do not cover:
//
//   - a genuine human CHANGES_REQUESTED submitted AFTER PR creation returns true
//     (proves the bot/time filters don't over-filter a real review),
//   - per-user latest-state dedup: when one user requests changes then later
//     approves, the latest APPROVED state wins and the function returns false,
//   - a non-bot user co-existing with a bot CHANGES_REQUESTED still surfaces the
//     human review,
//   - the "-bot" suffix and "[bot]" substring filters each independently exclude
//     automated reviews,
//   - an APPROVED-only PR returns false.
//
// CreatedAt is fixed before all review timestamps so the time filter is a no-op
// and we isolate the bot-filter + latest-state-per-user logic. (gap: direct
// unit test for hasChangesRequested filter logic)
func TestHasChangesRequested_FilterLogic(t *testing.T) {
	prCreatedAt := time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC)
	after := func(min int) string {
		return prCreatedAt.Add(time.Duration(min) * time.Minute).Format(time.RFC3339)
	}

	tests := []struct {
		name    string
		reviews []*github.PullRequestReview
		want    bool
	}{
		{
			name: "human changes_requested after PR creation -> true",
			reviews: []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "alice"}, State: "CHANGES_REQUESTED", SubmittedAt: after(10)},
			},
			want: true,
		},
		{
			name: "same user requests changes then approves -> latest approved wins -> false",
			reviews: []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "alice"}, State: "CHANGES_REQUESTED", SubmittedAt: after(10)},
				{ID: 2, User: github.User{Login: "alice"}, State: "APPROVED", SubmittedAt: after(20)},
			},
			want: false,
		},
		{
			name: "human changes_requested alongside bot changes_requested -> true",
			reviews: []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "ci-bot"}, State: "CHANGES_REQUESTED", SubmittedAt: after(10)},
				{ID: 2, User: github.User{Login: "bob"}, State: "CHANGES_REQUESTED", SubmittedAt: after(11)},
			},
			want: true,
		},
		{
			name: "only [bot]-substring reviewer requests changes -> false",
			reviews: []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "pilot[bot]"}, State: "CHANGES_REQUESTED", SubmittedAt: after(10)},
			},
			want: false,
		},
		{
			name: "only -bot-suffix reviewer requests changes -> false",
			reviews: []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "release-bot"}, State: "CHANGES_REQUESTED", SubmittedAt: after(10)},
			},
			want: false,
		},
		{
			name: "approved-only human review -> false",
			reviews: []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "alice"}, State: "APPROVED", SubmittedAt: after(10)},
			},
			want: false,
		},
		{
			name: "two distinct humans, one still requesting changes -> true",
			reviews: []*github.PullRequestReview{
				{ID: 1, User: github.User{Login: "alice"}, State: "APPROVED", SubmittedAt: after(10)},
				{ID: 2, User: github.User{Login: "bob"}, State: "CHANGES_REQUESTED", SubmittedAt: after(11)},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/pulls/42/reviews" {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(mustJSON(t, tt.reviews))
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.ReviewFeedback = &ReviewFeedbackConfig{Enabled: true, MaxIterations: 3}

			c := NewController(cfg, ghClient, nil, "owner", "repo")
			c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

			c.mu.Lock()
			c.activePRs[42].CreatedAt = prCreatedAt
			c.mu.Unlock()

			prState, _ := c.GetPRState(42)
			if got := c.hasChangesRequested(context.Background(), prState); got != tt.want {
				t.Errorf("hasChangesRequested() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHasChangesRequested_FetchErrorFailsOpen proves the fail-open contract: when
// the reviews API errors, hasChangesRequested must return false (do not block the
// merge path on a transient list-reviews failure) rather than panicking or
// treating the missing data as a changes-requested signal.
func TestHasChangesRequested_FetchErrorFailsOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.ReviewFeedback = &ReviewFeedbackConfig{Enabled: true, MaxIterations: 3}

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	prState, _ := c.GetPRState(42)
	if c.hasChangesRequested(context.Background(), prState) {
		t.Error("hasChangesRequested must fail open (return false) when fetching reviews errors")
	}
}
