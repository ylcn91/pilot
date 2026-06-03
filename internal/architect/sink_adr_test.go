package architect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugifyADR(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "Refactor RFC", "refactor-rfc"},
		{"already_slug", "break-cycles", "break-cycles"},
		{"uppercase", "BREAK CYCLES", "break-cycles"},
		{"punctuation_collapses", "Fix: layer/violation (now!)", "fix-layer-violation-now"},
		{"leading_trailing_sep", "  --Hello, World--  ", "hello-world"},
		{"runs_collapse", "a___b...c   d", "a-b-c-d"},
		{"traversal_neutralised", "../../etc/passwd", "etc-passwd"},
		{"separators_stripped", "a/b\\c", "a-b-c"},
		{"empty_is_untitled", "", "untitled"},
		{"all_punct_is_untitled", "!!!___///", "untitled"},
		{"unicode_dropped", "café ☕ plan", "caf-plan"},
		{"digits_kept", "v2 migration 99", "v2-migration-99"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SlugifyADR(tt.in); got != tt.want {
				t.Fatalf("SlugifyADR(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSlugifyADR_Capped proves a very long title is truncated to maxSlugLen and
// never left with a trailing hyphen, and the slug stays within [a-z0-9-].
func TestSlugifyADR_Capped(t *testing.T) {
	long := strings.Repeat("word ", 60) // 300 chars of "word " repeats
	got := SlugifyADR(long)
	if len(got) > maxSlugLen {
		t.Fatalf("slug length = %d, want <= %d", len(got), maxSlugLen)
	}
	if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
		t.Fatalf("slug must not have leading/trailing hyphen: %q", got)
	}
	for _, r := range got {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			t.Fatalf("slug %q contains illegal rune %q", got, r)
		}
	}
}

func TestSlugifyADR_Deterministic(t *testing.T) {
	in := "Decompose the Mega Handler (phase 2)"
	first := SlugifyADR(in)
	for i := 0; i < 5; i++ {
		if again := SlugifyADR(in); again != first {
			t.Fatalf("SlugifyADR non-deterministic: %q != %q", again, first)
		}
	}
}

func TestADRPath(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		slug string
		want string
	}{
		{"plain", "/tmp/sys", "break-cycles", filepath.Join("/tmp/sys", "rfc_break-cycles.md")},
		{"slug_resanitised", "/tmp/sys", "Break Cycles", filepath.Join("/tmp/sys", "rfc_break-cycles.md")},
		{"traversal_contained", "/tmp/sys", "../../etc/passwd", filepath.Join("/tmp/sys", "rfc_etc-passwd.md")},
		{"empty_slug_untitled", "/tmp/sys", "", filepath.Join("/tmp/sys", "rfc_untitled.md")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ADRPath(tt.dir, tt.slug); got != tt.want {
				t.Fatalf("ADRPath(%q,%q) = %q, want %q", tt.dir, tt.slug, got, tt.want)
			}
		})
	}
}

// TestADRPath_NeverEscapesDir proves a hostile slug never resolves outside dir:
// the cleaned path's directory is exactly dir for every adversarial input.
func TestADRPath_NeverEscapesDir(t *testing.T) {
	dir := "/tmp/adr-root"
	for _, slug := range []string{"../../escape", "/abs/path", "..", "a/../../b", "....//x"} {
		got := ADRPath(dir, slug)
		if filepath.Dir(got) != dir {
			t.Fatalf("ADRPath(%q,%q) = %q escaped dir %q", dir, slug, got, dir)
		}
		if !strings.HasPrefix(filepath.Base(got), adrFilePrefix) {
			t.Fatalf("ADRPath(%q,%q) base %q lost the %q prefix", dir, slug, filepath.Base(got), adrFilePrefix)
		}
	}
}

// TestWriteADR_WritesExpectedFile proves WriteADR writes the content verbatim to
// the rfc_<slug>.md path inside a freshly-created dir and returns that path.
func TestWriteADR_WritesExpectedFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "system")
	content := "---\ntitle: \"X\"\n---\n\n# X\n\n## Problem Statement\n\nbody\n"

	path, err := WriteADR(dir, "My RFC", content)
	if err != nil {
		t.Fatalf("WriteADR: %v", err)
	}
	want := filepath.Join(dir, "rfc_my-rfc.md")
	if path != want {
		t.Fatalf("WriteADR path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != content {
		t.Fatalf("written content mismatch:\n got: %q\nwant: %q", got, content)
	}
}

// TestWriteADR_CreatesMissingDir proves the parent directory tree is created when
// absent (MkdirAll), so the lens never has to pre-make .agent/system.
func TestWriteADR_CreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("precondition: dir should not exist yet")
	}
	if _, err := WriteADR(dir, "slug", "x"); err != nil {
		t.Fatalf("WriteADR: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("dir not created: err=%v", err)
	}
}

// TestWriteADR_Idempotent proves a second WriteADR with the same slug overwrites
// the first file in place (same path, new content) rather than creating a second
// file or appending.
func TestWriteADR_Idempotent(t *testing.T) {
	dir := t.TempDir()
	p1, err := WriteADR(dir, "doc", "first")
	if err != nil {
		t.Fatalf("first WriteADR: %v", err)
	}
	p2, err := WriteADR(dir, "doc", "second")
	if err != nil {
		t.Fatalf("second WriteADR: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("idempotent path mismatch: %q != %q", p1, p2)
	}
	got, _ := os.ReadFile(p2)
	if string(got) != "second" {
		t.Fatalf("overwrite content = %q, want %q", got, "second")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("idempotent write produced %d files, want 1", len(entries))
	}
}

// TestWriteADR_NoTraversal proves a slug carrying ".." stays contained: the file
// lands inside dir and nothing is written above it.
func TestWriteADR_NoTraversal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "system")
	if _, err := WriteADR(dir, "../../escape-attempt", "payload"); err != nil {
		t.Fatalf("WriteADR: %v", err)
	}
	// The escape file must NOT exist at the traversal target.
	if _, err := os.Stat(filepath.Join(root, "escape-attempt")); !os.IsNotExist(err) {
		t.Fatalf("traversal escaped: file written above dir")
	}
	// It must exist, contained, inside dir.
	if _, err := os.Stat(filepath.Join(dir, "rfc_escape-attempt.md")); err != nil {
		t.Fatalf("contained file missing: %v", err)
	}
}

func TestWriteADR_EmptyDirErrors(t *testing.T) {
	if _, err := WriteADR("   ", "slug", "x"); err == nil {
		t.Fatal("WriteADR with empty dir must error")
	}
}

// TestWriteADR_UnwritableDirErrors proves an MkdirAll failure surfaces as an
// error rather than a panic (a file occupying the dir path).
func TestWriteADR_UnwritableDirErrors(t *testing.T) {
	root := t.TempDir()
	clash := filepath.Join(root, "occupied")
	if err := os.WriteFile(clash, []byte("file, not dir"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// MkdirAll under a path that is a regular file must fail.
	if _, err := WriteADR(filepath.Join(clash, "sub"), "slug", "x"); err == nil {
		t.Fatal("WriteADR under a file-occupied path must error")
	}
}
