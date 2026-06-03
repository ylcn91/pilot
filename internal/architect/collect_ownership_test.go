package architect

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// authorRunner is a mock commandRunner that returns a canned `git log --format=%an`
// stream per path. A path absent from logs yields an error+empty output (the
// untracked / git-absent path); fail forces every call to error.
type authorRunner struct {
	logs  map[string]string
	fail  bool
	calls int
}

func (r *authorRunner) run(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	r.calls++
	if r.fail || name != "git" {
		return nil, errors.New("git unavailable")
	}
	path := args[len(args)-1]
	if log, ok := r.logs[path]; ok {
		return []byte(log), nil
	}
	return nil, errors.New("untracked")
}

func TestTopAuthorFromLog(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want string
	}{
		{"empty", "", ""},
		{"single", "Ada\n", "Ada"},
		{"majority", "Ada\nBob\nAda\nAda\nBob\n", "Ada"},
		{"tie_breaks_lexicographically", "Bob\nAda\n", "Ada"},
		{"blank_lines_ignored", "\n  \nGrace\nGrace\n\n", "Grace"},
		{"whitespace_trimmed", "  Ada  \nAda\nBob\n", "Ada"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := topAuthorFromLog(tt.log); got != tt.want {
				t.Fatalf("topAuthorFromLog(%q) = %q, want %q", tt.log, got, tt.want)
			}
		})
	}
}

func TestOwnershipCollector_TopAuthor_Mock(t *testing.T) {
	r := &authorRunner{logs: map[string]string{
		"a.go": "Ada\nAda\nBob\n",
		"b.go": "",
	}}
	c := newOwnershipCollector(r.run)

	if got := c.TopAuthor(context.Background(), "/proj", "a.go"); got != "Ada" {
		t.Fatalf("a.go owner = %q, want Ada", got)
	}
	// Tracked but empty history -> no owner.
	if got := c.TopAuthor(context.Background(), "/proj", "b.go"); got != "" {
		t.Fatalf("b.go owner = %q, want empty", got)
	}
	// Untracked path -> git errors with empty output -> no owner.
	if got := c.TopAuthor(context.Background(), "/proj", "missing.go"); got != "" {
		t.Fatalf("missing.go owner = %q, want empty", got)
	}
	// Empty path is rejected before shelling.
	before := r.calls
	if got := c.TopAuthor(context.Background(), "/proj", "  "); got != "" {
		t.Fatalf("blank path owner = %q, want empty", got)
	}
	if r.calls != before {
		t.Fatalf("blank path should not shell git (calls %d -> %d)", before, r.calls)
	}
}

func TestOwnershipCollector_GitAbsentFallback(t *testing.T) {
	r := &authorRunner{fail: true}
	c := newOwnershipCollector(r.run)
	if got := c.TopAuthor(context.Background(), "/proj", "a.go"); got != "" {
		t.Fatalf("git-absent owner = %q, want empty (graceful)", got)
	}
	owners := c.Owners(context.Background(), "/proj", []string{"a.go", "b.go"})
	if len(owners) != 0 {
		t.Fatalf("git-absent Owners = %v, want empty map", owners)
	}
}

func TestOwnershipCollector_NilRunnerAndNilReceiver(t *testing.T) {
	var nilC *OwnershipCollector
	if got := nilC.TopAuthor(context.Background(), "/p", "a.go"); got != "" {
		t.Fatalf("nil receiver owner = %q, want empty", got)
	}
	c := &OwnershipCollector{run: nil}
	if got := c.TopAuthor(context.Background(), "/p", "a.go"); got != "" {
		t.Fatalf("nil run owner = %q, want empty", got)
	}
	if owners := c.Owners(context.Background(), "/p", []string{"a.go"}); len(owners) != 0 {
		t.Fatalf("nil run Owners = %v, want empty", owners)
	}
}

func TestOwnershipCollector_Owners_DedupAndOmitUnknown(t *testing.T) {
	r := &authorRunner{logs: map[string]string{
		"a.go": "Ada\nAda\n",
		"b.go": "Bob\n",
	}}
	c := newOwnershipCollector(r.run)
	got := c.Owners(context.Background(), "/proj", []string{"a.go", "a.go", "b.go", "c.go", "  "})

	if len(got) != 2 {
		t.Fatalf("Owners = %v, want 2 entries (unknown c.go omitted, blank skipped)", got)
	}
	if got["a.go"] != "Ada" || got["b.go"] != "Bob" {
		t.Fatalf("Owners = %v, want a.go=Ada b.go=Bob", got)
	}
	// a.go probed once despite appearing twice (dedup).
	if r.calls != 3 { // a.go, b.go, c.go
		t.Fatalf("expected 3 git probes (a,b,c deduped), got %d", r.calls)
	}
}

func TestOwnershipCollector_Owners_ContextCancelled(t *testing.T) {
	r := &authorRunner{logs: map[string]string{"a.go": "Ada\n"}}
	c := newOwnershipCollector(r.run)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := c.Owners(ctx, "/proj", []string{"a.go", "b.go"})
	if len(got) != 0 {
		t.Fatalf("cancelled Owners = %v, want empty (short-circuit)", got)
	}
}

// TestOwnershipCollector_RealGitFixture exercises the production
// execCommandRunner against a real temp repo where one author dominates a file's
// history, proving the end-to-end top-author resolution without mocks.
func TestOwnershipCollector_RealGitFixture(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)

	owned := filepath.Join(root, "owned.go")
	writeOwnedCommit(t, root, owned, "package p\n", "Ada", "ada@x")
	writeOwnedCommit(t, root, owned, "package p\n// 2\n", "Ada", "ada@x")
	writeOwnedCommit(t, root, owned, "package p\n// 3\n", "Bob", "bob@x")

	c := NewOwnershipCollector()
	if got := c.TopAuthor(context.Background(), root, "owned.go"); got != "Ada" {
		t.Fatalf("real-git top author = %q, want Ada (2 of 3 commits)", got)
	}

	owners := c.Owners(context.Background(), root, []string{"owned.go", "ghost.go"})
	if owners["owned.go"] != "Ada" {
		t.Fatalf("Owners[owned.go] = %q, want Ada", owners["owned.go"])
	}
	if _, ok := owners["ghost.go"]; ok {
		t.Fatalf("untracked ghost.go must be omitted from Owners")
	}
}

// writeOwnedCommit writes content to abs and commits it authored by name/email,
// so a fixture can attribute commits to specific authors.
func writeOwnedCommit(t *testing.T, dir, abs, content, name, email string) {
	t.Helper()
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
	runGit(t, dir, "add", "-A")
	cmd := exec.Command("git",
		"-c", "user.name="+name, "-c", "user.email="+email,
		"commit", "-q", "-m", "change by "+name)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit (%s): %v\n%s", name, err, out)
	}
}
