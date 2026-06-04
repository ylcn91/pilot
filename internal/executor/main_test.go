package executor

import (
	"os"
	"testing"
)

// TestMain pins the package-level parentStateResolver seam to a deterministic
// no-op for the whole test binary. In production it is wired to
// queryParentDoneViaGitHub, which shells out to `gh issue view`; letting that
// fire from tests would (a) make GH-* fixtures non-hermetic and (b) trip
// fake-`gh`-on-PATH guardrail tests that assert no gh call happened.
//
// Individual tests that want to exercise the live-resolution path (see
// epic_test_recover_test.go) override parentStateResolver explicitly and
// restore it via t.Cleanup. This replaces the former in-production
// `testing.Testing()` guard (#32) with a test-only seam.
func TestMain(m *testing.M) {
	parentStateResolver = func(taskID, dir string) bool { return false }
	os.Exit(m.Run())
}
