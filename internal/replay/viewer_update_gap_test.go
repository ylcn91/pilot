package replay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newUpdateTestModel builds a ViewerModel backed by a small recording so the
// Update() dispatch has real events to navigate and filter.
func newUpdateTestModel(t *testing.T) *ViewerModel {
	t.Helper()
	tmpDir := t.TempDir()

	recorder, err := NewRecorder("TASK-UPDATE", "/test/project", tmpDir)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	events := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"thinking"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/a.go"}}]}}`,
		`{"type":"result","result":"done"}`,
		`{"type":"result","result":"boom","is_error":true}`,
	}
	for _, e := range events {
		_ = recorder.RecordEvent(e)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	model, err := NewViewerModel(recording)
	if err != nil {
		t.Fatalf("NewViewerModel: %v", err)
	}
	return model
}

// runeKey constructs a tea.KeyMsg whose String() equals the given single-rune
// key string (e.g. "q", "g", "1", "t").
func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune(s)})
}

// TestViewerUpdateQuitKeys verifies the quit key bindings set the quit flag and
// return the tea.Quit command.
func TestViewerUpdateQuitKeys(t *testing.T) {
	keys := []tea.KeyMsg{
		runeKey("q"),
		tea.KeyMsg(tea.Key{Type: tea.KeyCtrlC}),
	}
	for _, key := range keys {
		m := newUpdateTestModel(t)
		updated, cmd := m.Update(key)
		vm := updated.(*ViewerModel)
		if !vm.quit {
			t.Errorf("key %q: expected quit flag set", key.String())
		}
		if cmd == nil {
			t.Errorf("key %q: expected a tea.Quit command", key.String())
		}
	}
}

// TestViewerUpdateSpeedKeys verifies the digit keys map to the documented
// playback speeds.
func TestViewerUpdateSpeedKeys(t *testing.T) {
	tests := []struct {
		key  string
		want float64
	}{
		{"1", 0.5},
		{"2", 1.0},
		{"3", 2.0},
		{"4", 4.0},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			m := newUpdateTestModel(t)
			updated, _ := m.Update(runeKey(tc.key))
			vm := updated.(*ViewerModel)
			if vm.speed != tc.want {
				t.Errorf("key %q: speed = %v, want %v", tc.key, vm.speed, tc.want)
			}
		})
	}
}

// TestViewerUpdateNavigationKeys verifies next/prev/go-to-start/go-to-end key
// bindings move the current cursor as expected.
func TestViewerUpdateNavigationKeys(t *testing.T) {
	// Go to end ("G") should land on the last filtered event.
	m := newUpdateTestModel(t)
	updated, _ := m.Update(runeKey("G"))
	vm := updated.(*ViewerModel)
	last := len(vm.filteredIdx) - 1
	if vm.current != last {
		t.Fatalf("after G, current = %d, want %d", vm.current, last)
	}

	// "down"/"j" advances and pauses playback; from the end it stays put.
	vm.current = 0
	vm.playing = true
	updated, _ = vm.Update(tea.KeyMsg(tea.Key{Type: tea.KeyDown}))
	vm = updated.(*ViewerModel)
	if vm.current != 1 {
		t.Errorf("after down, current = %d, want 1", vm.current)
	}
	if vm.playing {
		t.Error("down key should pause playback")
	}

	// "up"/"k" moves back one.
	updated, _ = vm.Update(runeKey("k"))
	vm = updated.(*ViewerModel)
	if vm.current != 0 {
		t.Errorf("after k, current = %d, want 0", vm.current)
	}

	// "g" jumps to the start.
	vm.current = last
	updated, _ = vm.Update(runeKey("g"))
	vm = updated.(*ViewerModel)
	if vm.current != 0 {
		t.Errorf("after g, current = %d, want 0", vm.current)
	}
}

// TestViewerUpdatePagingKeys verifies pgup/pgdown jump by ten events, clamped to
// the bounds of the filtered list.
func TestViewerUpdatePagingKeys(t *testing.T) {
	m := newUpdateTestModel(t)
	last := len(m.filteredIdx) - 1

	updated, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyPgDown}))
	vm := updated.(*ViewerModel)
	if vm.current != last {
		t.Errorf("after pgdown, current = %d, want clamped to %d", vm.current, last)
	}

	updated, _ = vm.Update(tea.KeyMsg(tea.Key{Type: tea.KeyPgUp}))
	vm = updated.(*ViewerModel)
	if vm.current != 0 {
		t.Errorf("after pgup, current = %d, want clamped to 0", vm.current)
	}
}

