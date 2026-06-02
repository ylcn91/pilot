package architect

import (
	"context"
	"path/filepath"
	"testing"
)

// TestScan_RealCollectorsOverFixture wires the deterministic file-walk
// collectors (LOC + TODO) through the Scanner against a temp-dir fixture and
// asserts the aggregate, exercising the full SCAN happy path without an LLM,
// lint tool, or memory store.
func TestScan_RealCollectorsOverFixture(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "oversized.go", 500)
	writeFile(t, dir, filepath.Join("internal", "svc.go"), "package svc\n// TODO wire this up\nfunc F() {} // FIXME\n")
	writeFile(t, dir, filepath.Join("vendor", "dep.go"), "package dep\n// TODO ignored\n")
	writeGoFile(t, dir, "small.go", 10)

	s := NewScanner(NewLOCCollector(), NewTODOCollector())
	signals, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var loc, todo int
	for _, sig := range signals {
		switch sig.Kind {
		case "loc_over_400":
			loc++
		case "todo_fixme":
			todo++
		default:
			t.Errorf("unexpected signal kind %q", sig.Kind)
		}
	}
	if loc != 1 {
		t.Errorf("expected 1 LOC signal, got %d", loc)
	}
	if todo != 2 {
		t.Errorf("expected 2 TODO signals (vendor excluded), got %d", todo)
	}
}

// TestScan_EmptyProjectAllCollectors confirms an empty project yields no
// signals across every real collector, including the best-effort ones whose
// sources are nil/absent.
func TestScan_EmptyProjectAllCollectors(t *testing.T) {
	dir := t.TempDir()
	s := NewScanner(
		NewLOCCollector(),
		NewTODOCollector(),
		NewLintCollector(nil, "lint"),
		NewCoverageCollector(nil, "coverage", 80),
		NewChurnCollector(nil, memoryQueryZero(), 10, "proj"),
		NewPitfallCollector(nil, "proj"),
	)
	signals, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("empty project must produce no signals; got %+v", signals)
	}
}

// TestScan_ToleratesOneFailingCollectorInPipeline confirms a failing
// collector mid-pipeline does not prevent later real collectors from running.
func TestScan_ToleratesOneFailingCollectorInPipeline(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "big.go", 500)

	failing := &stubCollector{name: "boom", err: errFixture}
	s := NewScanner(failing, NewLOCCollector())
	signals, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan must tolerate failing collector; got %v", err)
	}
	if len(signals) != 1 || signals[0].Kind != "loc_over_400" {
		t.Fatalf("LOC collector after failure should still produce its signal; got %+v", signals)
	}
}

func TestRelOrAbs(t *testing.T) {
	root := "/a/b"
	if got := relOrAbs(root, "/a/b/c.go"); got != "c.go" {
		t.Errorf("relOrAbs = %q, want c.go", got)
	}
	// Unrelated absolute path: returned unchanged.
	if got := relOrAbs(root, "x.go"); got == "" {
		t.Errorf("relOrAbs returned empty for %q", "x.go")
	}
}

func TestHasExt(t *testing.T) {
	if !hasExt("a.go", []string{".go", ".py"}) {
		t.Error("a.go should match .go")
	}
	if hasExt("a.txt", []string{".go"}) {
		t.Error("a.txt should not match .go")
	}
	if hasExt("a.go", nil) {
		t.Error("empty ext list must match nothing")
	}
}
