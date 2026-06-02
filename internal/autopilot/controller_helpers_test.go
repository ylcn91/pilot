package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/memory"
)

// mockNotifier is a test double for the Notifier interface
type mockNotifier struct {
	notifyMergedFunc           func(ctx context.Context, prState *PRState) error
	notifyCIFailedFunc         func(ctx context.Context, prState *PRState, failedChecks []string) error
	notifyApprovalRequiredFunc func(ctx context.Context, prState *PRState) error
	notifyFixIssueCreatedFunc  func(ctx context.Context, prState *PRState, issueNumber int) error
	notifyReleasedFunc         func(ctx context.Context, prState *PRState, releaseURL string) error
}

func (m *mockNotifier) NotifyMerged(ctx context.Context, prState *PRState) error {
	if m.notifyMergedFunc != nil {
		return m.notifyMergedFunc(ctx, prState)
	}
	return nil
}

func (m *mockNotifier) NotifyCIFailed(ctx context.Context, prState *PRState, failedChecks []string) error {
	if m.notifyCIFailedFunc != nil {
		return m.notifyCIFailedFunc(ctx, prState, failedChecks)
	}
	return nil
}

func (m *mockNotifier) NotifyApprovalRequired(ctx context.Context, prState *PRState) error {
	if m.notifyApprovalRequiredFunc != nil {
		return m.notifyApprovalRequiredFunc(ctx, prState)
	}
	return nil
}

func (m *mockNotifier) NotifyFixIssueCreated(ctx context.Context, prState *PRState, issueNumber int) error {
	if m.notifyFixIssueCreatedFunc != nil {
		return m.notifyFixIssueCreatedFunc(ctx, prState, issueNumber)
	}
	return nil
}

func (m *mockNotifier) NotifyReleased(ctx context.Context, prState *PRState, releaseURL string) error {
	if m.notifyReleasedFunc != nil {
		return m.notifyReleasedFunc(ctx, prState, releaseURL)
	}
	return nil
}

// mockTaskMonitor implements TaskMonitor for testing.
type mockTaskMonitor struct {
	completedTasks map[string]string // taskID -> prURL
}

func newMockTaskMonitor() *mockTaskMonitor {
	return &mockTaskMonitor{
		completedTasks: make(map[string]string),
	}
}

func (m *mockTaskMonitor) Complete(taskID, prURL string) {
	m.completedTasks[taskID] = prURL
}

// newTestLearningLoop creates a LearningLoop backed by a temp SQLite store for testing.
// The store is returned so the caller can close and clean it up.
func newTestLearningLoop(t *testing.T) (*memory.LearningLoop, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "controller-learn-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := memory.NewStore(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}
	// nil extractor: LearnFromReview will return an error (logged as warning, not propagated)
	loop := memory.NewLearningLoop(store, nil, nil)
	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return loop, cleanup
}

// mockExecutionHealer captures execution self-heal calls for testing.
type mockExecutionHealer struct {
	selfHealed []selfHealCall
}

type selfHealCall struct {
	TaskID      string
	ProjectPath string
	PRURL       string
}

func (m *mockExecutionHealer) SelfHealExecutionAfterMerge(taskID, projectPath, prURL string) error {
	m.selfHealed = append(m.selfHealed, selfHealCall{TaskID: taskID, ProjectPath: projectPath, PRURL: prURL})
	return nil
}

// mustJSON serialises v to JSON and fails the test on error.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}
	return b
}

// mergeMockServer returns an httptest server that answers the handleMerging
// happy-path requests (PR fetch, merge, labels) and counts POSTs to the
// issue comments endpoint.
func mergeMockServer(t *testing.T, prNumber, issueNumber int, commentCount *int) *httptest.Server {
	t.Helper()
	commentPath := "/repos/owner/repo/issues/" + itoa(issueNumber) + "/comments"
	prPath := "/repos/owner/repo/pulls/" + itoa(prNumber)
	mergePath := prPath + "/merge"
	labelsPath := "/repos/owner/repo/issues/" + itoa(issueNumber) + "/labels"

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/check-runs"):
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "success"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == commentPath && r.Method == http.MethodPost:
			*commentCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 1})
		case r.URL.Path == mergePath && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sha": "merged123", "merged": true, "message": "Pull Request successfully merged",
			})
		case r.URL.Path == prPath && r.Method == http.MethodGet:
			pr := github.PullRequest{
				Number: prNumber,
				State:  "open",
				Head:   github.PRRef{Ref: "pilot/GH-" + itoa(issueNumber), SHA: "abc1234"},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(pr)
		case r.URL.Path == labelsPath && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]github.Label{{Name: github.LabelDone}})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// asyncApprovalManager returns an approval.Manager configured for async pre-merge approval.
func asyncApprovalManager() *approval.Manager {
	cfg := &approval.Config{
		Enabled: true,

		DefaultTimeout: 1 * time.Hour,
		DefaultAction:  approval.DecisionRejected,
		PreMerge: &approval.StageConfig{
			Enabled:       true,
			Timeout:       1 * time.Hour,
			DefaultAction: approval.DecisionRejected,
		},
	}
	return approval.NewManager(cfg)
}

// mockApprovalPersister records calls to SetApprovalRequestID and SetApprovalDecision
// so tests can verify that the controller wires through to the memory store.
type mockApprovalPersister struct {
	requestIDCalls []struct{ taskID, requestID string }
	decisionCalls  []struct{ requestID, decision, by string }
}

func (m *mockApprovalPersister) SetApprovalRequestID(_ context.Context, taskID, requestID string) error {
	m.requestIDCalls = append(m.requestIDCalls, struct{ taskID, requestID string }{taskID, requestID})
	return nil
}

func (m *mockApprovalPersister) SetApprovalDecision(_ context.Context, requestID, decision, by string) error {
	m.decisionCalls = append(m.decisionCalls, struct{ requestID, decision, by string }{requestID, decision, by})
	return nil
}

// errApprovalPersister is a mock that returns a configurable error for both methods.
type errApprovalPersister struct {
	requestIDErr error
	decisionErr  error
}

func (m *errApprovalPersister) SetApprovalRequestID(_ context.Context, _, _ string) error {
	return m.requestIDErr
}

func (m *errApprovalPersister) SetApprovalDecision(_ context.Context, _, _, _ string) error {
	return m.decisionErr
}

// --- Board/Project/Status/Review/Block tests (GH-3260) ---

// mockBoardSyncer is a test double for projectBoardSyncer.
type mockBoardSyncer struct {
	calls []boardSyncCall
	err   error // if non-nil, returned from UpdateProjectItemStatus
}

type boardSyncCall struct {
	issueNodeID string
	statusName  string
}

func (m *mockBoardSyncer) UpdateProjectItemStatus(_ context.Context, issueNodeID, statusName string) error {
	m.calls = append(m.calls, boardSyncCall{issueNodeID: issueNodeID, statusName: statusName})
	return m.err
}

// withBoardSyncerForTest is a ControllerOption that injects a mockBoardSyncer
// (bypasses the *github.ProjectBoardSync type constraint of WithProjectBoardSync).
func withBoardSyncerForTest(bs projectBoardSyncer, done, fail, review, inProgress string) ControllerOption {
	return func(c *Controller) {
		c.boardSync = bs
		c.doneStatus = done
		c.failStatus = fail
		c.reviewStatus = review
		c.inProgressStatus = inProgress
	}
}
