package architect

import (
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// proposalJSONShape is the exact JSON contract the PROPOSE stage instructs the
// backend to emit. It is appended verbatim to the prompt so the model knows the
// field names and types expected by pilotapi.Finding. Keeping the shape here
// (next to the parser that consumes it) keeps the prompt and the unmarshal
// target in sync.
const proposalJSONShape = `[
  {
    "title": "<short imperative title of the proposed change>",
    "kind": "<category, e.g. refactor | bug | test-gap | hardening>",
    "risk": "<one of: low | medium | high | release-blocker>",
    "why_it_matters": "<concise rationale grounded in the signals>",
    "suggested_pr_pieces": ["<small, reviewable PR-sized step>", "..."],
    "test_plan": "<how to verify the change is correct>",
    "files": ["<relative path the change touches>", "..."]
  }
]`

// extractJSON pulls the JSON array out of a backend's free-form output. Backends
// frequently wrap JSON in markdown code fences and/or surround it with prose, so
// this is deliberately tolerant: it strips ```json / ``` fences, then narrows to
// the substring from the first '[' to the last ']'. When no array delimiters are
// present it returns the empty string, signalling the caller that there is
// nothing to unmarshal (rather than feeding json.Unmarshal obvious garbage).
func extractJSON(output string) string {
	s := stripCodeFences(output)

	start := strings.IndexByte(s, '[')
	end := strings.LastIndexByte(s, ']')
	if start < 0 || end < 0 || end < start {
		return ""
	}
	return strings.TrimSpace(s[start : end+1])
}

// stripCodeFences removes markdown code-fence lines (```), including the
// language hint variant (```json), from output. It only drops the fence lines
// themselves; the fenced content is preserved so the array inside survives.
func stripCodeFences(output string) string {
	if !strings.Contains(output, "```") {
		return output
	}
	lines := strings.Split(output, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// normalizeRisk maps a possibly-noisy risk string onto the canonical pilotapi
// scale. Unknown or empty values default to medium so a single malformed risk
// never drops an otherwise-useful proposal. Recognised values (case-insensitive,
// whitespace-tolerant) are normalised via pilotapi.ParseRisk.
func normalizeRisk(raw pilotapi.RiskLevel) pilotapi.RiskLevel {
	if r, ok := pilotapi.ParseRisk(string(raw)); ok {
		return r
	}
	return pilotapi.RiskMedium
}
