package asana

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNotifyTaskStarted_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"internal server error"}]}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.NotifyTaskStarted(context.Background(), "123456", "PILOT-001")
	if err == nil {
		t.Fatal("expected error when API returns 500")
	}
	if !strings.Contains(err.Error(), "failed to add start comment") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestNotifyTaskFailed_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"internal server error"}]}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.NotifyTaskFailed(context.Background(), "123456", "some reason")
	if err == nil {
		t.Fatal("expected error when API returns 500")
	}
	if !strings.Contains(err.Error(), "failed to add failure comment") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestNotifyProgress_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"internal server error"}]}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.NotifyProgress(context.Background(), "123456", "implementing", "details")
	if err == nil {
		t.Fatal("expected error when API returns 500")
	}
	if !strings.Contains(err.Error(), "failed to add progress comment") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestNotifyTaskCompleted_CommentAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"internal server error"}]}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.NotifyTaskCompleted(context.Background(), "123456", "https://github.com/pr/1", "summary")
	if err == nil {
		t.Fatal("expected error when comment API returns 500")
	}
	if !strings.Contains(err.Error(), "failed to add completion comment") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestCompleteTask_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"internal server error"}]}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.CompleteTask(context.Background(), "123456")
	if err == nil {
		t.Fatal("expected error when API returns 500")
	}
	if !strings.Contains(err.Error(), "failed to complete task") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestNotifierMethodSignatures(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")
	ctx := context.Background()

	// Verify method signatures compile
	var err error

	err = notifier.NotifyTaskStarted(ctx, "123", "PILOT-001")
	_ = err

	err = notifier.NotifyProgress(ctx, "123", "implementing", "details")
	_ = err

	err = notifier.NotifyTaskCompleted(ctx, "123", "https://github.com/pr", "summary")
	_ = err

	err = notifier.CompleteTask(ctx, "123")
	_ = err

	err = notifier.NotifyTaskFailed(ctx, "123", "reason")
	_ = err

	err = notifier.LinkPR(ctx, "123", 42, "https://github.com/pr")
	_ = err

	err = notifier.RemovePilotTag(ctx, "123")
	_ = err

	err = notifier.AddPilotTag(ctx, "123")
	_ = err
}
