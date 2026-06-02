package replay

import (
	"testing"
)

func TestNewViewerModel(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a recording with events
	recorder, err := NewRecorder("TASK-VIEWER", "/test/project", tmpDir)
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

	// Load and create viewer
	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	model, err := NewViewerModel(recording)
	if err != nil {
		t.Fatalf("Failed to create viewer: %v", err)
	}

	if len(model.events) != 4 {
		t.Errorf("Expected 4 events, got %d", len(model.events))
	}

	if model.playing {
		t.Error("Viewer should start paused")
	}

	if model.speed != 1.0 {
		t.Errorf("Default speed should be 1.0, got %f", model.speed)
	}
}

func TestViewerNavigation(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-NAV", "/test/project", tmpDir)
	for i := 0; i < 20; i++ {
		_ = recorder.RecordEvent(`{"type":"system","subtype":"test"}`)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	model, _ := NewViewerModel(recording)

	// Test next
	model.nextEvent()
	if model.current != 1 {
		t.Errorf("After next, current should be 1, got %d", model.current)
	}

	// Test prev
	model.prevEvent()
	if model.current != 0 {
		t.Errorf("After prev, current should be 0, got %d", model.current)
	}

	// Test prev at start (should stay at 0)
	model.prevEvent()
	if model.current != 0 {
		t.Errorf("Prev at start should stay at 0, got %d", model.current)
	}

	// Test jumping to end
	for i := 0; i < 25; i++ {
		model.nextEvent()
	}
	if model.current != 19 {
		t.Errorf("After many nexts, current should be 19, got %d", model.current)
	}
}

func TestViewerSpeedSettings(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-SPEED", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	model, _ := NewViewerModel(recording)

	// Test default speed
	if model.speed != 1.0 {
		t.Errorf("Default speed should be 1.0, got %f", model.speed)
	}

	// Test speed values
	speeds := []float64{0.5, 1.0, 2.0, 4.0}
	for _, s := range speeds {
		model.speed = s
		if model.speed != s {
			t.Errorf("Speed should be %f, got %f", s, model.speed)
		}
	}
}

func TestViewerScrolling(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-SCROLL", "/test/project", tmpDir)
	for i := 0; i < 100; i++ {
		_ = recorder.RecordEvent(`{"type":"system","subtype":"test"}`)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	model, _ := NewViewerModel(recording)
	model.height = 30 // Simulating a terminal with 30 rows

	// Initial scroll should be 0
	if model.scrollY != 0 {
		t.Errorf("Initial scroll should be 0, got %d", model.scrollY)
	}

	// Navigate down beyond visible area
	for i := 0; i < 30; i++ {
		model.nextEvent()
	}

	// Scroll should have updated
	if model.scrollY <= 0 {
		t.Error("Scroll should have increased after navigating down")
	}
}

func TestCheckTerminalSupport(t *testing.T) {
	// This test just ensures the function runs without panicking
	// The actual result depends on the test environment
	_ = CheckTerminalSupport()
}

func TestViewerView(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-VIEW", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.RecordEvent(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]}}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	model, _ := NewViewerModel(recording)
	model.width = 80
	model.height = 24

	// Test normal view
	view := model.View()
	if !containsString(view, recording.ID) {
		t.Error("View should contain recording ID")
	}

	// Test help view
	model.showHelp = true
	helpView := model.View()
	if !containsString(helpView, "NAVIGATION") {
		t.Error("Help view should contain NAVIGATION section")
	}

	// Test quit view
	model.quit = true
	quitView := model.View()
	if quitView != "" {
		t.Error("Quit view should be empty")
	}
}

func TestViewerInit(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-INIT", "/test/project", tmpDir)
	_ = recorder.RecordEvent(`{"type":"system","subtype":"init"}`)
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	model, _ := NewViewerModel(recording)

	cmd := model.Init()
	if cmd != nil {
		t.Error("Init should return nil command")
	}
}
