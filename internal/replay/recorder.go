package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
	"log/slog"
)

// Recorder captures execution events for later replay
type Recorder struct {
	id           string
	taskID       string
	projectPath  string
	basePath     string // ~/.pilot/recordings
	recording    *Recording
	streamFile   *os.File
	diffFiles    map[string]*FileDiff // Track file changes
	sequence     int
	currentPhase string
	phaseStart   time.Time
	mu           sync.Mutex
	log          *slog.Logger
}

// NewRecorder creates a recorder for a task execution
func NewRecorder(taskID, projectPath, basePath string) (*Recorder, error) {
	// Generate unique recording ID
	id := fmt.Sprintf("TG-%d", time.Now().UnixNano()/int64(time.Millisecond))

	// Create recording directory
	recordingDir := filepath.Join(basePath, id)
	if err := os.MkdirAll(recordingDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create recording directory: %w", err)
	}

	// Create diffs subdirectory
	diffsDir := filepath.Join(recordingDir, "diffs")
	if err := os.MkdirAll(diffsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create diffs directory: %w", err)
	}

	// Create stream file
	streamPath := filepath.Join(recordingDir, "stream.jsonl")
	streamFile, err := os.Create(streamPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream file: %w", err)
	}

	r := &Recorder{
		id:          id,
		taskID:      taskID,
		projectPath: projectPath,
		basePath:    basePath,
		streamFile:  streamFile,
		diffFiles:   make(map[string]*FileDiff),
		log:         logging.WithComponent("recorder"),
		recording: &Recording{
			ID:          id,
			TaskID:      taskID,
			ProjectPath: projectPath,
			StartTime:   time.Now(),
			StreamPath:  streamPath,
			DiffsPath:   diffsDir,
			SummaryPath: filepath.Join(recordingDir, "summary.md"),
			Metadata: &Metadata{
				Tags: make(map[string]string),
			},
			TokenUsage:   &TokenUsage{},
			PhaseTimings: make([]PhaseTiming, 0),
		},
	}

	r.log.Info("Recording started",
		slog.String("id", id),
		slog.String("task_id", taskID),
		slog.String("path", recordingDir),
	)

	return r, nil
}

// RecordEvent records a raw stream event
func (r *Recorder) RecordEvent(rawJSON string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sequence++
	event := StreamEvent{
		Timestamp: time.Now(),
		Sequence:  r.sequence,
		Raw:       rawJSON,
	}

	// Parse the event type and extract useful info
	parsed := r.parseEvent(rawJSON)
	if parsed != nil {
		event.Type = parsed.Type
		event.Parsed = parsed

		// Track token usage
		if parsed.InputTokens > 0 || parsed.OutputTokens > 0 {
			r.recording.TokenUsage.InputTokens += parsed.InputTokens
			r.recording.TokenUsage.OutputTokens += parsed.OutputTokens
			r.recording.TokenUsage.TotalTokens = r.recording.TokenUsage.InputTokens + r.recording.TokenUsage.OutputTokens
		}

		// Track phase changes
		if phase := r.detectPhase(parsed); phase != "" && phase != r.currentPhase {
			r.recordPhaseEnd()
			r.currentPhase = phase
			r.phaseStart = time.Now()
		}

		// Track file operations
		if parsed.FilePath != "" && parsed.FileOperation != "" {
			r.trackFileChange(parsed)
		}
	}

	// Write to stream file
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := r.streamFile.WriteString(string(eventJSON) + "\n"); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	r.recording.EventCount = r.sequence
	return nil
}

// SetMetadata sets recording metadata
func (r *Recorder) SetMetadata(key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording.Metadata.Tags[key] = value
}

// SetBranch sets the branch metadata
func (r *Recorder) SetBranch(branch string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording.Metadata.Branch = branch
}

// SetCommitSHA sets the commit SHA metadata
func (r *Recorder) SetCommitSHA(sha string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording.Metadata.CommitSHA = sha
}

// SetPRUrl sets the PR URL metadata
func (r *Recorder) SetPRUrl(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording.Metadata.PRUrl = url
}

// SetNavigator sets the Navigator flag
func (r *Recorder) SetNavigator(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording.Metadata.HasNavigator = enabled
}

// SetModel sets the model name
func (r *Recorder) SetModel(model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording.Metadata.ModelName = model
}

// Finish completes the recording
func (r *Recorder) Finish(status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Record final phase
	r.recordPhaseEnd()

	// Update recording metadata
	r.recording.EndTime = time.Now()
	r.recording.Duration = r.recording.EndTime.Sub(r.recording.StartTime)
	r.recording.Status = status

	// Calculate estimated cost
	r.recording.TokenUsage.EstimatedCostUSD = r.estimateCost()

	// Close stream file
	if err := r.streamFile.Close(); err != nil {
		r.log.Error("Failed to close stream file", slog.Any("error", err))
	}

	// Save metadata
	if err := r.saveMetadata(); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	// Save diffs
	if err := r.saveDiffs(); err != nil {
		r.log.Warn("Failed to save diffs", slog.Any("error", err))
	}

	// Generate summary
	if err := r.generateSummary(); err != nil {
		r.log.Warn("Failed to generate summary", slog.Any("error", err))
	}

	r.log.Info("Recording finished",
		slog.String("id", r.id),
		slog.String("status", status),
		slog.Duration("duration", r.recording.Duration),
		slog.Int("events", r.recording.EventCount),
	)

	return nil
}

// GetRecordingID returns the recording ID
func (r *Recorder) GetRecordingID() string {
	return r.id
}

// GetRecording returns the recording metadata
func (r *Recorder) GetRecording() *Recording {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording
}
