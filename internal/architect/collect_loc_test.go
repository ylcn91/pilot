package architect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// writeGoFile writes a .go file with the given number of lines under dir.
func writeGoFile(t *testing.T, dir, name string, lines int) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var sb strings.Builder
	sb.WriteString("package x\n")
	for i := 1; i < lines; i++ {
		sb.WriteString("// line\n")
	}
	if err := os.WriteFile(full, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return full
}

func TestLOCCollector_FlagsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "big.go", 450)
	writeGoFile(t, dir, "small.go", 50)

	c := NewLOCCollector()
	signals, err := c.Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected exactly 1 oversized signal, got %d: %+v", len(signals), signals)
	}
	s := signals[0]
	if s.Kind != "loc_over_400" {
		t.Errorf("Kind = %q, want loc_over_400", s.Kind)
	}
	if s.File != "big.go" {
		t.Errorf("File = %q, want big.go", s.File)
	}
	if s.Risk != pilotapi.RiskMedium {
		t.Errorf("Risk = %q, want medium for 450-line file", s.Risk)
	}
	if s.Weight <= 0 {
		t.Errorf("Weight = %v, want > 0", s.Weight)
	}
}

func TestLOCCollector_ExactThresholdIsFlagged(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "exact.go", LOCThreshold) // exactly 400 lines

	signals, err := NewLOCCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("file at exactly %d lines must be flagged; got %d", LOCThreshold, len(signals))
	}
	if signals[0].Weight != 0 {
		t.Errorf("Weight at exactly threshold should be 0, got %v", signals[0].Weight)
	}
}

func TestLOCCollector_JustUnderThresholdNotFlagged(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "under.go", LOCThreshold-1)

	signals, err := NewLOCCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("file under threshold must not be flagged; got %+v", signals)
	}
}

func TestLOCCollector_HighRiskForVeryLargeFile(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "huge.go", 700)

	signals, err := NewLOCCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 || signals[0].Risk != pilotapi.RiskHigh {
		t.Fatalf("700-line file should be high risk; got %+v", signals)
	}
}

func TestLOCCollector_SkipsVendorAndGit(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, filepath.Join("vendor", "dep.go"), 900)
	writeGoFile(t, dir, filepath.Join(".git", "hook.go"), 900)
	writeGoFile(t, dir, filepath.Join("node_modules", "m.go"), 900)
	writeGoFile(t, dir, filepath.Join("dist", "b.go"), 900)
	writeGoFile(t, dir, "real.go", 500)

	signals, err := NewLOCCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 || signals[0].File != "real.go" {
		t.Fatalf("only non-vendored file should be flagged; got %+v", signals)
	}
}

func TestLOCCollector_EmptyProjectNoSignals(t *testing.T) {
	dir := t.TempDir()
	signals, err := NewLOCCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("empty project must produce no signals; got %+v", signals)
	}
}

func TestLOCCollector_IgnoresNonGoFiles(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "README.md")
	if err := os.WriteFile(big, []byte(strings.Repeat("x\n", 900)), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	signals, err := NewLOCCollector().Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("non-go files must be ignored; got %+v", signals)
	}
}

func TestCountLines(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"empty", "", 0},
		{"one_no_newline", "abc", 1},
		{"one_with_newline", "abc\n", 1},
		{"three", "a\nb\nc\n", 3},
		{"trailing_partial", "a\nb", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name)
			if err := os.WriteFile(p, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := countLines(p)
			if err != nil {
				t.Fatalf("countLines: %v", err)
			}
			if got != tc.want {
				t.Errorf("countLines(%q) = %d, want %d", tc.content, got, tc.want)
			}
		})
	}
}

func TestCountLines_MissingFile(t *testing.T) {
	if _, err := countLines(filepath.Join(t.TempDir(), "nope.go")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
