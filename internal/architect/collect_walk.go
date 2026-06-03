package architect

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// skipDirs are directory names never descended into during a project walk:
// vendored code, VCS metadata, and build/dependency output that is not the
// project's own source.
var skipDirs = map[string]bool{
	"vendor":       true,
	".git":         true,
	"node_modules": true,
	"dist":         true,
}

// shouldSkipDir reports whether a directory with the given base name should
// be pruned from the walk.
func shouldSkipDir(name string) bool {
	return skipDirs[name]
}

// walkGoFiles invokes fn for every regular *.go file under root, pruning the
// directories in skipDirs. Walk errors on individual entries are skipped
// rather than aborting the whole walk, so one unreadable directory does not
// hide the rest of the project. fn receives the absolute path; returning an
// error from fn aborts the walk and is propagated.
func walkGoFiles(root string, fn func(path string) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable entry: skip it (and its subtree if a dir) but keep going.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		return fn(path)
	})
}

// walkSourceFiles is like walkGoFiles but visits files with any of the given
// extensions (each including the leading dot, e.g. ".go", ".py"). When exts
// is empty it visits no files.
func walkSourceFiles(root string, exts []string, fn func(path string) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !hasExt(d.Name(), exts) {
			return nil
		}
		return fn(path)
	})
}

// hasExt reports whether name ends with one of the given extensions.
func hasExt(name string, exts []string) bool {
	for _, e := range exts {
		if strings.HasSuffix(name, e) {
			return true
		}
	}
	return false
}

// relOrAbs returns path relative to root when possible, otherwise path
// unchanged. Used to keep Signal.File stable and project-relative.
func relOrAbs(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}
