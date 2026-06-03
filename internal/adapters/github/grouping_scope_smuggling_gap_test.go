package github

import (
	"testing"
)

// groupContaining returns the group (by issue numbers) that contains the given
// issue number, or nil if not found.
func groupContaining(groups [][]*Issue, number int) []int {
	for _, g := range groups {
		for _, is := range g {
			if is.Number == number {
				nums := make([]int, 0, len(g))
				for _, m := range g {
					nums = append(nums, m.Number)
				}
				return nums
			}
		}
	}
	return nil
}

// TestGroupByOverlappingScope_SanitizesSmuggledPaths verifies that
// groupByOverlappingScope sanitizes invisible Unicode out of the issue body
// BEFORE extracting directory references. Without sanitization an attacker can
// embed a zero-width / Tag-block / bidi character inside a path so the raw
// regex extracts a garbled directory (e.g. "internal/co<ZWSP>mms/x.go" yields
// the bogus dir "mms") — that bogus dir would NOT collide with a legitimate
// "internal/comms" issue, so the smuggled issue would slip past the
// scope-overlap guard and dispatch in parallel, risking merge conflicts.
//
// After sanitization the invisible rune is stripped, the path normalizes to
// "internal/comms/...", and the smuggled issue correctly joins the same
// overlap group as the legitimate one. Each case asserts the two issues land
// in a single group of size 2 (proof the body was sanitized first).
//
// helpers (zwsp, zwnj, zwj, bom, rlo, injectRune, interleaveRune,
// encodeTagSmuggle) are defined in converter_smuggling_test.go.
func TestGroupByOverlappingScope_SanitizesSmuggledPaths(t *testing.T) {
	const legitBody = "Change internal/comms/router.go"

	cases := []struct {
		name string
		// smuggledBody references internal/comms but hides an invisible rune
		// inside the path so the un-sanitized regex would mis-parse it.
		smuggledBody string
	}{
		{
			name:         "zero-width space inside directory segment",
			smuggledBody: injectRune("Update internal/comms/handler.go", zwsp, len("Update internal/co")),
		},
		{
			name:         "zero-width joiner inside directory segment",
			smuggledBody: injectRune("Update internal/comms/handler.go", zwj, len("Update internal/co")),
		},
		{
			name:         "BOM inside directory segment",
			smuggledBody: injectRune("Update internal/comms/handler.go", bom, len("Update internal/co")),
		},
		{
			name:         "right-to-left override inside directory segment",
			smuggledBody: injectRune("Update internal/comms/handler.go", rlo, len("Update internal/co")),
		},
		{
			name:         "Tag-block copy appended after a clean comms path",
			smuggledBody: "Update internal/comms/handler.go" + encodeTagSmuggle(" internal/comms/secret.go"),
		},
		{
			name:         "zero-width non-joiner interleaved through the path",
			smuggledBody: "Touch " + interleaveRune("internal/comms/x.go", zwnj),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := []*Issue{
				{Number: 1, Body: legitBody},
				{Number: 2, Body: tc.smuggledBody},
			}

			groups := groupByOverlappingScope(issues)

			if len(groups) != 1 {
				t.Fatalf("got %d groups, want 1 (smuggled path must be sanitized so it overlaps the legit issue); groups=%v",
					len(groups), allGroupNums(groups))
			}
			members := groupContaining(groups, 2)
			if len(members) != 2 {
				t.Errorf("smuggled issue #2 group = %v, want both #1 and #2 in one group", members)
			}
		})
	}
}

// TestGroupByOverlappingScope_SmuggledOnlyDoesNotFakeOverlap is the inverse
// guard: an invisible rune inserted between two otherwise-distinct paths must
// not be relied on to *create* a spurious group. Two issues touching genuinely
// different directories (internal/comms vs internal/gateway), each carrying an
// invisible rune, must remain in two separate groups — sanitization removes the
// noise but does not merge unrelated scopes.
func TestGroupByOverlappingScope_SmuggledOnlyDoesNotFakeOverlap(t *testing.T) {
	commsBody := injectRune("Change internal/comms/handler.go", zwsp, len("Change internal/co"))
	gatewayBody := injectRune("Change internal/gateway/server.go", bom, len("Change internal/gate"))

	issues := []*Issue{
		{Number: 1, Body: commsBody},
		{Number: 2, Body: gatewayBody},
	}

	groups := groupByOverlappingScope(issues)

	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (distinct dirs must not merge after sanitization); groups=%v",
			len(groups), allGroupNums(groups))
	}
}

func allGroupNums(groups [][]*Issue) [][]int {
	out := make([][]int, 0, len(groups))
	for _, g := range groups {
		nums := make([]int, 0, len(g))
		for _, is := range g {
			nums = append(nums, is.Number)
		}
		out = append(out, nums)
	}
	return out
}
