package architect

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestTODOCollector_FindsAllMarkers(t *testing.T) {
	dir := t.TempDir()
	content := `package x
// TODO: implement this
func a() {} // fixme later
/* HACK: ugly */
// XXX revisit
// clean line
`
	writeFile(t, dir, "a.go", content)

	signals, err := NewTODOCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 4 {
		t.Fatalf("expected 4 marker signals, got %d: %+v", len(signals), signals)
	}
	for _, s := range signals {
		if s.Kind != "todo_fixme" {
			t.Errorf("Kind = %q, want todo_fixme", s.Kind)
		}
		if s.Risk != pilotapi.RiskLow {
			t.Errorf("Risk = %q, want low", s.Risk)
		}
		if s.Line <= 0 {
			t.Errorf("Line = %d, want > 0", s.Line)
		}
	}
}

func TestTODOCollector_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "// todo lower\n// ToDo mixed\n")
	signals, err := NewTODOCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("case-insensitive match expected 2, got %d", len(signals))
	}
}

func TestTODOCollector_LineNumbersAndUncapped(t *testing.T) {
	dir := t.TempDir()
	content := ""
	for i := 0; i < 50; i++ {
		content += "// TODO entry\n"
	}
	writeFile(t, dir, "many.go", content)

	signals, err := NewTODOCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 50 {
		t.Fatalf("collector must be uncapped: expected 50, got %d", len(signals))
	}
	if signals[0].Line != 1 || signals[49].Line != 50 {
		t.Errorf("line numbering wrong: first=%d last=%d", signals[0].Line, signals[49].Line)
	}
}

func TestTODOCollector_ScansMultipleExtensions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "// TODO go\n")
	writeFile(t, dir, "b.py", "# FIXME py\n")
	writeFile(t, dir, "c.ts", "// HACK ts\n")
	writeFile(t, dir, "d.txt", "TODO not scanned\n")

	signals, err := NewTODOCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 3 {
		t.Fatalf(".txt must be ignored; expected 3, got %d: %+v", len(signals), signals)
	}
}

func TestTODOCollector_EmptyProject(t *testing.T) {
	signals, err := NewTODOCollector().Collect(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("empty project must yield no signals; got %+v", signals)
	}
}

func TestTODOCollector_SkipsVendor(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, filepath.Join("vendor", "v.go"), "// TODO vendored\n")
	writeFile(t, dir, "real.go", "// TODO real\n")
	signals, err := NewTODOCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 || signals[0].File != "real.go" {
		t.Fatalf("vendored TODOs must be skipped; got %+v", signals)
	}
}

func TestMatchMarker(t *testing.T) {
	cases := []struct {
		line   string
		want   string
		wantOk bool
	}{
		{"// TODO x", "TODO", true},
		{"// fixme y", "FIXME", true},
		{"no marker here", "", false},
		{"HACK and TODO", "TODO", true}, // first in marker order wins
		{"// xxx", "XXX", true},
	}
	for _, tc := range cases {
		got, ok := matchMarker(tc.line)
		if ok != tc.wantOk || got != tc.want {
			t.Errorf("matchMarker(%q) = (%q,%v), want (%q,%v)", tc.line, got, ok, tc.want, tc.wantOk)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly", 7, "exactly"},
		{"toolong", 4, "tool…"},
		{"any", 0, ""},
		{"any", -1, ""},
	}
	for _, tc := range cases {
		if got := truncate(tc.in, tc.max); got != tc.want {
			t.Errorf("truncate(%q,%d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

func TestCleanComment(t *testing.T) {
	cases := map[string]string{
		"// TODO fix":   "TODO fix",
		"# FIXME py":    "FIXME py",
		"   /* HACK */": "HACK */",
		"plain":         "plain",
	}
	for in, want := range cases {
		if got := cleanComment(in); got != want {
			t.Errorf("cleanComment(%q) = %q, want %q", in, got, want)
		}
	}
}
