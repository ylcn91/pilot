package architect

import "strings"

// LayerRule is a single forbidden import-direction rule: no package whose path
// is under From may import any package under To. Rules are seeded from import
// directions that are CURRENTLY SATISFIED in the codebase, so any future hit is
// a genuine regression (a newly introduced bad edge), never noise about the
// existing structure.
type LayerRule struct {
	// Name is a short identifier for the rule, used in Signal detail.
	Name string
	// From is the package-path prefix that must not import To. A package
	// matches when its path equals From or is under From + "/".
	From string
	// To is the package-path prefix that From must not import.
	To string
	// Reason explains why the edge is forbidden, surfaced in the Signal.
	Reason string
}

// LayerViolation is one detected forbidden edge: package From imports package
// To, breaking Rule.
type LayerViolation struct {
	Rule LayerRule
	From string
	To   string
}

// defaultLayerRules are the project's verified architectural boundaries. Each
// was confirmed satisfied against the live graph before being seeded:
//
//   - pilotapi is a leaf shared-types package: it must import no other
//     internal package (verified: pilotapi imports zero internal packages).
//   - the executor must not depend on config; the dependency runs the other
//     way (config imports executor for StageConfig validation), so an
//     executor->config edge would be an inversion (verified: executor imports
//     no config package).
//
// modulePrefix is substituted into the prefixes at match time so the rules are
// independent of the concrete module path.
var defaultLayerRules = []LayerRule{
	{
		Name:   "pilotapi-is-a-leaf",
		From:   "internal/pilotapi",
		To:     "internal",
		Reason: "pilotapi is the shared leaf types package and must not import other internal packages",
	},
	{
		Name:   "executor-must-not-import-config",
		From:   "internal/executor",
		To:     "internal/config",
		Reason: "config depends on executor (StageConfig validation); the reverse edge inverts the layering",
	},
}

// CheckLayerRules returns every forbidden edge in the graph under the given
// rules, scoped to modulePrefix. Each rule's From/To are joined to the module
// prefix (e.g. "internal/executor" -> "<mod>/internal/executor") before
// matching, so rules stay module-agnostic. Results are deterministic: rules are
// evaluated in order and edges within a rule are sorted by (From, To).
func CheckLayerRules(g *PackageGraph, modulePrefix string, rules []LayerRule) []LayerViolation {
	var out []LayerViolation
	for _, rule := range rules {
		from := joinPrefix(modulePrefix, rule.From)
		to := joinPrefix(modulePrefix, rule.To)
		for _, pkg := range g.Nodes() {
			if !underPrefix(pkg, from) {
				continue
			}
			node, _ := g.Node(pkg)
			for _, imp := range node.Imports {
				if underPrefix(imp, to) && !underPrefix(imp, from) {
					out = append(out, LayerViolation{Rule: rule, From: pkg, To: imp})
				}
			}
		}
	}
	return out
}

// joinPrefix joins a module prefix and a relative package prefix with a single
// slash. An empty module prefix returns the relative prefix unchanged so rules
// remain usable in tests that build graphs without a module path.
func joinPrefix(modulePrefix, rel string) string {
	if modulePrefix == "" {
		return rel
	}
	return modulePrefix + "/" + rel
}

// underPrefix reports whether pkg equals prefix or is a sub-package of it
// (prefix + "/"). It guards against the false match where "internal/configx"
// would otherwise look like it is under "internal/config".
func underPrefix(pkg, prefix string) bool {
	return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
}
