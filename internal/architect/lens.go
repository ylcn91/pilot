package architect

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// CoreLensName is the name of the default lens: the deterministic core scan
// (oversized files, TODO/FIXME, lint, coverage). Running `pilot architect`
// with no --lens flag selects this lens, preserving the original behaviour.
const CoreLensName = "core"

// Lens is a named bundle of Collectors over the shared SCAN spine. A lens lets
// a user aim the Architect at a specific concern (e.g. the dependency-doctor
// lens bundles the import-graph collectors) without changing the pipeline: the
// same Scanner runs the lens's collectors, the same PROPOSE/EMIT stages
// consume their Signals.
//
// Collectors is a factory rather than a fixed slice because some collectors
// need the project path at construction time and because a lens may compose
// differently per project.
type Lens struct {
	// Name is the stable selector used by the --lens flag and the registry.
	Name string
	// Description is a one-line human summary shown in help/listings.
	Description string
	// Collectors builds the lens's collectors for the given project root and
	// scan options. It must be deterministic and must not perform I/O itself
	// (collectors do their work lazily in Collect). A lens that ignores the
	// options (e.g. depdoctor) simply does not read them.
	Collectors func(projectPath string, opts ScanOptions) []Collector
}

// lensRegistry is the process-wide set of registered lenses, keyed by Name.
// Guarded by a mutex so RegisterLens is safe to call from package init
// functions across files without ordering assumptions.
var (
	lensMu       sync.RWMutex
	lensRegistry = map[string]Lens{}
)

// RegisterLens adds a lens to the registry. It panics on an empty name, a nil
// Collectors factory, or a duplicate name — all programmer errors that should
// surface at startup, never at runtime. Lens names are matched
// case-insensitively, so they are stored lower-cased.
func RegisterLens(l Lens) {
	name := normalizeLensName(l.Name)
	if name == "" {
		panic("architect: RegisterLens requires a non-empty Name")
	}
	if l.Collectors == nil {
		panic(fmt.Sprintf("architect: lens %q requires a non-nil Collectors factory", l.Name))
	}

	lensMu.Lock()
	defer lensMu.Unlock()
	if _, dup := lensRegistry[name]; dup {
		panic(fmt.Sprintf("architect: lens %q already registered", name))
	}
	l.Name = name
	lensRegistry[name] = l
}

// LensByName returns the registered lens for name (case-insensitive). An empty
// name resolves to the core lens, so the no-flag CLI path keeps the original
// behaviour. An unknown name returns an error listing the available lenses.
func LensByName(name string) (Lens, error) {
	key := normalizeLensName(name)
	if key == "" {
		key = CoreLensName
	}

	lensMu.RLock()
	defer lensMu.RUnlock()
	l, ok := lensRegistry[key]
	if !ok {
		return Lens{}, fmt.Errorf("unknown lens %q; available: %s", name, strings.Join(lensNamesLocked(), ", "))
	}
	return l, nil
}

// LensNames returns the names of all registered lenses, sorted, with the core
// lens first when present so listings lead with the default.
func LensNames() []string {
	lensMu.RLock()
	defer lensMu.RUnlock()
	return lensNamesLocked()
}

// lensNamesLocked returns the sorted lens names (core first). Callers must hold
// at least a read lock.
func lensNamesLocked() []string {
	names := make([]string, 0, len(lensRegistry))
	for n := range lensRegistry {
		names = append(names, n)
	}
	sort.Strings(names)
	// Surface the core lens first if present.
	for i, n := range names {
		if n == CoreLensName && i != 0 {
			names = append(names[:i], names[i+1:]...)
			names = append([]string{CoreLensName}, names...)
			break
		}
	}
	return names
}

// normalizeLensName lower-cases and trims a lens name for case-insensitive,
// whitespace-tolerant matching.
func normalizeLensName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// BuildLensScanner resolves the named lens and wires a Scanner over its
// collectors for projectPath. An empty name selects the core lens. The
// returned scanner is filtered by opts.Signals (when non-empty) exactly like
// BuildDefaultScanner, so the same per-Kind selection works across every lens.
func BuildLensScanner(name, projectPath string, opts ScanOptions) (*Scanner, error) {
	l, err := LensByName(name)
	if err != nil {
		return nil, err
	}
	collectors := l.Collectors(projectPath, opts)
	selected := filterCollectors(collectors, opts.Signals)
	return NewScanner(selected...), nil
}

// init registers the core lens: the deterministic default roster. Its
// Collectors factory reuses the shared coreCollectors roster so the core lens
// and the legacy BuildDefaultScanner path stay identical by construction.
func init() {
	RegisterLens(Lens{
		Name:        CoreLensName,
		Description: "deterministic core scan: oversized files, TODO/FIXME, lint, coverage",
		Collectors: func(_ string, opts ScanOptions) []Collector {
			return coreCollectors(opts)
		},
	})
}
