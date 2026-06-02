package jira

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewPoller(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{
		PilotLabel: "pilot",
		ProjectKey: "TEST",
	}
	poller := NewPoller(client, config, 30*time.Second)

	if poller.pilotLabel != "pilot" {
		t.Errorf("expected pilotLabel 'pilot', got '%s'", poller.pilotLabel)
	}

	if poller.interval != 30*time.Second {
		t.Errorf("expected interval 30s, got %v", poller.interval)
	}

	if len(poller.processed) != 0 {
		t.Error("expected empty processed map")
	}
}

func TestNewPoller_DefaultLabel(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{
		PilotLabel: "", // Empty label should default to "pilot"
	}
	poller := NewPoller(client, config, 30*time.Second)

	if poller.pilotLabel != "pilot" {
		t.Errorf("expected default pilotLabel 'pilot', got '%s'", poller.pilotLabel)
	}
}

func TestPollerWithOptions(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	var callbackCalled bool
	handler := func(ctx context.Context, issue *Issue) (*IssueResult, error) {
		callbackCalled = true
		return &IssueResult{Success: true}, nil
	}

	poller := NewPoller(client, config, 30*time.Second,
		WithOnJiraIssue(handler),
	)

	if poller.onIssue == nil {
		t.Error("expected onIssue handler to be set")
	}

	// Call the handler to verify it's wired correctly
	_, _ = poller.onIssue(context.Background(), &Issue{})
	if !callbackCalled {
		t.Error("expected callback to be called")
	}
}

func TestPollerMarkProcessed(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	if poller.IsProcessed("TEST-123") {
		t.Error("expected TEST-123 NOT to be processed initially")
	}

	poller.markProcessed("TEST-123")

	if !poller.IsProcessed("TEST-123") {
		t.Error("expected TEST-123 to be processed after marking")
	}

	if poller.ProcessedCount() != 1 {
		t.Errorf("expected processed count 1, got %d", poller.ProcessedCount())
	}

	poller.Reset()

	if poller.IsProcessed("TEST-123") {
		t.Error("expected TEST-123 NOT to be processed after reset")
	}

	if poller.ProcessedCount() != 0 {
		t.Errorf("expected processed count 0 after reset, got %d", poller.ProcessedCount())
	}
}

func TestPollerClearProcessed(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	poller.markProcessed("TEST-123")
	poller.markProcessed("TEST-456")

	if poller.ProcessedCount() != 2 {
		t.Errorf("expected processed count 2, got %d", poller.ProcessedCount())
	}

	poller.ClearProcessed("TEST-123")

	if poller.IsProcessed("TEST-123") {
		t.Error("expected TEST-123 NOT to be processed after clearing")
	}
	if !poller.IsProcessed("TEST-456") {
		t.Error("expected TEST-456 to still be processed")
	}
	if poller.ProcessedCount() != 1 {
		t.Errorf("expected processed count 1 after clearing one, got %d", poller.ProcessedCount())
	}
}

func TestPollerConcurrentAccess(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "TEST-" + string(rune('0'+n%10))
			poller.markProcessed(key)
			_ = poller.IsProcessed(key)
			_ = poller.ProcessedCount()
		}(i)
	}
	wg.Wait()

	// No race condition should occur
	count := poller.ProcessedCount()
	if count == 0 {
		t.Error("expected some processed items")
	}
}

func TestPollerBuildJQL(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)

	tests := []struct {
		name    string
		config  *Config
		wantJQL string
	}{
		{
			name: "label only",
			config: &Config{
				PilotLabel: "pilot",
			},
			wantJQL: `labels = "pilot" AND statusCategory != Done ORDER BY created ASC`,
		},
		{
			name: "label and project",
			config: &Config{
				PilotLabel: "pilot",
				ProjectKey: "TEST",
			},
			wantJQL: `labels = "pilot" AND project = "TEST" AND statusCategory != Done ORDER BY created ASC`,
		},
		{
			name: "custom label",
			config: &Config{
				PilotLabel: "autopilot",
				ProjectKey: "MYPROJ",
			},
			wantJQL: `labels = "autopilot" AND project = "MYPROJ" AND statusCategory != Done ORDER BY created ASC`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			poller := NewPoller(client, tt.config, 30*time.Second)
			got := poller.buildJQL()
			if got != tt.wantJQL {
				t.Errorf("buildJQL() = %q, want %q", got, tt.wantJQL)
			}
		})
	}
}

func TestPollerHasStatusLabel(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	tests := []struct {
		name   string
		labels []string
		want   bool
	}{
		{"no labels", []string{}, false},
		{"pilot only", []string{"pilot"}, false},
		{"in-progress", []string{"pilot", "pilot-in-progress"}, true},
		{"done", []string{"pilot", "pilot-done"}, true},
		{"failed", []string{"pilot", "pilot-failed"}, true},
		{"case insensitive", []string{"PILOT-IN-PROGRESS"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{
				Fields: Fields{Labels: tt.labels},
			}
			got := poller.hasStatusLabel(issue)
			if got != tt.want {
				t.Errorf("hasStatusLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClientHasLabel(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)

	tests := []struct {
		name   string
		labels []string
		search string
		want   bool
	}{
		{"exact match", []string{"pilot"}, "pilot", true},
		{"case insensitive", []string{"Pilot"}, "pilot", true},
		{"not found", []string{"pilot"}, "autopilot", false},
		{"empty labels", []string{}, "pilot", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{
				Fields: Fields{Labels: tt.labels},
			}
			got := client.HasLabel(issue, tt.search)
			if got != tt.want {
				t.Errorf("HasLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}
