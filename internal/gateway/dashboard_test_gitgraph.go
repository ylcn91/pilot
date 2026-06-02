package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleGitGraph(t *testing.T) {
	// Fake fetcher that records calls and returns a stub result.
	var capturedPath string
	var capturedLimit int
	fakeFetcher := GitGraphFetcher(func(path string, limit int) interface{} {
		capturedPath = path
		capturedLimit = limit
		return &mockGitGraphResult{Lines: []interface{}{}, TotalCount: 0}
	})

	tests := []struct {
		name           string
		method         string
		url            string
		fetcher        GitGraphFetcher
		projectPath    string
		expectedStatus int
		checkBody      func(t *testing.T, body []byte)
		checkCaptures  func(t *testing.T)
	}{
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			url:            "/api/v1/gitgraph",
			fetcher:        fakeFetcher,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "no fetcher configured returns 503",
			method:         http.MethodGet,
			url:            "/api/v1/gitgraph",
			fetcher:        nil,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:        "success returns JSON with lines field",
			method:      http.MethodGet,
			url:         "/api/v1/gitgraph",
			fetcher:     fakeFetcher,
			projectPath: "/some/repo",
			checkBody: func(t *testing.T, body []byte) {
				var resp map[string]interface{}
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if _, ok := resp["lines"]; !ok {
					t.Error("expected 'lines' field in response")
				}
			},
			checkCaptures: func(t *testing.T) {
				if capturedPath != "/some/repo" {
					t.Errorf("expected projectPath '/some/repo', got %q", capturedPath)
				}
				if capturedLimit != 100 {
					t.Errorf("expected default limit 100, got %d", capturedLimit)
				}
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "respects limit param",
			method:      http.MethodGet,
			url:         "/api/v1/gitgraph?limit=5",
			fetcher:     fakeFetcher,
			projectPath: ".",
			checkCaptures: func(t *testing.T) {
				if capturedLimit != 5 {
					t.Errorf("expected limit 5, got %d", capturedLimit)
				}
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "invalid limit uses default 100",
			method:      http.MethodGet,
			url:         "/api/v1/gitgraph?limit=bad",
			fetcher:     fakeFetcher,
			projectPath: ".",
			checkCaptures: func(t *testing.T) {
				if capturedLimit != 100 {
					t.Errorf("expected default limit 100, got %d", capturedLimit)
				}
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "empty projectPath defaults to dot",
			method:  http.MethodGet,
			url:     "/api/v1/gitgraph",
			fetcher: fakeFetcher,
			checkCaptures: func(t *testing.T) {
				if capturedPath != "." {
					t.Errorf("expected default path '.', got %q", capturedPath)
				}
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capturedPath = ""
			capturedLimit = 0

			s := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
			if tt.fetcher != nil {
				s.SetGitGraphFetcher(tt.fetcher)
			}
			if tt.projectPath != "" {
				s.SetGitGraphPath(tt.projectPath)
			}
			req := httptest.NewRequest(tt.method, tt.url, nil)
			w := httptest.NewRecorder()

			s.handleGitGraph(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, w.Body.Bytes())
			}
			if tt.checkCaptures != nil {
				tt.checkCaptures(t)
			}
		})
	}
}

func TestIssueIDFromTaskID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GH-100", "GH-100"},
		{"repo/GH-100", "GH-100"},
		{"org/repo/GH-100", "repo/GH-100"},
		{"", ""},
	}

	for _, tt := range tests {
		got := issueIDFromTaskID(tt.input)
		if got != tt.expected {
			t.Errorf("issueIDFromTaskID(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeDashboardStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"completed", "done"},
		{"running", "running"},
		{"queued", "queued"},
		{"pending", "pending"},
		{"failed", "failed"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		got := normalizeDashboardStatus(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeDashboardStatus(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDashboardIssueURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GH-100", "https://github.com/ylcn91/pilot/issues/100"},
		{"LINEAR-123", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := dashboardIssueURL(tt.input)
		if got != tt.expected {
			t.Errorf("dashboardIssueURL(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
