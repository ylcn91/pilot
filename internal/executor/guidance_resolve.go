package executor

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// guidanceFileBudget caps the body of a single executor-guidance/<key>.md atom
// once its frontmatter is stripped. Mirrors capProjectContext's discipline: an
// oversized overlay/override is truncated on a line boundary and warned, rather
// than allowed to crowd the prompt unbounded. Sized larger than the README's
// ~1200-token soft cap so authoring stays within budget without tripping this
// hard guard on normal atoms.
const guidanceFileBudget = 8000

// overlayOnlyKeys are structural guidance fragments that MUST never be replaced
// by an override. `header` carries the [PILOT-EXEC] sentinel project CLAUDE.md
// files branch on (GH-2328); `evidence-spec` carries the no-op guard and the
// mandatory NO-OP RATIONALE marker (GH-3224/GH-3222). An override on these is
// downgraded to overlay so the const always ships intact ahead of any addition.
var overlayOnlyKeys = map[string]bool{
	"header":        true,
	"evidence-spec": true,
}

// loadGuidance resolves a single executor-prompt fragment against an optional
// file-driven overlay/override at <agentDir>/executor-guidance/<key>.md.
//
// Resolution (see .agent/executor-guidance/README.md — the contract):
//  1. agentDir == "" or the file is absent/unreadable → return fallback verbatim.
//     This keeps prompts byte-identical when no guidance atom exists.
//  2. mode: override → the file body replaces the const entirely.
//  3. mode: overlay (or missing/unrecognized) → const first, then the body.
//  4. For overlay-only keys (header, evidence-spec) an override is forced to
//     overlay so the structural sentinel/no-op contract is never dropped.
func loadGuidance(agentDir, key, fallback string) string {
	if agentDir == "" {
		return fallback
	}

	path := filepath.Join(agentDir, "executor-guidance", key+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}

	mode, body := parseGuidanceAtom(string(data))
	body = capGuidanceBody(key, body)
	if body == "" {
		return fallback
	}

	if mode == "override" && overlayOnlyKeys[key] {
		slog.Warn("guidance_override_ignored_on_overlay_only_key",
			slog.String("component", "executor"),
			slog.String("key", key),
		)
		mode = "overlay"
	}

	if mode == "override" {
		return body
	}

	// Overlay (or missing/unrecognized mode): const leads, file refines.
	if strings.TrimSpace(fallback) == "" {
		return body
	}
	return fallback + "\n\n" + body
}

// parseGuidanceAtom extracts the `mode:` field from leading YAML frontmatter
// (between the first two `---` fences) and returns the mode plus the body with
// that frontmatter stripped. A simple line scan mirrors the repo's existing
// .agent-reading style — no YAML dependency. mode is lowercased; absent or
// unfenced frontmatter yields mode "" (treated as overlay by the caller).
func parseGuidanceAtom(content string) (mode, body string) {
	lines := strings.Split(content, "\n")

	// Frontmatter must open on the first non-empty line with a `---` fence.
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || strings.TrimSpace(lines[start]) != "---" {
		return "", strings.TrimSpace(content)
	}

	for i := start + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			body = strings.Join(lines[i+1:], "\n")
			return mode, strings.TrimSpace(body)
		}
		if mode == "" {
			if rest, ok := strings.CutPrefix(line, "mode:"); ok {
				mode = strings.ToLower(strings.TrimSpace(rest))
			}
		}
	}

	// No closing fence: not valid frontmatter, treat the whole thing as body.
	return "", strings.TrimSpace(content)
}

// capGuidanceBody enforces guidanceFileBudget on a resolved atom body, cutting
// on a line boundary and warning when an atom overflows so over-large overlays
// stay visible. Mirrors capProjectContext.
func capGuidanceBody(key, body string) string {
	if len(body) <= guidanceFileBudget {
		return body
	}

	cut := body[:guidanceFileBudget]
	if bound := strings.LastIndexByte(cut, '\n'); bound > 0 {
		cut = cut[:bound]
	}

	slog.Warn("guidance_atom_truncated",
		slog.String("component", "executor"),
		slog.String("key", key),
		slog.Int("original_chars", len(body)),
		slog.Int("budget", guidanceFileBudget),
	)

	return strings.TrimRight(cut, " \n\t")
}
