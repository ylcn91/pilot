package architect

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// adrFilePrefix and adrFileExt frame the on-disk name of an emitted RFC/ADR
// document: WriteADR writes `<dir>/rfc_<slug>.md`. The prefix keeps the RFC
// drafts grouped and greppable inside .agent/system without colliding with the
// hand-written architecture docs that already live there.
const (
	adrFilePrefix = "rfc_"
	adrFileExt    = ".md"
)

// maxSlugLen caps the slug length so a verbose RFC title cannot produce an
// unwieldy (or filesystem-rejecting) path. The cap is applied after
// sanitisation and never splits in the middle of a run of separators.
const maxSlugLen = 80

// slugSanitizer matches every run of characters that is not an ASCII
// lowercase letter or digit. SlugifyADR collapses each such run to a single
// hyphen so the result is a clean, deterministic, path-safe slug.
var slugSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

// SlugifyADR turns an arbitrary RFC/ADR title into a deterministic, path-safe
// slug: lower-cased, with every run of non-alphanumeric characters collapsed to
// a single hyphen and the leading/trailing hyphens trimmed. The result contains
// only [a-z0-9-], can never contain a path separator or "..", and is capped to
// maxSlugLen so it is safe to embed directly in a filename. An input that
// sanitises to nothing (empty, or all punctuation) yields "untitled" so WriteADR
// always has a usable name.
func SlugifyADR(title string) string {
	s := slugSanitizer.ReplaceAllString(strings.ToLower(title), "-")
	s = strings.Trim(s, "-")
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-")
	}
	if s == "" {
		return "untitled"
	}
	return s
}

// ADRPath returns the absolute-or-relative file path WriteADR would write for
// dir and slug, with the slug re-sanitised through SlugifyADR so a caller that
// passes a raw or hostile slug ("../../etc/passwd") still resolves to a single
// `rfc_<safe>.md` file inside dir. The returned path is always dir joined with
// exactly one filename — never an escape outside dir.
func ADRPath(dir, slug string) string {
	safe := SlugifyADR(slug)
	return filepath.Join(dir, adrFilePrefix+safe+adrFileExt)
}

// WriteADR writes content to `<dir>/rfc_<slug>.md`, creating dir (and any
// missing parents) when absent. The slug is re-sanitised through SlugifyADR so
// the filename is always a single safe component: a slug carrying separators or
// ".." cannot traverse out of dir. The write is a full overwrite, so calling
// WriteADR twice with the same slug is idempotent in location (the second call
// replaces the first file's contents). It returns the path written so the lens
// can report exactly where the document landed.
//
// Errors are returned, never panicked: an empty dir, an un-creatable directory,
// or an unwritable file each surface as a wrapped error.
func WriteADR(dir, slug, content string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("architect: WriteADR requires a non-empty dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("architect: WriteADR create dir %q: %w", dir, err)
	}
	path := ADRPath(dir, slug)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("architect: WriteADR write %q: %w", path, err)
	}
	return path, nil
}
