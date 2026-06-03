package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestHandleReleasing_ExistingTagSkipsRelease covers the primary TASK-316
// duplicate-tag race guard: when two PRs merge simultaneously, the first
// release tags the commit, and the second PR's handleReleasing sees that its
// HEAD SHA is ALREADY tagged via GetTagForSHA. It must skip the release
// entirely — never calling CreateTagForRepo (POST /git/refs), never fetching
// PR commits for bump detection — and drain the PR from tracking, returning nil.
//
// This is distinct from the two existing cases in controller_releasing_test.go:
//   - GetTagForSHAError: the LOOKUP fails (transient) — retry path.
//   - DuplicateTagTreatedAsReleased: the lookup sees nothing, but the CREATE
//     races and returns "Reference already exists" — create-side recovery.
//
// Here the lookup itself returns a non-empty tag, so the guard short-circuits
// before any create attempt. (TASK-316, path 0)
func TestHandleReleasing_ExistingTagSkipsRelease(t *testing.T) {
	const headSHA = "feedface"

	var (
		createCalled  bool
		commitsCalled bool
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/tags"):
			// GetTagForSHA sees an existing tag pointing at this PR's HEAD SHA:
			// a racing release already tagged this commit.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"v2.0.0","commit":{"sha":"` + headSHA + `"}}]`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/pulls/") && strings.HasSuffix(r.URL.Path, "/commits"):
			commitsCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs"):
			createCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	c := newReleasingController(t, server.URL)
	prState := &PRState{PRNumber: 102, HeadSHA: headSHA, Stage: StageReleasing}
	c.mu.Lock()
	c.activePRs[102] = prState
	c.mu.Unlock()

	err := c.handleReleasing(context.Background(), prState)
	if err != nil {
		t.Fatalf("existing tag should be treated as already-released (nil error), got: %v", err)
	}
	if createCalled {
		t.Error("CreateTagForRepo must NOT be called when the commit is already tagged")
	}
	if commitsCalled {
		t.Error("bump detection (GetPRCommits) must be skipped once an existing tag is found")
	}
	c.mu.RLock()
	_, stillTracked := c.activePRs[102]
	c.mu.RUnlock()
	if stillTracked {
		t.Error("PR must be drained from tracking once its commit is confirmed tagged")
	}
}

// TestHandleReleasing_NilReleaser proves the early no-op guard: when the
// releaser is unconfigured (auto-release disabled), handleReleasing drains the
// PR and returns nil without touching GitHub. This complements the duplicate-tag
// guard tests by exercising the branch above the GetTagForSHA lookup.
func TestHandleReleasing_NilReleaser(t *testing.T) {
	var anyGHCall bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		anyGHCall = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	// No Release config => NewController leaves c.releaser nil.
	c := NewController(cfg, ghClient, nil, "owner", "repo")
	if c.releaser != nil {
		t.Fatal("precondition: releaser must be nil when auto-release is unconfigured")
	}

	prState := &PRState{PRNumber: 103, HeadSHA: "abc123", Stage: StageReleasing}
	c.mu.Lock()
	c.activePRs[103] = prState
	c.mu.Unlock()

	if err := c.handleReleasing(context.Background(), prState); err != nil {
		t.Fatalf("nil releaser must be a graceful no-op, got error: %v", err)
	}
	if anyGHCall {
		t.Error("nil releaser must not make any GitHub API call")
	}
	c.mu.RLock()
	_, stillTracked := c.activePRs[103]
	c.mu.RUnlock()
	if stillTracked {
		t.Error("PR must be drained from tracking when releaser is nil")
	}
}
