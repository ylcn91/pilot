package replay

import (
	"context"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// TestPlayerPlayContextAlreadyCancelled covers the select-on-ctx.Done branch
// at the top of Play's loop: when the context is already cancelled before the
// first event is processed, Play must return ctx.Err() without invoking the
// callback at all. This is wall-clock independent.
func TestPlayerPlayContextAlreadyCancelled(t *testing.T) {
	tmpDir := t.TempDir()

	recorder, _ := NewRecorder("TASK-PRECANCEL", "/test/project", tmpDir)
	for i := 0; i < 5; i++ {
		_ = recorder.RecordEvent(`{"type":"system","subtype":"test"}`)
	}
	_ = recorder.Finish("completed")

	recording, _ := LoadRecording(tmpDir, recorder.GetRecordingID())
	player, _ := NewPlayer(recording, nil)

	var count int
	player.OnEvent(func(event *StreamEvent, index, total int) error {
		count++
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Play starts

	err := player.Play(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 events played for a pre-cancelled context, got %d", count)
	}
}

// TestPlayerPlaySpeedSkipsZeroDelay drives the Speed>0 branch with events that
// share an identical timestamp so the scaled delay is zero and the time.Sleep
// guard (scaledDelay > 0) is skipped. No wall-clock synchronization is needed
// because the delay between equal timestamps is zero.
func TestPlayerPlaySpeedSkipsZeroDelay(t *testing.T) {
	player := &Player{
		events: []*StreamEvent{
			{Sequence: 0, Timestamp: fixedTime, Parsed: &ParsedEvent{Type: "system"}},
			{Sequence: 1, Timestamp: fixedTime, Parsed: &ParsedEvent{Type: "assistant", Text: "x"}},
			{Sequence: 2, Timestamp: fixedTime, Parsed: &ParsedEvent{Type: "result"}},
		},
		options: &ReplayOptions{Speed: 1.0},
	}

	var count int
	player.OnEvent(func(event *StreamEvent, index, total int) error {
		count++
		return nil
	})

	if err := player.Play(context.Background()); err != nil {
		t.Fatalf("Play failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 events played, got %d", count)
	}
}
