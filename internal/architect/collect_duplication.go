package architect

import (
	"bufio"
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// kindDuplicateBlock is the Signal kind the duplication collector emits: a run
// of consecutive significant lines that appears verbatim (after whitespace and
// comment normalization) in two or more places, i.e. a copy-paste block that
// has drifted out of a shared helper.
const kindDuplicateBlock = "duplicate_block"

// dupWindowK is the number of consecutive significant (non-blank, non-comment)
// lines that form a duplication window. Six lines is large enough to skip
// trivial boilerplate (a struct literal, an import block) while still catching
// copy-pasted logic.
const dupWindowK = 6

// dupHighRiskLocations is the location count at or above which a duplicated
// block is treated as high risk rather than medium: a block copied three or
// more times is a maintenance hazard whose drift surface grows with each copy.
const dupHighRiskLocations = 3

// location is a single occurrence of a duplicated window: the project-relative
// file it lives in and the 1-based line where the window starts.
type location struct {
	file string
	line int
}

// DuplicationCollector flags runs of dupWindowK consecutive significant lines
// that appear, byte-for-byte after whitespace/comment normalization, in two or
// more places. It is a line-based rolling-hash detector rather than an AST
// comparison, matching the package's deterministic text-window idiom.
type DuplicationCollector struct{}

// NewDuplicationCollector returns a DuplicationCollector.
func NewDuplicationCollector() *DuplicationCollector { return &DuplicationCollector{} }

// Name implements Collector.
func (c *DuplicationCollector) Name() string { return kindDuplicateBlock }

// Collect walks projectPath and emits one Signal per duplicated window group: a
// normalized dupWindowK-line block whose occurrences span at least two distinct
// files or two non-overlapping ranges in the same file. Test files (*_test.go)
// and generated files are skipped so the report stays focused on hand-written
// production code. The returned error is non-nil only on context cancellation,
// mirroring the other file-walk collectors.
func (c *DuplicationCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	windows := map[uint64][]location{}

	err := walkGoFiles(projectPath, func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		lines, err := readSignificantLines(path)
		if err != nil {
			// Unreadable or generated file: skip it, don't fail the collector.
			return nil
		}
		rel := relOrAbs(projectPath, path)
		hashWindows(rel, lines, windows)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return dupSignals(windows), nil
}

// significantLine is a normalized source line paired with its original 1-based
// line number, so a window hash can point back at where the block starts.
type significantLine struct {
	text string
	line int
}

// readSignificantLines reads the file at path and returns its significant lines
// (blank and pure-comment lines dropped, internal whitespace collapsed) keyed by
// their original 1-based line numbers. A generated file (its first non-blank
// line carries the standard "Code generated ... DO NOT EDIT." marker) returns no
// lines so it never contributes duplication signals.
func readSignificantLines(path string) ([]significantLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out []significantLine
	lineNum := 0
	sawNonBlank := false
	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		if !sawNonBlank {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			sawNonBlank = true
			if isGeneratedMarker(raw) {
				return nil, nil
			}
		}
		norm, ok := normalizeLine(raw)
		if !ok {
			continue
		}
		out = append(out, significantLine{text: norm, line: lineNum})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// isGeneratedMarker reports whether line is the standard generated-code header
// (`// Code generated ... DO NOT EDIT.`) that go:generate tooling emits.
func isGeneratedMarker(line string) bool {
	s := strings.TrimSpace(line)
	return strings.HasPrefix(s, "// Code generated") && strings.HasSuffix(s, "DO NOT EDIT.")
}

// normalizeLine trims s, drops blank and pure-comment lines, and collapses
// internal runs of whitespace to single spaces so cosmetic reformatting does not
// hide an otherwise identical block. ok is false for ignored (blank/comment)
// lines.
func normalizeLine(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", false
	}
	if strings.HasPrefix(trimmed, "//") {
		return "", false
	}
	return strings.Join(strings.Fields(trimmed), " "), true
}

// hashWindows folds every dupWindowK-line window in lines into windows, keyed by
// the fnv64a hash of the joined normalized text and recording the location of the
// window's first line. A file with fewer than dupWindowK significant lines
// contributes nothing.
func hashWindows(file string, lines []significantLine, windows map[uint64][]location) {
	if len(lines) < dupWindowK {
		return
	}
	for i := 0; i+dupWindowK <= len(lines); i++ {
		h := fnv.New64a()
		for j := 0; j < dupWindowK; j++ {
			_, _ = h.Write([]byte(lines[i+j].text))
			_, _ = h.Write([]byte{'\n'})
		}
		key := h.Sum64()
		windows[key] = append(windows[key], location{file: file, line: lines[i].line})
	}
}

// dupSignals turns the window map into Signals: one Signal per duplicated group,
// anchored at the group's first location. A group qualifies only when it has at
// least two occurrences spanning at least two distinct files or two
// non-overlapping ranges in one file. The map is iterated in sorted-key order
// and the result is sorted by File then Line, so the output is fully
// deterministic across runs.
func dupSignals(windows map[uint64][]location) []Signal {
	keys := make([]uint64, 0, len(windows))
	for k := range windows {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	var signals []Signal
	for _, k := range keys {
		locs := windows[k]
		if !qualifiesAsDuplicate(locs) {
			continue
		}
		sort.Slice(locs, func(i, j int) bool {
			if locs[i].file != locs[j].file {
				return locs[i].file < locs[j].file
			}
			return locs[i].line < locs[j].line
		})
		anchor := locs[0]
		signals = append(signals, Signal{
			Kind:   kindDuplicateBlock,
			File:   anchor.file,
			Line:   anchor.line,
			Detail: fmt.Sprintf("%d-line block duplicated in %d locations", dupWindowK, len(locs)),
			Weight: float64(len(locs)),
			Risk:   dupRisk(len(locs)),
		})
	}

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].File != signals[j].File {
			return signals[i].File < signals[j].File
		}
		return signals[i].Line < signals[j].Line
	})
	return signals
}

// qualifiesAsDuplicate reports whether a window's occurrences are a genuine
// duplication: at least two of them, spanning at least two distinct files or two
// non-overlapping line ranges within a single file. The same window hashed once
// per file position never collides with itself because each position has a
// distinct start line, but a single self-overlapping run is excluded here.
func qualifiesAsDuplicate(locs []location) bool {
	if len(locs) < 2 {
		return false
	}
	files := map[string]bool{}
	for _, l := range locs {
		files[l.file] = true
	}
	if len(files) >= 2 {
		return true
	}
	// Single file: require two windows whose ranges do not overlap.
	lines := make([]int, len(locs))
	for i, l := range locs {
		lines[i] = l.line
	}
	sort.Ints(lines)
	for i := 1; i < len(lines); i++ {
		if lines[i]-lines[i-1] >= dupWindowK {
			return true
		}
	}
	return false
}

// dupRisk classifies a duplicated block: medium for a single duplicate (two
// copies), high once it has spread to dupHighRiskLocations or more.
func dupRisk(count int) pilotapi.RiskLevel {
	if count >= dupHighRiskLocations {
		return pilotapi.RiskHigh
	}
	return pilotapi.RiskMedium
}
