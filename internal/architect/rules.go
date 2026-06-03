package architect

import (
	"context"
	"sort"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// Violation is a single guardrail finding: a changed file broke an
// architectural rule. It is the per-PR analogue of a SCAN Signal — instead of
// surveying the whole tree it judges only the files a pull request touched.
//
// Rule is the producing rule's Name (stable, used for dedup and config opt-out).
// File is the project-relative path of the offending file. Detail is a short
// human-readable explanation. Risk maps the finding onto the shared pilotapi
// risk scale so downstream sinks can rank or threshold on it.
type Violation struct {
	Rule   string             `json:"rule"`
	File   string             `json:"file"`
	Detail string             `json:"detail"`
	Risk   pilotapi.RiskLevel `json:"risk"`
}

// Rule is one repo-specific architectural guardrail evaluated per pull request.
//
// Implementations MUST be deterministic, side-effect free, and must never
// panic: a guardrail that crashes would break the very autopilot flow it is
// meant to protect. Every rule degrades gracefully when worktreePath is empty
// or unreadable — it returns no violations rather than erroring, keeping the
// whole engine fail-open by construction.
type Rule interface {
	// Name returns a short, stable identifier (e.g. "loc-400"). It is used in
	// Violation.Rule, for deterministic ordering, and as the key a repo uses
	// to opt out of a single rule.
	Name() string

	// Eval inspects the given changedFiles (project-relative paths, as a PR
	// reports them) against the working tree rooted at worktreePath and
	// returns any violations. It never returns an error: unreadable inputs
	// yield zero violations, not a failure.
	Eval(ctx context.Context, changedFiles []string, worktreePath string) []Violation
}

// RuleRegistry is an ordered, name-addressable set of guardrail Rules. Order is
// preserved so a guardrails run is deterministic; lookup by name powers config
// opt-out and dedup.
type RuleRegistry struct {
	rules  []Rule
	byName map[string]Rule
}

// NewRuleRegistry builds a registry over the given rules in order. A later rule
// with a duplicate Name replaces the earlier one in name lookups but both keep
// their position in the ordered list; callers should avoid duplicate names.
func NewRuleRegistry(rules ...Rule) *RuleRegistry {
	r := &RuleRegistry{
		rules:  append([]Rule(nil), rules...),
		byName: make(map[string]Rule, len(rules)),
	}
	for _, rule := range rules {
		r.byName[rule.Name()] = rule
	}
	return r
}

// DefaultRuleRegistry returns the seeded guardrail registry: the LOC ceiling,
// the forbidden-import-edge rule (seeded with currently-satisfied directions),
// and the gateway-auth heuristic. modulePrefix is the project's module path
// (e.g. "github.com/ylcn91/pilot"); it is needed to resolve import edges.
func DefaultRuleRegistry(modulePrefix string) *RuleRegistry {
	return NewRuleRegistry(
		NewLOCRule(),
		NewForbiddenImportRule(modulePrefix, defaultLayerRules),
		NewGatewayAuthRule(),
	)
}

// Rules returns the registered rules in order. The slice is a copy.
func (r *RuleRegistry) Rules() []Rule {
	out := make([]Rule, len(r.rules))
	copy(out, r.rules)
	return out
}

// Names returns the registered rule names in registry order.
func (r *RuleRegistry) Names() []string {
	out := make([]string, 0, len(r.rules))
	for _, rule := range r.rules {
		out = append(out, rule.Name())
	}
	return out
}

// Lookup returns the rule registered under name and whether it exists.
func (r *RuleRegistry) Lookup(name string) (Rule, bool) {
	rule, ok := r.byName[name]
	return rule, ok
}

// Evaluate runs every registered rule whose name is not in disabled against the
// changed files and returns the aggregated violations. Results are sorted by
// (Rule, File) for stable output regardless of rule or file ordering. A nil or
// empty changedFiles yields no violations. The run never errors and never
// panics: rules that cannot read the worktree simply contribute nothing.
func (r *RuleRegistry) Evaluate(ctx context.Context, changedFiles []string, worktreePath string, disabled []string) []Violation {
	skip := make(map[string]bool, len(disabled))
	for _, name := range disabled {
		skip[name] = true
	}

	var out []Violation
	for _, rule := range r.rules {
		if skip[rule.Name()] {
			continue
		}
		if err := ctx.Err(); err != nil {
			break
		}
		out = append(out, rule.Eval(ctx, changedFiles, worktreePath)...)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].File < out[j].File
	})
	return out
}

// changedGoFiles filters changedFiles to project-relative *.go paths, dropping
// anything that is clearly outside source (vendored or VCS metadata) so rules
// share one consistent notion of "a Go file this PR touched".
func changedGoFiles(changedFiles []string) []string {
	var out []string
	for _, f := range changedFiles {
		if !hasExt(f, []string{".go"}) {
			continue
		}
		if isVendoredOrMeta(f) {
			continue
		}
		out = append(out, f)
	}
	return out
}
