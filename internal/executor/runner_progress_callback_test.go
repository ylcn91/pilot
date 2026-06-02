package executor

import (
	"testing"
)

// TestAddProgressCallback verifies that multiple named callbacks can be registered
// and all receive progress updates (GH-149 fix)
func TestAddProgressCallback(t *testing.T) {
	runner := NewRunner()

	var callback1Received, callback2Received, legacyReceived bool
	var callback1TaskID, callback2TaskID, legacyTaskID string

	// Register legacy callback (simulates Telegram handler)
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		legacyReceived = true
		legacyTaskID = taskID
	})

	// Register named callbacks (simulates dashboard)
	runner.AddProgressCallback("dashboard", func(taskID, phase string, progress int, message string) {
		callback1Received = true
		callback1TaskID = taskID
	})

	runner.AddProgressCallback("monitor", func(taskID, phase string, progress int, message string) {
		callback2Received = true
		callback2TaskID = taskID
	})

	// Emit progress
	runner.EmitProgress("TASK-TEST", "Testing", 50, "Test message")

	// All callbacks should receive the progress
	if !legacyReceived {
		t.Error("Legacy OnProgress callback should be called")
	}
	if !callback1Received {
		t.Error("Named callback 'dashboard' should be called")
	}
	if !callback2Received {
		t.Error("Named callback 'monitor' should be called")
	}

	// Task IDs should match
	if legacyTaskID != "TASK-TEST" {
		t.Errorf("Legacy taskID = %q, want TASK-TEST", legacyTaskID)
	}
	if callback1TaskID != "TASK-TEST" {
		t.Errorf("Dashboard taskID = %q, want TASK-TEST", callback1TaskID)
	}
	if callback2TaskID != "TASK-TEST" {
		t.Errorf("Monitor taskID = %q, want TASK-TEST", callback2TaskID)
	}
}

// TestRemoveProgressCallback verifies that named callbacks can be removed
func TestRemoveProgressCallback(t *testing.T) {
	runner := NewRunner()

	var received bool

	runner.AddProgressCallback("test", func(taskID, phase string, progress int, message string) {
		received = true
	})

	// Remove the callback
	runner.RemoveProgressCallback("test")

	// Emit progress - callback should NOT be called
	runner.EmitProgress("TASK-TEST", "Testing", 50, "Test message")

	if received {
		t.Error("Removed callback should not be called")
	}
}

// TestProgressCallbackIsolation verifies that OnProgress(nil) doesn't affect named callbacks
// This is the core fix for GH-149
func TestProgressCallbackIsolation(t *testing.T) {
	runner := NewRunner()

	var namedReceived bool

	// Register named callback (simulates dashboard)
	runner.AddProgressCallback("dashboard", func(taskID, phase string, progress int, message string) {
		namedReceived = true
	})

	// Set and clear legacy callback (simulates Telegram handler behavior)
	runner.OnProgress(func(taskID, phase string, progress int, message string) {})
	runner.OnProgress(nil) // This is what Telegram handler does after execution

	// Emit progress - named callback should still work
	runner.EmitProgress("TASK-TEST", "Testing", 50, "Test message")

	if !namedReceived {
		t.Error("Named callback should still be called after OnProgress(nil)")
	}
}

// TestSuppressProgressLogs verifies that slog output can be suppressed
// This is the fix for GH-152: show visual progress instead of log spam
func TestSuppressProgressLogs(t *testing.T) {
	runner := NewRunner()

	var callbackReceived bool
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		callbackReceived = true
	})

	// Suppress logs (simulates visual progress mode)
	runner.SuppressProgressLogs(true)

	// Emit progress - callback should still be called even when logs suppressed
	runner.EmitProgress("TASK-TEST", "Testing", 50, "Test message")

	if !callbackReceived {
		t.Error("Callback should be called even when progress logs are suppressed")
	}

	// Verify the flag is set correctly
	if !runner.suppressProgressLogs {
		t.Error("suppressProgressLogs should be true after SuppressProgressLogs(true)")
	}

	// Reset suppression
	runner.SuppressProgressLogs(false)
	if runner.suppressProgressLogs {
		t.Error("suppressProgressLogs should be false after SuppressProgressLogs(false)")
	}
}
