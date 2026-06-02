package autopilot

import (
	"testing"
	"time"
)

func TestStateStore_SaveAndLoadPRState(t *testing.T) {
	store := newTestStateStore(t)

	pr := &PRState{
		PRNumber:        42,
		PRURL:           "https://github.com/owner/repo/pull/42",
		IssueNumber:     10,
		BranchName:      "pilot/GH-10",
		HeadSHA:         "abc123def456",
		Stage:           StageWaitingCI,
		CIStatus:        CIRunning,
		LastChecked:     time.Now().Truncate(time.Second),
		CIWaitStartedAt: time.Now().Add(-5 * time.Minute).Truncate(time.Second),
		MergeAttempts:   1,
		Error:           "",
		CreatedAt:       time.Now().Add(-10 * time.Minute).Truncate(time.Second),
		ReleaseVersion:  "",
		ReleaseBumpType: BumpNone,
	}

	// Save
	if err := store.SavePRState(pr); err != nil {
		t.Fatalf("SavePRState failed: %v", err)
	}

	// Load single
	loaded, err := store.GetPRState(42)
	if err != nil {
		t.Fatalf("GetPRState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("GetPRState returned nil")
	}

	if loaded.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", loaded.PRNumber)
	}
	if loaded.PRURL != pr.PRURL {
		t.Errorf("PRURL = %s, want %s", loaded.PRURL, pr.PRURL)
	}
	if loaded.IssueNumber != 10 {
		t.Errorf("IssueNumber = %d, want 10", loaded.IssueNumber)
	}
	if loaded.BranchName != "pilot/GH-10" {
		t.Errorf("BranchName = %s, want pilot/GH-10", loaded.BranchName)
	}
	if loaded.HeadSHA != "abc123def456" {
		t.Errorf("HeadSHA = %s, want abc123def456", loaded.HeadSHA)
	}
	if loaded.Stage != StageWaitingCI {
		t.Errorf("Stage = %s, want %s", loaded.Stage, StageWaitingCI)
	}
	if loaded.CIStatus != CIRunning {
		t.Errorf("CIStatus = %s, want %s", loaded.CIStatus, CIRunning)
	}
	if loaded.MergeAttempts != 1 {
		t.Errorf("MergeAttempts = %d, want 1", loaded.MergeAttempts)
	}
}

func TestStateStore_LoadAllPRStates(t *testing.T) {
	store := newTestStateStore(t)

	// Save multiple PRs
	for _, num := range []int{1, 2, 3} {
		pr := &PRState{
			PRNumber:   num,
			PRURL:      "https://github.com/owner/repo/pull/1",
			BranchName: "pilot/GH-1",
			Stage:      StagePRCreated,
			CIStatus:   CIPending,
			CreatedAt:  time.Now(),
		}
		if err := store.SavePRState(pr); err != nil {
			t.Fatalf("SavePRState(%d) failed: %v", num, err)
		}
	}

	states, err := store.LoadAllPRStates()
	if err != nil {
		t.Fatalf("LoadAllPRStates failed: %v", err)
	}
	if len(states) != 3 {
		t.Errorf("got %d states, want 3", len(states))
	}
}

func TestStateStore_UpdatePRState(t *testing.T) {
	store := newTestStateStore(t)

	pr := &PRState{
		PRNumber:   42,
		PRURL:      "https://github.com/owner/repo/pull/42",
		BranchName: "pilot/GH-10",
		Stage:      StagePRCreated,
		CIStatus:   CIPending,
		CreatedAt:  time.Now(),
	}

	if err := store.SavePRState(pr); err != nil {
		t.Fatalf("initial SavePRState failed: %v", err)
	}

	// Update stage
	pr.Stage = StageWaitingCI
	pr.CIStatus = CIRunning
	pr.CIWaitStartedAt = time.Now()
	if err := store.SavePRState(pr); err != nil {
		t.Fatalf("update SavePRState failed: %v", err)
	}

	loaded, err := store.GetPRState(42)
	if err != nil {
		t.Fatalf("GetPRState failed: %v", err)
	}
	if loaded.Stage != StageWaitingCI {
		t.Errorf("Stage = %s, want %s", loaded.Stage, StageWaitingCI)
	}
	if loaded.CIStatus != CIRunning {
		t.Errorf("CIStatus = %s, want %s", loaded.CIStatus, CIRunning)
	}
}

func TestStateStore_RemovePRState(t *testing.T) {
	store := newTestStateStore(t)

	pr := &PRState{
		PRNumber:   42,
		PRURL:      "https://github.com/owner/repo/pull/42",
		BranchName: "pilot/GH-10",
		Stage:      StagePRCreated,
		CIStatus:   CIPending,
		CreatedAt:  time.Now(),
	}

	if err := store.SavePRState(pr); err != nil {
		t.Fatalf("SavePRState failed: %v", err)
	}

	if err := store.RemovePRState(42); err != nil {
		t.Fatalf("RemovePRState failed: %v", err)
	}

	loaded, err := store.GetPRState(42)
	if err != nil {
		t.Fatalf("GetPRState failed: %v", err)
	}
	if loaded != nil {
		t.Error("expected nil after removal, got non-nil")
	}
}
