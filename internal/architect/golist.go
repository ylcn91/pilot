package architect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// goListPackage is the subset of `go list -json` output the dependency-doctor
// lens consumes. The full record has dozens of fields; decoding only these
// keeps the parser stable across Go versions.
type goListPackage struct {
	ImportPath string   `json:"ImportPath"`
	Standard   bool     `json:"Standard"`
	Imports    []string `json:"Imports"`
	Module     *goMod   `json:"Module"`
}

// goMod is the module record embedded in a `go list -json` package.
type goMod struct {
	Path string `json:"Path"`
}

// commandRunner shells a command in dir and returns its combined behaviour:
// stdout, the error (if any), and is the single injection point the
// dependency-doctor collector mocks in tests so no test ever spawns `go`.
type commandRunner func(ctx context.Context, dir, name string, args ...string) (stdout []byte, err error)

// execCommandRunner is the production commandRunner: it runs the binary with
// os/exec, returning stdout even when the command exits non-zero (so a partial
// `go list` still yields the packages it managed to load).
func execCommandRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		return out.Bytes(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}

// parseGoListStream decodes the concatenated JSON object stream that
// `go list -json` emits (one object per package, not a JSON array) into
// goListPackage records. Malformed trailing data is tolerated: objects decoded
// before a decode error are returned alongside the error, so a best-effort
// caller can still use the partial graph.
func parseGoListStream(r io.Reader) ([]goListPackage, error) {
	dec := json.NewDecoder(r)
	var pkgs []goListPackage
	for {
		var p goListPackage
		if err := dec.Decode(&p); err != nil {
			if err == io.EOF {
				return pkgs, nil
			}
			return pkgs, fmt.Errorf("decode go list stream: %w", err)
		}
		pkgs = append(pkgs, p)
	}
}

// goListNodes converts decoded go-list packages into PackageNodes scoped to the
// module prefix (the project's own packages). Standard-library packages are
// dropped entirely; third-party imports are retained on each node only as
// ExternalImports so heavy/unused analysis can see them while cycle/layer
// traversal stays internal. modulePrefix is the project module path
// (e.g. "github.com/ylcn91/pilot"); a node is internal iff its ImportPath has
// that prefix.
func goListNodes(pkgs []goListPackage, modulePrefix string) []PackageNode {
	internal := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		if isInternalPath(p.ImportPath, modulePrefix) {
			internal[p.ImportPath] = true
		}
	}

	var nodes []PackageNode
	for _, p := range pkgs {
		if !isInternalPath(p.ImportPath, modulePrefix) {
			continue
		}
		var inEdges, exEdges []string
		for _, imp := range p.Imports {
			if internal[imp] {
				inEdges = append(inEdges, imp)
			} else {
				exEdges = append(exEdges, imp)
			}
		}
		mod := ""
		if p.Module != nil {
			mod = p.Module.Path
		}
		nodes = append(nodes, PackageNode{
			ImportPath:      p.ImportPath,
			Module:          mod,
			Imports:         inEdges,
			ExternalImports: exEdges,
		})
	}
	return nodes
}

// isInternalPath reports whether importPath belongs to the project module
// identified by modulePrefix. An empty prefix treats nothing as internal.
func isInternalPath(importPath, modulePrefix string) bool {
	if modulePrefix == "" {
		return false
	}
	return importPath == modulePrefix || strings.HasPrefix(importPath, modulePrefix+"/")
}

// loadPackageGraph shells `go list -deps -json ./...` via run, parses the
// stream, and builds the internal PackageGraph. It is best-effort: a non-zero
// `go list` exit still yields whatever packages were emitted before the error,
// and the error is returned only when nothing could be parsed.
func loadPackageGraph(ctx context.Context, run commandRunner, dir, modulePrefix string) (*PackageGraph, error) {
	out, runErr := run(ctx, dir, "go", "list", "-deps", "-json", "./...")
	pkgs, parseErr := parseGoListStream(bytes.NewReader(out))
	if len(pkgs) == 0 {
		if runErr != nil {
			return nil, runErr
		}
		if parseErr != nil {
			return nil, parseErr
		}
		return NewPackageGraph(nil), nil
	}
	return NewPackageGraph(goListNodes(pkgs, modulePrefix)), nil
}
