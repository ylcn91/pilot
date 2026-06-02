package replay

import (
	"context"
	"fmt"
	"testing"
)

func TestNewPlayer(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a recording with events
	recorder, err := NewRecorder("TASK-PLAYER", "/test/project", tmpDir)
	if err != nil {
		t.Fatalf("Failed to create recorder: %v", err)
	}

	events := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Analyzing..."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test/file.go"}}]}}`,
		`{"type":"result","result":"done"}`,
	}

	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	// Load and create player
	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	player, err := NewPlayer(recording, nil)
	if err != nil {
		t.Fatalf("Failed to create player: %v", err)
	}

	if player.EventCount() != 4 {
		t.Errorf("Expected 4 events, got %d", player.EventCount())
	}
}

func TestPlayerPlay(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a recording
	recorder, _ := NewRecorder("TASK-PLAY", "/test/project", tmpDir)
	events := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]}}`,
		`{"type":"result","result":"done"}`,
	}
	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	// Play back
	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	player, _ := NewPlayer(recording, nil)

	var playedEvents []*StreamEvent
	player.OnEvent(func(event *StreamEvent, index, total int) error {
		playedEvents = append(playedEvents, event)
		return nil
	})

	if err := player.Play(context.Background()); err != nil {
		t.Fatalf("Play failed: %v", err)
	}

	if len(playedEvents) != 3 {
		t.Errorf("Expected 3 played events, got %d", len(playedEvents))
	}
}

func TestPlayerWithRange(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a recording with more events
	recorder, _ := NewRecorder("TASK-RANGE", "/test/project", tmpDir)
	for i := 0; i < 10; i++ {
		_ = recorder.RecordEvent(`{"type":"system","subtype":"test"}`)
	}
	_ = recorder.Finish("completed")

	// Play with range
	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	options := &ReplayOptions{
		StartAt: 3,
		StopAt:  7,
	}
	player, _ := NewPlayer(recording, options)

	var count int
	player.OnEvent(func(event *StreamEvent, index, total int) error {
		count++
		return nil
	})

	if err := player.Play(context.Background()); err != nil {
		t.Fatalf("Play failed: %v", err)
	}

	if count != 4 { // Events 3, 4, 5, 6 (indices, stopAt is exclusive)
		t.Errorf("Expected 4 events in range, got %d", count)
	}
}

func TestPlayerCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a recording
	recorder, _ := NewRecorder("TASK-CANCEL", "/test/project", tmpDir)
	for i := 0; i < 100; i++ {
		_ = recorder.RecordEvent(`{"type":"system","subtype":"test"}`)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	player, _ := NewPlayer(recording, nil)

	ctx, cancel := context.WithCancel(context.Background())

	var count int
	player.OnEvent(func(event *StreamEvent, index, total int) error {
		count++
		if count >= 10 {
			cancel()
		}
		return nil
	})

	err := player.Play(ctx)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}

	if count < 10 {
		t.Errorf("Should have played at least 10 events before cancel, got %d", count)
	}
}

func TestGetEvent(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-GET", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.RecordEvent(`{"type":"result","result":"done"}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	player, _ := NewPlayer(recording, nil)

	// Test valid index
	event := player.GetEvent(0)
	if event == nil {
		t.Error("Event at index 0 should not be nil")
	}

	event = player.GetEvent(1)
	if event == nil {
		t.Error("Event at index 1 should not be nil")
	}

	// Test invalid indices
	if player.GetEvent(-1) != nil {
		t.Error("Event at index -1 should be nil")
	}

	if player.GetEvent(100) != nil {
		t.Error("Event at index 100 should be nil")
	}
}

func TestDefaultReplayOptions(t *testing.T) {
	opts := DefaultReplayOptions()

	if opts.StartAt != 0 {
		t.Errorf("Default StartAt should be 0, got %d", opts.StartAt)
	}
	if opts.StopAt != 0 {
		t.Errorf("Default StopAt should be 0, got %d", opts.StopAt)
	}
	if opts.Speed != 0 {
		t.Errorf("Default Speed should be 0 (instant), got %f", opts.Speed)
	}
	if !opts.ShowTools {
		t.Error("Default ShowTools should be true")
	}
	if !opts.ShowText {
		t.Error("Default ShowText should be true")
	}
}

func TestPlayerGetRecording(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-REC", "/test/project", tmpDir)
	recorder.SetBranch("test-branch")
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	player, err := NewPlayer(recording, nil)
	if err != nil {
		t.Fatalf("Failed to create player: %v", err)
	}

	rec := player.GetRecording()
	if rec == nil {
		t.Fatal("GetRecording should not return nil")
	}
	if rec.TaskID != "TASK-REC" {
		t.Errorf("Expected task ID TASK-REC, got %s", rec.TaskID)
	}
	if rec.Metadata.Branch != "test-branch" {
		t.Errorf("Expected branch test-branch, got %s", rec.Metadata.Branch)
	}
}

func TestPlayerPlayWithSpeed(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-SPEED", "/test/project", tmpDir)
	// Record events with different timestamps (simulated by sequence)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.RecordEvent(`{"type":"assistant","message":{"content":[{"type":"text","text":"Processing"}]}}`)
	_ = recorder.RecordEvent(`{"type":"result","result":"done"}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())

	// Test with very fast speed (should still work)
	options := &ReplayOptions{
		Speed: 100.0, // 100x speed
	}
	player, _ := NewPlayer(recording, options)

	var count int
	player.OnEvent(func(event *StreamEvent, index, total int) error {
		count++
		return nil
	})

	if err := player.Play(context.Background()); err != nil {
		t.Fatalf("Play failed: %v", err)
	}

	if count != 3 {
		t.Errorf("Expected 3 events, got %d", count)
	}
}

func TestPlayerPlayEmptyRecording(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-EMPTY", "/test/project", tmpDir)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	player, _ := NewPlayer(recording, nil)

	var count int
	player.OnEvent(func(event *StreamEvent, index, total int) error {
		count++
		return nil
	})

	if err := player.Play(context.Background()); err != nil {
		t.Fatalf("Play failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 events for empty recording, got %d", count)
	}
}

func TestPlayerPlayCallbackError(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-CBERR", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.RecordEvent(`{"type":"result","result":"done"}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	player, _ := NewPlayer(recording, nil)

	expectedErr := fmt.Errorf("callback error")
	player.OnEvent(func(event *StreamEvent, index, total int) error {
		return expectedErr
	})

	err := player.Play(context.Background())
	if err != expectedErr {
		t.Errorf("Expected callback error, got: %v", err)
	}
}
