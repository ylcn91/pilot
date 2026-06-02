package replay

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultRecordingsPath returns the default recordings directory
func DefaultRecordingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pilot", "recordings")
}

// ListRecordings lists all recordings matching the filter
func ListRecordings(basePath string, filter *RecordingFilter) ([]*RecordingSummary, error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*RecordingSummary{}, nil
		}
		return nil, fmt.Errorf("failed to read recordings directory: %w", err)
	}

	var summaries []*RecordingSummary

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "TG-") {
			continue
		}

		// Load metadata
		metadataPath := filepath.Join(basePath, entry.Name(), "metadata.json")
		data, err := os.ReadFile(metadataPath)
		if err != nil {
			continue // Skip invalid recordings
		}

		var recording Recording
		if err := json.Unmarshal(data, &recording); err != nil {
			continue
		}

		// Apply filters
		if filter != nil {
			if filter.ProjectPath != "" && recording.ProjectPath != filter.ProjectPath {
				continue
			}
			if filter.Status != "" && recording.Status != filter.Status {
				continue
			}
			if !filter.Since.IsZero() && recording.StartTime.Before(filter.Since) {
				continue
			}
		}

		summaries = append(summaries, &RecordingSummary{
			ID:          recording.ID,
			TaskID:      recording.TaskID,
			ProjectPath: recording.ProjectPath,
			Status:      recording.Status,
			StartTime:   recording.StartTime,
			Duration:    recording.Duration,
			EventCount:  recording.EventCount,
		})
	}

	// Apply limit
	if filter != nil && filter.Limit > 0 && len(summaries) > filter.Limit {
		summaries = summaries[:filter.Limit]
	}

	return summaries, nil
}

// LoadRecording loads a recording by ID
func LoadRecording(basePath, id string) (*Recording, error) {
	metadataPath := filepath.Join(basePath, id, "metadata.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read recording: %w", err)
	}

	var recording Recording
	if err := json.Unmarshal(data, &recording); err != nil {
		return nil, fmt.Errorf("failed to parse recording: %w", err)
	}

	return &recording, nil
}

// LoadStreamEvents loads stream events from a recording
func LoadStreamEvents(recording *Recording) ([]*StreamEvent, error) {
	file, err := os.Open(recording.StreamPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream file: %w", err)
	}
	defer func() { _ = file.Close() }()

	var events []*StreamEvent
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var event StreamEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue // Skip malformed events
		}
		events = append(events, &event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read stream: %w", err)
	}

	return events, nil
}

// DeleteRecording deletes a recording
func DeleteRecording(basePath, id string) error {
	recordingDir := filepath.Join(basePath, id)
	return os.RemoveAll(recordingDir)
}
