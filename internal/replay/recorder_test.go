package replay

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewRecorder(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	recorder, err := NewRecorder("TASK-123", "/path/to/project", tmpDir)
	if err != nil {
		t.Fatalf("Failed to create recorder: %v", err)
	}

	// Verify recording ID format
	if recorder.id == "" {
		t.Error("Recording ID should not be empty")
	}
	if recorder.id[:3] != "TG-" {
		t.Errorf("Recording ID should start with TG-, got: %s", recorder.id)
	}

	// Verify directory structure created
	recordingDir := filepath.Join(tmpDir, recorder.id)
	if _, err := os.Stat(recordingDir); os.IsNotExist(err) {
		t.Error("Recording directory should exist")
	}

	diffsDir := filepath.Join(recordingDir, "diffs")
	if _, err := os.Stat(diffsDir); os.IsNotExist(err) {
		t.Error("Diffs directory should exist")
	}

	// Verify stream file created
	streamPath := filepath.Join(recordingDir, "stream.jsonl")
	if _, err := os.Stat(streamPath); os.IsNotExist(err) {
		t.Error("Stream file should exist")
	}

	// Clean up
	_ = recorder.Finish("completed")
}

func TestRecordEvent(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, err := NewRecorder("TASK-456", "/test/project", tmpDir)
	if err != nil {
		t.Fatalf("Failed to create recorder: %v", err)
	}

	// Record some events
	events := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test/file.go"}}]}}`,
		`{"type":"user","tool_use_result":{"content":"file contents"}}`,
		`{"type":"result","result":"done","is_error":false,"usage":{"input_tokens":100,"output_tokens":50}}`,
	}

	for _, event := range events {
		if err := recorder.RecordEvent(event); err != nil {
			t.Errorf("Failed to record event: %v", err)
		}
	}

	// Finish recording
	if err := recorder.Finish("completed"); err != nil {
		t.Fatalf("Failed to finish recording: %v", err)
	}

	// Verify event count
	recording := recorder.GetRecording()
	if recording.EventCount != 4 {
		t.Errorf("Expected 4 events, got %d", recording.EventCount)
	}

	// Verify token usage was tracked
	if recording.TokenUsage.InputTokens != 100 {
		t.Errorf("Expected 100 input tokens, got %d", recording.TokenUsage.InputTokens)
	}
	if recording.TokenUsage.OutputTokens != 50 {
		t.Errorf("Expected 50 output tokens, got %d", recording.TokenUsage.OutputTokens)
	}
}

func TestSetMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, err := NewRecorder("TASK-789", "/test/project", tmpDir)
	if err != nil {
		t.Fatalf("Failed to create recorder: %v", err)
	}

	// Set various metadata
	recorder.SetBranch("feature/test")
	recorder.SetCommitSHA("abc1234")
	recorder.SetPRUrl("https://github.com/test/pr/123")
	recorder.SetNavigator(true)
	recorder.SetModel("claude-sonnet-4-6")
	recorder.SetMetadata("custom_key", "custom_value")

	if err := recorder.Finish("completed"); err != nil {
		t.Fatalf("Failed to finish recording: %v", err)
	}

	// Verify metadata
	recording := recorder.GetRecording()
	if recording.Metadata.Branch != "feature/test" {
		t.Errorf("Branch mismatch: %s", recording.Metadata.Branch)
	}
	if recording.Metadata.CommitSHA != "abc1234" {
		t.Errorf("CommitSHA mismatch: %s", recording.Metadata.CommitSHA)
	}
	if recording.Metadata.PRUrl != "https://github.com/test/pr/123" {
		t.Errorf("PRUrl mismatch: %s", recording.Metadata.PRUrl)
	}
	if !recording.Metadata.HasNavigator {
		t.Error("HasNavigator should be true")
	}
	if recording.Metadata.ModelName != "claude-sonnet-4-6" {
		t.Errorf("ModelName mismatch: %s", recording.Metadata.ModelName)
	}
	if recording.Metadata.Tags["custom_key"] != "custom_value" {
		t.Errorf("Custom tag mismatch: %v", recording.Metadata.Tags)
	}
}

func TestRecorderConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	recorder, _ := NewRecorder("TASK-CONCURRENT", "/test/project", tmpDir)

	// Concurrent metadata updates
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			recorder.SetMetadata("key", "value")
			recorder.SetBranch("branch")
			recorder.SetCommitSHA("sha")
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	_ = recorder.Finish("completed")
}
