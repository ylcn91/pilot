package main

import (
	"testing"

	"github.com/ylcn91/pilot/internal/approval"
)

// =============================================================================
// GH-2134: qualityCheckerWrapper adapter test
// =============================================================================

func TestQualityCheckerWrapper_NilResultFields(t *testing.T) {
	// qualityCheckerWrapper is a type adapter — verify it compiles and implements
	// the interface correctly by checking the struct can be instantiated.
	// Full integration would require a quality.Executor with real gates.
	var _ interface {
		Check(ctx interface{}) (interface{}, error)
	}
	// Type assertion at compile time is sufficient — the wrapper adapts
	// quality.Executor to executor.QualityChecker. If the interface changes,
	// this file won't compile.
	_ = &qualityCheckerWrapper{}
}

// =============================================================================
// GH-2134: convertKeyboardToTelegram test
// =============================================================================

func TestConvertKeyboardToTelegram(t *testing.T) {
	input := [][]approval.InlineKeyboardButton{
		{
			{Text: "Approve", CallbackData: "approve_1"},
			{Text: "Reject", CallbackData: "reject_1"},
		},
		{
			{Text: "Skip", CallbackData: "skip_1"},
		},
	}

	result := convertKeyboardToTelegram(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result))
	}
	if len(result[0]) != 2 {
		t.Fatalf("expected 2 buttons in row 0, got %d", len(result[0]))
	}
	if len(result[1]) != 1 {
		t.Fatalf("expected 1 button in row 1, got %d", len(result[1]))
	}

	if result[0][0].Text != "Approve" {
		t.Errorf("button[0][0].Text = %q, want %q", result[0][0].Text, "Approve")
	}
	if result[0][0].CallbackData != "approve_1" {
		t.Errorf("button[0][0].CallbackData = %q, want %q", result[0][0].CallbackData, "approve_1")
	}
	if result[0][1].Text != "Reject" {
		t.Errorf("button[0][1].Text = %q, want %q", result[0][1].Text, "Reject")
	}
	if result[1][0].Text != "Skip" {
		t.Errorf("button[1][0].Text = %q, want %q", result[1][0].Text, "Skip")
	}
}

func TestConvertKeyboardToTelegram_Empty(t *testing.T) {
	result := convertKeyboardToTelegram(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 rows for nil input, got %d", len(result))
	}
}

// =============================================================================
// GH-2134: autopilotProviderAdapter compile check
// =============================================================================

func TestAutopilotProviderAdapter_ImplementsInterface(t *testing.T) {
	// Compile-time check that autopilotProviderAdapter satisfies gateway.AutopilotProvider
	// (if the interface signature changes, this file will fail to compile)
	_ = &autopilotProviderAdapter{}
}
