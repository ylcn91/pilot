package asana

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewPoller(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{
		PilotTag: "pilot",
	}
	poller := NewPoller(client, config, 30*time.Second)

	if poller.config.PilotTag != "pilot" {
		t.Errorf("expected pilotTag 'pilot', got '%s'", poller.config.PilotTag)
	}

	if poller.interval != 30*time.Second {
		t.Errorf("expected interval 30s, got %v", poller.interval)
	}

	if len(poller.processed) != 0 {
		t.Error("expected empty processed map")
	}
}

func TestPollerWithOptions(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}

	var callbackCalled bool
	handler := func(ctx context.Context, task *Task) (*TaskResult, error) {
		callbackCalled = true
		return &TaskResult{Success: true}, nil
	}

	poller := NewPoller(client, config, 30*time.Second,
		WithOnAsanaTask(handler),
		WithMaxConcurrent(3),
	)

	if poller.onTask == nil {
		t.Error("expected onTask handler to be set")
	}

	if poller.maxConcurrent != 3 {
		t.Errorf("expected maxConcurrent 3, got %d", poller.maxConcurrent)
	}

	if cap(poller.semaphore) != 3 {
		t.Errorf("expected semaphore capacity 3, got %d", cap(poller.semaphore))
	}

	// Call the handler to verify it's wired correctly
	_, _ = poller.onTask(context.Background(), &Task{})
	if !callbackCalled {
		t.Error("expected callback to be called")
	}
}

func TestPollerMarkProcessed(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	if poller.IsProcessed("123456") {
		t.Error("expected 123456 NOT to be processed initially")
	}

	poller.markProcessed("123456")

	if !poller.IsProcessed("123456") {
		t.Error("expected 123456 to be processed after marking")
	}

	if poller.ProcessedCount() != 1 {
		t.Errorf("expected processed count 1, got %d", poller.ProcessedCount())
	}

	poller.Reset()

	if poller.IsProcessed("123456") {
		t.Error("expected 123456 NOT to be processed after reset")
	}

	if poller.ProcessedCount() != 0 {
		t.Errorf("expected processed count 0 after reset, got %d", poller.ProcessedCount())
	}
}

func TestPollerClearProcessed(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	poller.markProcessed("123456")
	poller.markProcessed("789012")

	if poller.ProcessedCount() != 2 {
		t.Errorf("expected processed count 2, got %d", poller.ProcessedCount())
	}

	poller.ClearProcessed("123456")

	if poller.IsProcessed("123456") {
		t.Error("expected 123456 NOT to be processed after clearing")
	}
	if !poller.IsProcessed("789012") {
		t.Error("expected 789012 to still be processed")
	}
	if poller.ProcessedCount() != 1 {
		t.Errorf("expected processed count 1 after clearing one, got %d", poller.ProcessedCount())
	}
}

func TestPollerConcurrentAccess(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			gid := string(rune('0' + n%10))
			poller.markProcessed(gid)
			_ = poller.IsProcessed(gid)
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

func TestPollerHasTag(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	tests := []struct {
		name    string
		tags    []Tag
		tagName string
		want    bool
	}{
		{"no tags", []Tag{}, "pilot", false},
		{"exact match", []Tag{{Name: "pilot"}}, "pilot", true},
		{"case insensitive", []Tag{{Name: "PILOT"}}, "pilot", true},
		{"not found", []Tag{{Name: "other"}}, "pilot", false},
		{"multiple tags", []Tag{{Name: "pilot"}, {Name: "high-priority"}}, "pilot", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{Tags: tt.tags}
			got := poller.hasTag(task, tt.tagName)
			if got != tt.want {
				t.Errorf("hasTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPollerHasStatusTag(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	tests := []struct {
		name string
		tags []Tag
		want bool
	}{
		{"no tags", []Tag{}, false},
		{"pilot only", []Tag{{Name: "pilot"}}, false},
		{"in-progress", []Tag{{Name: "pilot"}, {Name: "pilot-in-progress"}}, true},
		{"done", []Tag{{Name: "pilot"}, {Name: "pilot-done"}}, true},
		{"failed", []Tag{{Name: "pilot"}, {Name: "pilot-failed"}}, true},
		{"case insensitive", []Tag{{Name: "PILOT-IN-PROGRESS"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{Tags: tt.tags}
			got := poller.hasStatusTag(task)
			if got != tt.want {
				t.Errorf("hasStatusTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPollerMaxConcurrentDefaults(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}

	// Test default value
	poller := NewPoller(client, config, 30*time.Second)
	if poller.maxConcurrent != 2 {
		t.Errorf("expected default maxConcurrent 2, got %d", poller.maxConcurrent)
	}

	// Test invalid value gets corrected
	poller = NewPoller(client, config, 30*time.Second,
		WithMaxConcurrent(0),
	)
	if poller.maxConcurrent != 1 {
		t.Errorf("expected corrected maxConcurrent 1, got %d", poller.maxConcurrent)
	}
}
