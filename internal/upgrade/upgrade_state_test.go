package upgrade

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestState(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "upgrade-state.json")

	// Test save and load
	t.Run("save and load", func(t *testing.T) {
		state := &State{
			PreviousVersion: "0.2.0",
			NewVersion:      "0.3.0",
			UpgradeStarted:  time.Now(),
			BackupPath:      "/path/to/backup",
			Status:          StatusPending,
		}

		if err := state.Save(statePath); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		loaded, err := LoadState(statePath)
		if err != nil {
			t.Fatalf("LoadState() error = %v", err)
		}

		if loaded.PreviousVersion != state.PreviousVersion {
			t.Errorf("PreviousVersion = %q, want %q", loaded.PreviousVersion, state.PreviousVersion)
		}
		if loaded.NewVersion != state.NewVersion {
			t.Errorf("NewVersion = %q, want %q", loaded.NewVersion, state.NewVersion)
		}
		if loaded.Status != state.Status {
			t.Errorf("Status = %q, want %q", loaded.Status, state.Status)
		}
	})

	// Test IsPending
	t.Run("IsPending", func(t *testing.T) {
		tests := []struct {
			status UpgradeStatus
			want   bool
		}{
			{StatusPending, true},
			{StatusDownloading, true},
			{StatusWaiting, true},
			{StatusInstalling, true},
			{StatusCompleted, false},
			{StatusFailed, false},
			{StatusRolledBack, false},
		}

		for _, tt := range tests {
			state := &State{Status: tt.status}
			if got := state.IsPending(); got != tt.want {
				t.Errorf("IsPending() for status %q = %v, want %v", tt.status, got, tt.want)
			}
		}
	})

	// Test NeedsRollback
	t.Run("NeedsRollback", func(t *testing.T) {
		state := &State{
			Status:     StatusFailed,
			BackupPath: "/path/to/backup",
		}
		if !state.NeedsRollback() {
			t.Error("NeedsRollback() = false, want true")
		}

		state.Status = StatusCompleted
		if state.NeedsRollback() {
			t.Error("NeedsRollback() = true for completed status, want false")
		}

		state.Status = StatusFailed
		state.BackupPath = ""
		if state.NeedsRollback() {
			t.Error("NeedsRollback() = true without backup path, want false")
		}
	})

	// Test ClearState
	t.Run("ClearState", func(t *testing.T) {
		// Create state file
		state := &State{Status: StatusCompleted}
		if err := state.Save(statePath); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		if err := ClearState(statePath); err != nil {
			t.Fatalf("ClearState() error = %v", err)
		}

		loaded, err := LoadState(statePath)
		if err != nil {
			t.Fatalf("LoadState() error = %v", err)
		}
		if loaded != nil {
			t.Error("LoadState() should return nil after ClearState()")
		}
	})
}

func TestNoOpTaskChecker(t *testing.T) {
	checker := &NoOpTaskChecker{}

	tasks := checker.GetRunningTaskIDs()
	if len(tasks) != 0 {
		t.Errorf("GetRunningTaskIDs() = %v, want empty slice", tasks)
	}

	ctx := context.Background()
	if err := checker.WaitForTasks(ctx, time.Second); err != nil {
		t.Errorf("WaitForTasks() error = %v", err)
	}
}

func TestLoadState_empty_path(t *testing.T) {
	// LoadState with empty path falls back to DefaultStatePath, which may or may not exist.
	// We just verify it doesn't panic.
	_, _ = LoadState("")
}

func TestState_JSON_roundtrip(t *testing.T) {
	s := &State{
		PreviousVersion: "1.0.0",
		NewVersion:      "2.0.0",
		UpgradeStarted:  time.Now().Truncate(time.Second),
		PendingTasks:    []string{"t1", "t2"},
		BackupPath:      "/tmp/backup",
		Status:          StatusWaiting,
		Error:           "some error",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var loaded State
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if loaded.PreviousVersion != s.PreviousVersion {
		t.Errorf("PreviousVersion = %q, want %q", loaded.PreviousVersion, s.PreviousVersion)
	}
	if loaded.Status != s.Status {
		t.Errorf("Status = %q, want %q", loaded.Status, s.Status)
	}
	if len(loaded.PendingTasks) != 2 {
		t.Errorf("PendingTasks len = %d, want 2", len(loaded.PendingTasks))
	}
}
