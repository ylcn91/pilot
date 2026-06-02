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

func TestGetWorkItem(t *testing.T) {
	workItem := WorkItem{
		ID:  123,
		Rev: 1,
		Fields: map[string]interface{}{
			"System.Title":       "Test work item",
			"System.Description": "Test description",
			"System.State":       "New",
			"System.Tags":        "pilot; bug",
		},
		URL: "https://dev.azure.com/org/project/_apis/wit/workitems/123",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(workItem)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	ctx := context.Background()

	result, err := client.GetWorkItem(ctx, 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID != 123 {
		t.Errorf("expected ID 123, got %d", result.ID)
	}
	if result.GetTitle() != "Test work item" {
		t.Errorf("expected title 'Test work item', got '%s'", result.GetTitle())
	}
}

func TestListWorkItemsByWIQL(t *testing.T) {
	// Mock WIQL response
	wiqlResponse := WIQLQueryResult{
		QueryType:       "flat",
		QueryResultType: "workItem",
		WorkItems: []WIQLWorkItemRef{
			{ID: 1, URL: "url1"},
			{ID: 2, URL: "url2"},
		},
	}

	// Mock work items response
	workItemsResponse := struct {
		Count int         `json:"count"`
		Value []*WorkItem `json:"value"`
	}{
		Count: 2,
		Value: []*WorkItem{
			{ID: 1, Fields: map[string]interface{}{"System.Title": "Item 1"}},
			{ID: 2, Fields: map[string]interface{}{"System.Title": "Item 2"}},
		},
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// WIQL query
			_ = json.NewEncoder(w).Encode(wiqlResponse)
		} else {
			// Work items batch
			_ = json.NewEncoder(w).Encode(workItemsResponse)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	ctx := context.Background()

	results, err := client.ListWorkItemsByWIQL(ctx, "SELECT [System.Id] FROM WorkItems")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestAddWorkItemTag(t *testing.T) {
	// First call: GET work item
	workItem := WorkItem{
		ID:     123,
		Fields: map[string]interface{}{"System.Tags": "existing-tag"},
	}

	// Second call: PATCH to update
	updatedWorkItem := WorkItem{
		ID:     123,
		Fields: map[string]interface{}{"System.Tags": "existing-tag; new-tag"},
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// GET work item
			_ = json.NewEncoder(w).Encode(workItem)
		} else {
			// PATCH to update
			if r.Method != http.MethodPatch {
				t.Errorf("expected PATCH for update, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json-patch+json" {
				t.Errorf("expected JSON Patch content type, got %s", r.Header.Get("Content-Type"))
			}
			_ = json.NewEncoder(w).Encode(updatedWorkItem)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	ctx := context.Background()

	err := client.AddWorkItemTag(ctx, 123, "new-tag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestRemoveWorkItemTag(t *testing.T) {
	// First call: GET work item
	workItem := WorkItem{
		ID:     123,
		Fields: map[string]interface{}{"System.Tags": "keep-tag; remove-tag"},
	}

	// Second call: PATCH to update
	updatedWorkItem := WorkItem{
		ID:     123,
		Fields: map[string]interface{}{"System.Tags": "keep-tag"},
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(workItem)
		} else {
			_ = json.NewEncoder(w).Encode(updatedWorkItem)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	ctx := context.Background()

	err := client.RemoveWorkItemTag(ctx, 123, "remove-tag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkItemHelpers(t *testing.T) {
	wi := &WorkItem{
		ID: 123,
		Fields: map[string]interface{}{
			"System.Title":                   "Test Title",
			"System.Description":             "Test Description",
			"System.State":                   "Active",
			"System.WorkItemType":            "Bug",
			"System.Tags":                    "tag1; tag2; pilot",
			"Microsoft.VSTS.Common.Priority": float64(2),
			"System.CreatedDate":             "2024-01-15T10:30:00Z",
			"System.ChangedDate":             "2024-01-16T15:45:00Z",
		},
	}

	if wi.GetTitle() != "Test Title" {
		t.Errorf("expected title 'Test Title', got '%s'", wi.GetTitle())
	}

	if wi.GetDescription() != "Test Description" {
		t.Errorf("expected description 'Test Description', got '%s'", wi.GetDescription())
	}

	if wi.GetState() != "Active" {
		t.Errorf("expected state 'Active', got '%s'", wi.GetState())
	}

	if wi.GetWorkItemType() != "Bug" {
		t.Errorf("expected type 'Bug', got '%s'", wi.GetWorkItemType())
	}

	tags := wi.GetTags()
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(tags))
	}

	if !wi.HasTag("pilot") {
		t.Error("expected work item to have 'pilot' tag")
	}

	if wi.HasTag("nonexistent") {
		t.Error("expected work item NOT to have 'nonexistent' tag")
	}

	if wi.GetPriority() != PriorityHigh {
		t.Errorf("expected priority High (2), got %d", wi.GetPriority())
	}

	created := wi.GetCreatedDate()
	if created.IsZero() {
		t.Error("expected non-zero created date")
	}
	if created.Year() != 2024 || created.Month() != time.January || created.Day() != 15 {
		t.Errorf("unexpected created date: %v", created)
	}
}

func TestTagHelpers(t *testing.T) {
	tests := []struct {
		name     string
		tags     string
		expected []string
	}{
		{"empty", "", nil},
		{"single", "tag1", []string{"tag1"}},
		{"multiple", "tag1; tag2; tag3", []string{"tag1", "tag2", "tag3"}},
		{"with spaces", "  tag1  ;  tag2  ", []string{"tag1", "tag2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitTags(tt.tags)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d tags, got %d", len(tt.expected), len(result))
				return
			}
			for i, tag := range result {
				if tag != tt.expected[i] {
					t.Errorf("expected tag %d to be '%s', got '%s'", i, tt.expected[i], tag)
				}
			}
		})
	}

	// Test joinTags
	joined := joinTags([]string{"a", "b", "c"})
	if joined != "a; b; c" {
		t.Errorf("expected 'a; b; c', got '%s'", joined)
	}

	// Test addTag
	newTags := addTag("existing", "new")
	if newTags != "existing; new" {
		t.Errorf("expected 'existing; new', got '%s'", newTags)
	}

	// Test addTag when tag exists
	sameTags := addTag("existing; new", "new")
	if sameTags != "existing; new" {
		t.Errorf("expected 'existing; new', got '%s'", sameTags)
	}

	// Test removeTag
	afterRemove := removeTag("keep; remove; also-keep", "remove")
	if afterRemove != "keep; also-keep" {
		t.Errorf("expected 'keep; also-keep', got '%s'", afterRemove)
	}
}
