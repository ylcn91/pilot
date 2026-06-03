package executor

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestTDDPromptsConsumeTypedHandoffArtifacts(t *testing.T) {
	taskID := "GH-prompt"
	plan := pilotapi.NewHandoffArtifact(pilotapi.RolePlan, taskID, "PLAN", "")
	architect := pilotapi.NewHandoffArtifact(pilotapi.RoleArchitect, taskID, "DESIGN", plan.TraceHash)
	testAuthor := pilotapi.NewHandoffArtifact(pilotapi.RoleTestAuthor, taskID, "TestAdd", architect.TraceHash)

	testAuthorPrompt := buildTestAuthorAppendix(architect, "fallback design")
	for _, want := range []string{
		"## Handoff Artifact: architect",
		"Role: architect",
		"TraceHash: " + architect.TraceHash,
		"ParentHash: " + plan.TraceHash,
		"DESIGN",
	} {
		if !strings.Contains(testAuthorPrompt, want) {
			t.Fatalf("test-author prompt missing %q:\n%s", want, testAuthorPrompt)
		}
	}
	if strings.Contains(testAuthorPrompt, "fallback design") {
		t.Fatalf("test-author prompt used prose fallback despite typed artifact:\n%s", testAuthorPrompt)
	}

	implementerPrompt := buildImplementerAppendix(architect, testAuthor, []string{"TestAdd"}, "fix compile error")
	for _, want := range []string{
		"## Handoff Artifact: architect",
		"TraceHash: " + architect.TraceHash,
		"## Handoff Artifact: test-author",
		"TraceHash: " + testAuthor.TraceHash,
		"ParentHash: " + architect.TraceHash,
		"Tests that MUST pass: TestAdd.",
		"fix compile error",
	} {
		if !strings.Contains(implementerPrompt, want) {
			t.Fatalf("implementer prompt missing %q:\n%s", want, implementerPrompt)
		}
	}
}

func TestTDDPromptFallbackOnlyWhenArtifactMissing(t *testing.T) {
	prompt := buildTestAuthorAppendix(pilotapi.HandoffArtifact{}, "legacy prose design")

	if !strings.Contains(prompt, "## Handoff Artifact Fallback: architect") {
		t.Fatalf("prompt missing fallback heading:\n%s", prompt)
	}
	if !strings.Contains(prompt, "legacy prose design") {
		t.Fatalf("prompt missing fallback content:\n%s", prompt)
	}
	if strings.Contains(prompt, "TraceHash:") {
		t.Fatalf("prompt rendered typed metadata for a missing artifact:\n%s", prompt)
	}
}
