package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

type fakeAPIErrorRecorder struct {
	endpoints []string
}

func (f *fakeAPIErrorRecorder) RecordAPIError(endpoint string) {
	f.endpoints = append(f.endpoints, endpoint)
}

func TestClient_RecordsAPIErrorOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer server.Close()

	rec := &fakeAPIErrorRecorder{}
	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL).WithAPIErrorRecorder(rec)

	err := client.doRequest(context.Background(), http.MethodGet, "/repos/owner/repo/issues/1", nil, nil)
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
	if len(rec.endpoints) != 1 {
		t.Fatalf("RecordAPIError called %d times, want 1", len(rec.endpoints))
	}
	if rec.endpoints[0] != "/repos/owner/repo/issues/1" {
		t.Errorf("endpoint = %q, want %q", rec.endpoints[0], "/repos/owner/repo/issues/1")
	}
}

func TestClient_NoRecorderIsNoOp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	// No recorder configured: must not panic.
	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	if err := client.doRequest(context.Background(), http.MethodGet, "/x", nil, nil); err == nil {
		t.Fatal("expected error from 404 response")
	}
}

func TestClient_RecordsAPIErrorNotOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer server.Close()

	rec := &fakeAPIErrorRecorder{}
	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL).WithAPIErrorRecorder(rec)

	if err := client.doRequest(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.endpoints) != 0 {
		t.Errorf("RecordAPIError called %d times on success, want 0", len(rec.endpoints))
	}
}
