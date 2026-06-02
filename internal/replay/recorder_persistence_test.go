package replay

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListRecordings(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple recordings
	for i := 0; i < 3; i++ {
		recorder, err := NewRecorder("TASK-"+string(rune('A'+i)), "/test/project", tmpDir)
		if err != nil {
			t.Fatalf("Failed to create recorder %d: %v", i, err)
		}
		_ = recorder.Finish("completed")
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// List recordings
	recordings, err := ListRecordings(tmpDir, nil)
	if err != nil {
		t.Fatalf("Failed to list recordings: %v", err)
	}

	if len(recordings) != 3 {
		t.Errorf("Expected 3 recordings, got %d", len(recordings))
	}

	// Test with limit
	filter := &RecordingFilter{Limit: 2}
	recordings, err = ListRecordings(tmpDir, filter)
	if err != nil {
		t.Fatalf("Failed to list recordings with limit: %v", err)
	}

	if len(recordings) != 2 {
		t.Errorf("Expected 2 recordings with limit, got %d", len(recordings))
	}
}

func TestLoadRecording(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a recording
	recorder, err := NewRecorder("TASK-LOAD", "/test/project", tmpDir)
	if err != nil {
		t.Fatalf("Failed to create recorder: %v", err)
	}

	recordingID := recorder.GetRecordingID()
	recorder.SetBranch("test-branch")
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.Finish("completed")

	// Load the recording
	loaded, err := LoadRecording(tmpDir, recordingID)
	if err != nil {
		t.Fatalf("Failed to load recording: %v", err)
	}

	if loaded.ID != recordingID {
		t.Errorf("ID mismatch: expected %s, got %s", recordingID, loaded.ID)
	}
	if loaded.TaskID != "TASK-LOAD" {
		t.Errorf("TaskID mismatch: expected TASK-LOAD, got %s", loaded.TaskID)
	}
	if loaded.Metadata.Branch != "test-branch" {
		t.Errorf("Branch mismatch: expected test-branch, got %s", loaded.Metadata.Branch)
	}
}

func TestLoadStreamEvents(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a recording with events
	recorder, err := NewRecorder("TASK-EVENTS", "/test/project", tmpDir)
	if err != nil {
		t.Fatalf("Failed to create recorder: %v", err)
	}

	events := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]}}`,
		`{"type":"result","result":"done"}`,
	}

	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	// Load recording and events
	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	loadedEvents, err := LoadStreamEvents(recording)
	if err != nil {
		t.Fatalf("Failed to load events: %v", err)
	}

	if len(loadedEvents) != 3 {
		t.Errorf("Expected 3 events, got %d", len(loadedEvents))
	}

	// Verify event sequence numbers
	for i, e := range loadedEvents {
		if e.Sequence != i+1 {
			t.Errorf("Event %d has wrong sequence: %d", i, e.Sequence)
		}
	}
}

func TestDeleteRecording(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a recording
	recorder, err := NewRecorder("TASK-DELETE", "/test/project", tmpDir)
	if err != nil {
		t.Fatalf("Failed to create recorder: %v", err)
	}

	recordingID := recorder.GetRecordingID()
	_ = recorder.Finish("completed")

	// Verify it exists
	recordingDir := filepath.Join(tmpDir, recordingID)
	if _, err := os.Stat(recordingDir); os.IsNotExist(err) {
		t.Fatal("Recording should exist before deletion")
	}

	// Delete it
	if err := DeleteRecording(tmpDir, recordingID); err != nil {
		t.Fatalf("Failed to delete recording: %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(recordingDir); !os.IsNotExist(err) {
		t.Error("Recording should not exist after deletion")
	}
}

func TestDefaultRecordingsPath(t *testing.T) {
	path := DefaultRecordingsPath()
	if path == "" {
		t.Error("Default recordings path should not be empty")
	}
	if !filepath.IsAbs(path) {
		t.Error("Default recordings path should be absolute")
	}
}

func TestListRecordingsWithFilters(t *testing.T) {
	tmpDir := t.TempDir()

	// Create recordings with different properties
	recorder1, _ := NewRecorder("TASK-A", "/project/a", tmpDir)
	_ = recorder1.Finish("completed")
	time.Sleep(20 * time.Millisecond)

	recorder2, _ := NewRecorder("TASK-B", "/project/b", tmpDir)
	_ = recorder2.Finish("failed")
	time.Sleep(20 * time.Millisecond)

	recorder3, _ := NewRecorder("TASK-C", "/project/a", tmpDir)
	_ = recorder3.Finish("completed")

	// Test filter by project path
	filter := &RecordingFilter{ProjectPath: "/project/a"}
	recordings, err := ListRecordings(tmpDir, filter)
	if err != nil {
		t.Fatalf("Failed to list recordings: %v", err)
	}
	if len(recordings) != 2 {
		t.Errorf("Expected 2 recordings for /project/a, got %d", len(recordings))
	}

	// Test filter by status
	filter = &RecordingFilter{Status: "failed"}
	recordings, err = ListRecordings(tmpDir, filter)
	if err != nil {
		t.Fatalf("Failed to list recordings: %v", err)
	}
	if len(recordings) != 1 {
		t.Errorf("Expected 1 failed recording, got %d", len(recordings))
	}

	// Test filter by since time
	filter = &RecordingFilter{Since: time.Now().Add(-10 * time.Millisecond)}
	recordings, err = ListRecordings(tmpDir, filter)
	if err != nil {
		t.Fatalf("Failed to list recordings: %v", err)
	}
	// Should only get the most recent one (recorder3)
	if len(recordings) != 1 {
		t.Errorf("Expected 1 recent recording, got %d", len(recordings))
	}
}

func TestListRecordingsNonExistentDir(t *testing.T) {
	recordings, err := ListRecordings("/nonexistent/path", nil)
	if err != nil {
		t.Fatalf("Should return empty slice for nonexistent path, got error: %v", err)
	}
	if len(recordings) != 0 {
		t.Errorf("Expected empty slice, got %d recordings", len(recordings))
	}
}

func TestListRecordingsSkipsInvalid(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid recording
	recorder, _ := NewRecorder("TASK-VALID", "/test/project", tmpDir)
	_ = recorder.Finish("completed")

	// Create an invalid directory (not a TG- prefix)
	_ = os.MkdirAll(filepath.Join(tmpDir, "invalid-dir"), 0755)

	// Create a TG- directory without metadata.json
	_ = os.MkdirAll(filepath.Join(tmpDir, "TG-invalid"), 0755)

	// Create a TG- directory with invalid JSON
	invalidDir := filepath.Join(tmpDir, "TG-badjson")
	_ = os.MkdirAll(invalidDir, 0755)
	_ = os.WriteFile(filepath.Join(invalidDir, "metadata.json"), []byte("not json"), 0644)

	recordings, err := ListRecordings(tmpDir, nil)
	if err != nil {
		t.Fatalf("Failed to list recordings: %v", err)
	}

	// Should only get the valid recording
	if len(recordings) != 1 {
		t.Errorf("Expected 1 valid recording, got %d", len(recordings))
	}
}

func TestLoadRecordingNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadRecording(tmpDir, "nonexistent-id")
	if err == nil {
		t.Error("Expected error for nonexistent recording")
	}
}

func TestLoadRecordingInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory with invalid JSON
	invalidDir := filepath.Join(tmpDir, "TG-invalid")
	_ = os.MkdirAll(invalidDir, 0755)
	_ = os.WriteFile(filepath.Join(invalidDir, "metadata.json"), []byte("not json"), 0644)

	_, err := LoadRecording(tmpDir, "TG-invalid")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestLoadStreamEventsEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-EMPTY", "/test/project", tmpDir)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	events, err := LoadStreamEvents(recording)
	if err != nil {
		t.Fatalf("Failed to load events: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("Expected 0 events, got %d", len(events))
	}
}
