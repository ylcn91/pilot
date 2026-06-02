package executor

import (
	"fmt"
	"sort"
	"strings"
)

// tddPhase identifies which gate is being evaluated, controlling the per-test
// verdict (RED requires the named tests to FAIL; GREEN requires them to PASS).
type tddPhase int

const (
	phaseRed tddPhase = iota
	phaseGreen
)

func (p tddPhase) String() string {
	if p == phaseGreen {
		return "GREEN"
	}
	return "RED"
}

// tddVerdict is the per-test proof outcome for one gate evaluation. OK is the
// gate's satisfied flag (RED: every named test failed; GREEN: every named test
// passed). Feedback is human-readable detail for the role retry loop and is
// non-empty exactly when OK is false.
type tddVerdict struct {
	OK       bool
	Feedback string
}

// evalGoTestVerdict turns a parsed `go test -json` run into a per-test verdict
// for the given phase. This is the H4 oracle: an exit code alone is not proof,
// so we require that the SPECIFIC named tests are present and in the expected
// terminal state.
//
//   - RED  (phaseRed):   PROOF requires every named test to be present and have
//     Action=="fail". A compile failure (no per-test events because the new test
//     references unimplemented symbols) is also a valid RED — the behavior is
//     genuinely unimplemented. A named test that PASSED or was SKIPPED, or that
//     is MISSING (typo / not actually added), fails the proof.
//   - GREEN (phaseGreen): requires every named test present and Action=="pass".
//     A failing, skipped, or missing named test fails the proof.
func evalGoTestVerdict(run *goTestRun, testNames []string, phase tddPhase) tddVerdict {
	if run == nil {
		return tddVerdict{OK: false, Feedback: "no go test results to evaluate"}
	}
	// Compile failure: the package did not build. For RED this is proof the
	// behavior is unimplemented (the new test won't even compile against current
	// code). For GREEN it means the implementation is still broken.
	if run.CompileFailed && len(run.Results) == 0 {
		if phase == phaseRed {
			return tddVerdict{OK: true}
		}
		return tddVerdict{OK: false, Feedback: tddCompileFeedback(run.Raw)}
	}

	names := dedupeSorted(testNames)
	var missing, wrongState []string
	for _, name := range names {
		res, ok := run.Results[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if !actionSatisfies(res.Action, phase) {
			wrongState = append(wrongState, fmt.Sprintf("%s=%s", name, res.Action))
		}
	}
	if len(missing) == 0 && len(wrongState) == 0 {
		return tddVerdict{OK: true}
	}
	return tddVerdict{OK: false, Feedback: tddVerdictFeedback(phase, missing, wrongState)}
}

// actionSatisfies reports whether a per-test terminal action satisfies the phase:
// RED wants "fail"; GREEN wants "pass". A "skip" never satisfies either gate — a
// skipped test proves nothing about the behavior under test.
func actionSatisfies(action string, phase tddPhase) bool {
	switch phase {
	case phaseGreen:
		return action == "pass"
	default:
		return action == "fail"
	}
}

// tddVerdictFeedback renders actionable detail for the role retry loop describing
// which named tests were missing and which were in the wrong state.
func tddVerdictFeedback(phase tddPhase, missing, wrongState []string) string {
	want := "FAIL"
	if phase == phaseGreen {
		want = "PASS"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s gate per-test proof failed: the named tests must %s.", phase, want)
	if len(missing) > 0 {
		fmt.Fprintf(&b, " Missing (not found in this run, check the exact function names): %s.", strings.Join(missing, ", "))
	}
	if len(wrongState) > 0 {
		fmt.Fprintf(&b, " Wrong state: %s.", strings.Join(wrongState, ", "))
	}
	return b.String()
}

// tddCompileFeedback trims the raw go output to a compact GREEN-gate compile
// failure hint without dumping the whole stream.
func tddCompileFeedback(raw string) string {
	const max = 2000
	if len(raw) > max {
		raw = raw[len(raw)-max:]
	}
	return "GREEN gate: the package failed to compile — the implementation does not build:\n" + raw
}

// dedupeSorted returns the unique, sorted set of names so verdict feedback is
// deterministic regardless of input order or duplicates.
func dedupeSorted(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
