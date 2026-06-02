package executor

import (
	"strings"
	"testing"
)

func TestBuildSelfReviewPromptContainsLintCheck(t *testing.T) {
	runner := NewRunner()
	task := &Task{
		ID:          "GH-1797",
		Title:       "Add lint check to self-review",
		Description: "Test self-review lint section",
	}

	prompt := runner.buildSelfReviewPrompt(task)

	// Verify self-review contains lint check section
	if !strings.Contains(prompt, "### 8. Lint Check") {
		t.Error("Self-review prompt should contain '### 8. Lint Check' section")
	}
	if !strings.Contains(prompt, "golangci-lint run --new-from-rev=origin/main") {
		t.Error("Self-review prompt should contain golangci-lint command")
	}
	if !strings.Contains(prompt, "unchecked return values") {
		t.Error("Self-review prompt should mention unchecked return values as common issue")
	}
}

func TestBuildSelfReviewPromptWithAcceptanceCriteria(t *testing.T) {
	runner := NewRunner()

	t.Run("AC section appears when criteria present", func(t *testing.T) {
		task := &Task{
			ID:    "GH-1966",
			Title: "Add AC verification",
			AcceptanceCriteria: []string{
				"Function returns error on invalid input",
				"Unit tests cover edge cases",
				"Documentation updated",
			},
		}

		prompt := runner.buildSelfReviewPrompt(task)

		if !strings.Contains(prompt, "### 9. Acceptance Criteria Verification") {
			t.Error("Self-review prompt should contain AC verification section when ACs present")
		}
	})

	t.Run("each AC listed individually", func(t *testing.T) {
		task := &Task{
			ID:    "GH-1966",
			Title: "Add AC verification",
			AcceptanceCriteria: []string{
				"Function returns error on invalid input",
				"Unit tests cover edge cases",
			},
		}

		prompt := runner.buildSelfReviewPrompt(task)

		if !strings.Contains(prompt, "**AC1**: Function returns error on invalid input") {
			t.Error("Self-review prompt should list first AC individually")
		}
		if !strings.Contains(prompt, "**AC2**: Unit tests cover edge cases") {
			t.Error("Self-review prompt should list second AC individually")
		}
		if !strings.Contains(prompt, "MET / UNMET (cite diff evidence)") {
			t.Error("Self-review prompt should instruct MET/UNMET with evidence")
		}
	})

	t.Run("AC section omitted when empty", func(t *testing.T) {
		task := &Task{
			ID:                 "GH-1966",
			Title:              "No ACs",
			AcceptanceCriteria: []string{},
		}

		prompt := runner.buildSelfReviewPrompt(task)

		if strings.Contains(prompt, "Acceptance Criteria Verification") {
			t.Error("Self-review prompt should NOT contain AC section when ACs are empty")
		}
	})

	t.Run("AC section omitted when nil", func(t *testing.T) {
		task := &Task{
			ID:                 "GH-1966",
			Title:              "Nil ACs",
			AcceptanceCriteria: nil,
		}

		prompt := runner.buildSelfReviewPrompt(task)

		if strings.Contains(prompt, "Acceptance Criteria Verification") {
			t.Error("Self-review prompt should NOT contain AC section when ACs are nil")
		}
	})
}

// B2e/A3/B7: self-review carries the full description, scope-creep critique,
// and tiered standards markers.
func TestBuildSelfReviewPromptScopeAndTiering(t *testing.T) {
	runner := NewRunner()
	longDesc := strings.Repeat("spec detail line. ", 60) // > 500 chars
	task := &Task{
		ID:          "GH-X",
		Title:       "Self-review additions",
		Description: longDesc,
	}

	prompt := runner.buildSelfReviewPrompt(task)

	if !strings.Contains(prompt, longDesc) {
		t.Error("full description should be included (no 500-char truncation)")
	}
	if strings.Contains(prompt, "(excerpt)") {
		t.Error("description should no longer be labeled an excerpt")
	}
	if !strings.Contains(prompt, "SCOPE_CREEP: <symbol>") {
		t.Error("self-review should include the scope-creep critique marker")
	}
	if !strings.Contains(prompt, "STANDARD_VIOLATION: <tier> <rule>") {
		t.Error("self-review should include the tiered standards marker")
	}
	for _, tier := range []string{"blocker", "must", "nice"} {
		if !strings.Contains(prompt, tier) {
			t.Errorf("self-review should mention tier %q", tier)
		}
	}
}
