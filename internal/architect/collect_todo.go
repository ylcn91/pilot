package architect

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// todoMarkers are the comment markers the TODOCollector flags, matched
// case-insensitively as whole words.
var todoMarkers = []string{"TODO", "FIXME", "HACK", "XXX"}

// todoExts are the source file extensions the TODOCollector scans.
var todoExts = []string{".go", ".py", ".js", ".ts", ".tsx", ".jsx"}

// TODOCollector scans source files for TODO/FIXME/HACK/XXX markers and emits
// one Signal per occurrence. It is uncapped: every matching line is reported.
type TODOCollector struct{}

// NewTODOCollector returns a TODOCollector.
func NewTODOCollector() *TODOCollector { return &TODOCollector{} }

// Name implements Collector.
func (c *TODOCollector) Name() string { return "todo_fixme" }

// Collect walks projectPath and emits one Signal for each line in a source
// file that contains a TODO/FIXME/HACK/XXX marker. The marker match is
// case-insensitive. Each Signal records the file, 1-based line number, the
// matched marker, and the (truncated) comment text.
func (c *TODOCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	var signals []Signal

	err := walkSourceFiles(projectPath, todoExts, func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		found, err := scanTODOs(path, relOrAbs(projectPath, path))
		if err != nil {
			// Unreadable file: skip, don't abort the collector.
			return nil
		}
		signals = append(signals, found...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return signals, nil
}

// scanTODOs reads the file at path and returns a Signal for every line
// containing a todo marker. relPath is used for Signal.File.
func scanTODOs(path, relPath string) ([]Signal, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var signals []Signal
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		marker, ok := matchMarker(line)
		if !ok {
			continue
		}
		signals = append(signals, Signal{
			Kind:   "todo_fixme",
			File:   relPath,
			Line:   lineNum,
			Detail: fmt.Sprintf("%s: %s", marker, truncate(cleanComment(line), 80)),
			Weight: 0.25,
			Risk:   pilotapi.RiskLow,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return signals, nil
}

// matchMarker reports whether line contains one of the todoMarkers and, if
// so, returns the canonical (upper-case) marker found. Matching is
// case-insensitive. The first marker in todoMarkers order wins.
func matchMarker(line string) (string, bool) {
	upper := strings.ToUpper(line)
	for _, m := range todoMarkers {
		if strings.Contains(upper, m) {
			return m, true
		}
	}
	return "", false
}

// cleanComment strips a leading comment delimiter and surrounding whitespace
// from a source line for use in Signal.Detail.
func cleanComment(line string) string {
	s := strings.TrimSpace(line)
	for _, prefix := range []string{"//", "#", "/*", "*"} {
		s = strings.TrimPrefix(s, prefix)
	}
	return strings.TrimSpace(s)
}

// truncate shortens s to at most max runes, appending an ellipsis when it
// was cut. A non-positive max returns the empty string.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
