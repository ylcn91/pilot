package executor

import (
	"regexp"
	"strings"
)

// TDD role prompt appendices. Each appendix is appended to the shared base prompt
// (BuildPrompt + BuildGuidancePreamble) so every role backend gets the same
// .agent priming and task context, then a role-specific instruction block on top.
//
// The appendices are intentionally terse: the base prompt already carries the
// ticket, project context, and guidance. These blocks only redefine the role's
// objective and its output contract (notably TESTS_ADDED for TEST-AUTHOR).

// buildArchitectAppendix instructs the ARCHITECT role to produce a read-only
// design. It must NOT write code or tests and MUST NOT commit; its output is
// advisory context threaded into the later roles.
func buildArchitectAppendix() string {
	return strings.TrimSpace(`
## TDD Role: ARCHITECT (read-only design)

You are the ARCHITECT in a test-driven-development pipeline. Produce a concise
design for the change described above. This phase is READ-ONLY:

- Do NOT modify, create, or delete any files.
- Do NOT run any command that mutates the repository.
- Do NOT commit.

Describe the intended public surface (functions/types/signatures), the behaviors
that must be tested, and the edge cases the tests should cover. Keep it short and
concrete — it will be handed to a TEST-AUTHOR and an IMPLEMENTER.
`)
}

// buildTestAuthorAppendix instructs the TEST-AUTHOR role to write FAILING tests
// and commit them, then emit the TESTS_ADDED contract so the RED/GREEN gates can
// scope to exactly those test names. The optional design is the ARCHITECT output.
func buildTestAuthorAppendix(design string) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(`
## TDD Role: TEST-AUTHOR (write FAILING tests, then commit)

You are the TEST-AUTHOR in a test-driven-development pipeline. Write tests that
capture the required behavior and FAIL against the current (unimplemented) code:

- Write tests ONLY. Do NOT implement the production change — the tests MUST fail
  now and be made to pass by a later IMPLEMENTER role.
- Do NOT weaken assertions or write trivially-passing tests.
- COMMIT the new tests (a fresh commit is required; an empty working tree will be
  rejected and you will be re-prompted).

When done, emit a single line listing the test function names you added, exactly:

    TESTS_ADDED: TestFoo, TestBar

Use the real function names. The pipeline scopes the RED/GREEN gates to exactly
these names, so an accurate list is required.
`))
	if d := strings.TrimSpace(design); d != "" {
		sb.WriteString("\n\n## Design (from ARCHITECT, advisory)\n\n")
		sb.WriteString(d)
	}
	return sb.String()
}

// buildImplementerAppendix instructs the IMPLEMENTER role to make the authored
// tests pass and commit. design is the ARCHITECT output (advisory); testNames are
// the tests the GREEN gate will assert; feedback is the prior gate's failure text
// on a retry (empty on the first attempt).
func buildImplementerAppendix(design string, testNames []string, feedback string) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(`
## TDD Role: IMPLEMENTER (make the failing tests pass, then commit)

You are the IMPLEMENTER in a test-driven-development pipeline. The TEST-AUTHOR has
committed tests that currently FAIL. Implement the production change so those
tests pass:

- Do NOT modify, delete, or weaken the authored tests — make them pass as written.
- COMMIT your implementation (a fresh commit is required).
`))
	if len(testNames) > 0 {
		sb.WriteString("\n\nTests that MUST pass: ")
		sb.WriteString(strings.Join(testNames, ", "))
		sb.WriteString(".")
	}
	if d := strings.TrimSpace(design); d != "" {
		sb.WriteString("\n\n## Design (from ARCHITECT, advisory)\n\n")
		sb.WriteString(d)
	}
	if f := strings.TrimSpace(feedback); f != "" {
		sb.WriteString("\n\n## Previous GREEN gate failure (fix this)\n\n")
		sb.WriteString(f)
	}
	return sb.String()
}

// buildTDDRolePrompt joins the shared base prompt with a role appendix.
func buildTDDRolePrompt(base, appendix string) string {
	base = strings.TrimRight(base, "\n")
	if appendix == "" {
		return base
	}
	return base + "\n\n" + appendix
}

var tddTestsAddedLineRe = regexp.MustCompile(`(?im)^\s*TESTS_ADDED:\s*(.+)$`)

// extractTestsAdded scans backend output for the TEST-AUTHOR's TESTS_ADDED line
// and returns the parsed, de-duped test names. It returns nil when no line is
// present or no valid names are found. The last TESTS_ADDED line wins so a role
// that re-emits the contract after a correction overrides an earlier draft.
func extractTestsAdded(output string) []string {
	matches := tddTestsAddedLineRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return nil
	}
	payload := matches[len(matches)-1][1]
	return parseTestsAdded(payload)
}
