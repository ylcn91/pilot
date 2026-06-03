package architect

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
)

// moduleFanIn maps a module path to the number of distinct requiring modules
// that depend on it in the module graph (`go mod graph`). A high fan-in marks
// a "heavy" dependency: many parts of the dependency tree pull it in, so
// removing or replacing it has broad blast radius.
type moduleFanIn map[string]int

// parseModGraph parses `go mod graph` output into per-module fan-in counts.
// Each line is "requirer requiree"; the requiree's fan-in is incremented once
// per distinct requirer. Module versions are stripped (everything after '@')
// so all versions of a module collapse to one count, matching how a human
// reasons about "how many things need lib X".
func parseModGraph(out []byte) moduleFanIn {
	requirers := map[string]map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		from := stripVersion(fields[0])
		to := stripVersion(fields[1])
		if to == "" || from == to {
			continue
		}
		if requirers[to] == nil {
			requirers[to] = map[string]bool{}
		}
		requirers[to][from] = true
	}

	fanIn := make(moduleFanIn, len(requirers))
	for mod, set := range requirers {
		fanIn[mod] = len(set)
	}
	return fanIn
}

// stripVersion removes the "@version" suffix from a `go mod graph` token,
// leaving the bare module path.
func stripVersion(token string) string {
	if i := strings.IndexByte(token, '@'); i >= 0 {
		return token[:i]
	}
	return token
}

// modWhyUnusedMarker is the line `go mod why` prints when the main module does
// not need a package: "(main module does not need package PATH)" or
// "(main module does not need module PATH)". Detecting it tells us a required
// module is no longer actually imported.
const modWhyUnusedMarker = "(main module does not need"

// isModuleUnused runs `go mod why -m <module>` via run and reports whether the
// output marks the module as not needed. Any execution error is treated as
// "unknown" (returns false) so a flaky tool never produces a false positive.
func isModuleUnused(ctx context.Context, run commandRunner, dir, module string) bool {
	out, err := run(ctx, dir, "go", "mod", "why", "-m", module)
	if err != nil && len(out) == 0 {
		return false
	}
	return whyOutputIsUnused(out)
}

// whyOutputIsUnused reports whether `go mod why` output contains the
// not-needed marker on any line.
func whyOutputIsUnused(out []byte) bool {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if strings.Contains(sc.Text(), modWhyUnusedMarker) {
			return true
		}
	}
	return false
}

// modulePathFromDir reads go.mod under dir and returns the declared module
// path, or "" when go.mod is missing or has no module directive. Best-effort:
// callers degrade gracefully (the deps collector emits no internal-scoped
// signals rather than failing).
func modulePathFromDir(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	return parseModulePath(data)
}

// parseModulePath extracts the path from a go.mod's `module` directive. It
// scans line by line for the first `module <path>` declaration, ignoring
// comments and surrounding whitespace. Returns "" when none is found.
func parseModulePath(data []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(stripLineComment(sc.Text()))
		if rest, ok := strings.CutPrefix(line, "module"); ok {
			rest = strings.TrimSpace(rest)
			if rest != "" {
				return unquoteModToken(rest)
			}
		}
	}
	return ""
}

// requiredModules reads the `require` directives from go.mod under dir,
// returning the required module paths (direct and indirect). It is the
// candidate set the unused-dependency check probes with `go mod why`. A missing
// or unparseable go.mod yields nil. Both the single-line
// (`require path version`) and block (`require (\n path version\n)`) forms are
// handled.
func requiredModules(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil
	}
	return parseRequiredModules(data)
}

// parseRequiredModules extracts required module paths from go.mod bytes,
// covering both the single-line and parenthesised-block require forms.
func parseRequiredModules(data []byte) []string {
	var out []string
	inBlock := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(stripLineComment(sc.Text()))
		if line == "" {
			continue
		}
		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			if path := firstToken(line); path != "" {
				out = append(out, unquoteModToken(path))
			}
			continue
		}
		rest, ok := strings.CutPrefix(line, "require")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "(" {
			inBlock = true
			continue
		}
		if path := firstToken(rest); path != "" {
			out = append(out, unquoteModToken(path))
		}
	}
	return out
}

// firstToken returns the first whitespace-delimited token of s, or "".
func firstToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// stripLineComment removes a trailing `// ...` comment from a go.mod line.
func stripLineComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// unquoteModToken strips surrounding double quotes from a go.mod token (paths
// may be quoted). It leaves unquoted tokens untouched.
func unquoteModToken(tok string) string {
	return strings.Trim(tok, `"`)
}