// TestViewerUpdateFilterToggleKeys verifies the per-category toggle keys flip
// the matching filter flag and re-apply the filter.
func TestViewerUpdateFilterToggleKeys(t *testing.T) {
	tests := []struct {
		key string
		get func(EventFilter) bool
	}{
		{"t", func(f EventFilter) bool { return f.ShowTools }},
		{"x", func(f EventFilter) bool { return f.ShowText }},
		{"r", func(f EventFilter) bool { return f.ShowResults }},
		{"s", func(f EventFilter) bool { return f.ShowSystem }},
		{"e", func(f EventFilter) bool { return f.ShowErrors }},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			m := newUpdateTestModel(t)
			// Default filter has the flag on; one toggle turns it off.
			before := tc.get(m.filter)
			updated, _ := m.Update(runeKey(tc.key))
			vm := updated.(*ViewerModel)
			if tc.get(vm.filter) == before {
				t.Errorf("key %q: expected filter flag to toggle from %v", tc.key, before)
			}
		})
	}
}

// TestViewerUpdateShowAllResetsFilter verifies "a" restores the default
// (all-on) filter after categories were toggled off.
func TestViewerUpdateShowAllResetsFilter(t *testing.T) {
	m := newUpdateTestModel(t)
	m.filter = EventFilter{} // everything off

	updated, _ := m.Update(runeKey("a"))
	vm := updated.(*ViewerModel)

	if vm.filter != DefaultEventFilter() {
		t.Errorf("after 'a', filter = %+v, want default all-on", vm.filter)
	}
	if len(vm.filteredIdx) != len(vm.events) {
		t.Errorf("after 'a', expected all %d events visible, got %d", len(vm.events), len(vm.filteredIdx))
	}
}

// TestViewerUpdatePlayPauseToggle verifies space toggles the playing flag and
// returns a tick command when transitioning into the playing state.
func TestViewerUpdatePlayPauseToggle(t *testing.T) {
	m := newUpdateTestModel(t)
	if m.playing {
		t.Fatal("viewer should start paused")
	}

	updated, cmd := m.Update(runeKey(" "))
	vm := updated.(*ViewerModel)
	if !vm.playing {
		t.Error("space should start playback")
	}
	if cmd == nil {
		t.Error("starting playback should return a tick command")
	}

	updated, _ = vm.Update(runeKey(" "))
	vm = updated.(*ViewerModel)
	if vm.playing {
		t.Error("second space should pause playback")
	}
}

// TestViewerUpdateHelpToggle verifies "?" toggles the help overlay.
func TestViewerUpdateHelpToggle(t *testing.T) {
	m := newUpdateTestModel(t)
	updated, _ := m.Update(runeKey("?"))
	vm := updated.(*ViewerModel)
	if !vm.showHelp {
		t.Error("'?' should enable the help overlay")
	}
	updated, _ = vm.Update(runeKey("h"))
	vm = updated.(*ViewerModel)
	if vm.showHelp {
		t.Error("'h' should toggle the help overlay back off")
	}
}

// TestViewerUpdateWindowResize verifies a WindowSizeMsg updates width/height.
func TestViewerUpdateWindowResize(t *testing.T) {
	m := newUpdateTestModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 123, Height: 45})
	vm := updated.(*ViewerModel)
	if vm.width != 123 || vm.height != 45 {
		t.Errorf("after resize, got %dx%d, want 123x45", vm.width, vm.height)
	}
}

// TestViewerUpdateTickAdvancesWhilePlaying verifies a tickMsg advances the
// cursor while playing and stops playback once the end is reached.
func TestViewerUpdateTickAdvancesWhilePlaying(t *testing.T) {
	m := newUpdateTestModel(t)
	m.playing = true
	m.current = 0

	updated, cmd := m.Update(tickMsg{})
	vm := updated.(*ViewerModel)
	if vm.current != 1 {
		t.Errorf("tick should advance current to 1, got %d", vm.current)
	}
	if cmd == nil {
		t.Error("tick mid-stream should schedule the next tick")
	}

	// Drive to the end: once current reaches the last event, playback stops.
	vm.current = len(vm.filteredIdx) - 2
	updated, _ = vm.Update(tickMsg{})
	vm = updated.(*ViewerModel)
	if vm.playing {
		t.Error("playback should stop at the end of the stream")
	}
}

// TestViewerUpdateTickIgnoredWhenPaused verifies a tickMsg is a no-op when not
// playing.
func TestViewerUpdateTickIgnoredWhenPaused(t *testing.T) {
	m := newUpdateTestModel(t)
	m.playing = false
	m.current = 2

	updated, cmd := m.Update(tickMsg{})
	vm := updated.(*ViewerModel)
	if vm.current != 2 {
		t.Errorf("tick while paused must not move cursor, got %d", vm.current)
	}
	if cmd != nil {
		t.Error("tick while paused must not schedule another tick")
	}
}
