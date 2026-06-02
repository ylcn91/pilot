package azuredevops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestPollerFindOldestUnprocessedWorkItem(t *testing.T) {
	// Create mock server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// WIQL query
			wiqlResult := WIQLQueryResult{
				WorkItems: []WIQLWorkItemRef{
					{ID: 1},
					{ID: 2},
					{ID: 3},
				},
			}
			_ = json.NewEncoder(w).Encode(wiqlResult)
		} else {
			// Work items batch
			workItems := struct {
				Count int         `json:"count"`
				Value []*WorkItem `json:"value"`
			}{
				Count: 3,
				Value: []*WorkItem{
					{
						ID: 1,
						Fields: map[string]interface{}{
							"System.Title":       "First (oldest)",
							"System.CreatedDate": "2024-01-01T10:00:00Z",
							"System.Tags":        "pilot",
						},
					},
					{
						ID: 2,
						Fields: map[string]interface{}{
							"System.Title":       "Second",
							"System.CreatedDate": "2024-01-02T10:00:00Z",
							"System.Tags":        "pilot; pilot-in-progress", // Already in progress
						},
					},
					{
						ID: 3,
						Fields: map[string]interface{}{
							"System.Title":       "Third",
							"System.CreatedDate": "2024-01-03T10:00:00Z",
							"System.Tags":        "pilot",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(workItems)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	poller := NewPoller(client, "pilot", 30*time.Second)

	ctx := context.Background()
	wi, err := poller.findOldestUnprocessedWorkItem(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wi == nil {
		t.Fatal("expected to find a work item")
	}

	// Should find ID 1 (oldest that's not in progress)
	if wi.ID != 1 {
		t.Errorf("expected oldest unprocessed work item ID 1, got %d", wi.ID)
	}
}

func TestPollerFindOldestUnprocessedWorkItemNone(t *testing.T) {
	// Create mock server with no work items
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// Empty WIQL result
			wiqlResult := WIQLQueryResult{
				WorkItems: []WIQLWorkItemRef{},
			}
			_ = json.NewEncoder(w).Encode(wiqlResult)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	poller := NewPoller(client, "pilot", 30*time.Second)

	ctx := context.Background()
	wi, err := poller.findOldestUnprocessedWorkItem(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wi != nil {
		t.Errorf("expected nil when no work items, got ID %d", wi.ID)
	}
}

func TestExtractPRNumber(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected int
		wantErr  bool
	}{
		{
			name:     "valid PR URL",
			url:      "https://dev.azure.com/org/project/_git/repo/pullrequest/123",
			expected: 123,
			wantErr:  false,
		},
		{
			name:     "PR URL with trailing slash",
			url:      "https://dev.azure.com/org/project/_git/repo/pullrequest/456/",
			expected: 456,
			wantErr:  false,
		},
		{
			name:     "empty URL",
			url:      "",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "URL without PR number",
			url:      "https://dev.azure.com/org/project/_git/repo",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "invalid URL format",
			url:      "not a url",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractPRNumber(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractPRNumber() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("ExtractPRNumber() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestPoller_hasStatusTag(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "org", "project")
	poller := NewPoller(client, "pilot", 30*time.Second)

	tests := []struct {
		name string
		tags string
		want bool
	}{
		{
			name: "no status tags",
			tags: "pilot; bug",
			want: false,
		},
		{
			name: "in-progress tag",
			tags: "pilot; " + TagInProgress,
			want: true,
		},
		{
			name: "done tag",
			tags: "pilot; " + TagDone,
			want: true,
		},
		{
			name: "failed tag",
			tags: "pilot; " + TagFailed,
			want: true,
		},
		{
			name: "empty tags",
			tags: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wi := &WorkItem{
				ID: 1,
				Fields: map[string]interface{}{
					"System.Tags": tt.tags,
				},
			}
			got := poller.hasStatusTag(wi)
			if got != tt.want {
				t.Errorf("hasStatusTag() = %v, want %v", got, tt.want)
			}
		})
	}
}
