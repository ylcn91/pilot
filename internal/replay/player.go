package replay

import (
	"context"
	"fmt"
	"time"
)

// Player replays execution recordings
type Player struct {
	recording *Recording
	events    []*StreamEvent
	options   *ReplayOptions
	callback  ReplayCallback
}

// ReplayCallback is called for each event during replay
type ReplayCallback func(event *StreamEvent, index int, total int) error

// NewPlayer creates a new replay player
func NewPlayer(recording *Recording, options *ReplayOptions) (*Player, error) {
	if options == nil {
		options = DefaultReplayOptions()
	}

	events, err := LoadStreamEvents(recording)
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}

	return &Player{
		recording: recording,
		events:    events,
		options:   options,
	}, nil
}

// OnEvent sets the replay callback
func (p *Player) OnEvent(callback ReplayCallback) {
	p.callback = callback
}

// Play replays the recording
func (p *Player) Play(ctx context.Context) error {
	total := len(p.events)
	if total == 0 {
		return nil
	}

	startIdx := p.options.StartAt
	endIdx := total
	if p.options.StopAt > 0 && p.options.StopAt < total {
		endIdx = p.options.StopAt
	}

	var prevTime time.Time
	for i := startIdx; i < endIdx; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event := p.events[i]

		// Apply real-time delay if speed > 0
		if p.options.Speed > 0 && !prevTime.IsZero() {
			delay := event.Timestamp.Sub(prevTime)
			scaledDelay := time.Duration(float64(delay) / p.options.Speed)
			if scaledDelay > 0 {
				time.Sleep(scaledDelay)
			}
		}
		prevTime = event.Timestamp

		// Call callback
		if p.callback != nil {
			if err := p.callback(event, i, total); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetEvent returns an event by index
func (p *Player) GetEvent(index int) *StreamEvent {
	if index < 0 || index >= len(p.events) {
		return nil
	}
	return p.events[index]
}

// EventCount returns the total number of events
func (p *Player) EventCount() int {
	return len(p.events)
}

// GetRecording returns the recording metadata
func (p *Player) GetRecording() *Recording {
	return p.recording
}
