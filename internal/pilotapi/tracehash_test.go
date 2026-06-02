package pilotapi

import (
	"regexp"
	"testing"
)

var hex12 = regexp.MustCompile(`^[0-9a-f]{12}$`)

func TestTraceHashFormat(t *testing.T) {
	cases := [][]string{
		{},
		{""},
		{"a"},
		{"a", "b", "c"},
		{"plan", "TASK-1", "long content here", ""},
	}
	for _, parts := range cases {
		got := TraceHash(parts...)
		if len(got) != 12 {
			t.Errorf("TraceHash(%v) length = %d, want 12", parts, len(got))
		}
		if !hex12.MatchString(got) {
			t.Errorf("TraceHash(%v) = %q, not 12 lowercase hex chars", parts, got)
		}
	}
}

func TestTraceHashDeterministic(t *testing.T) {
	parts := []string{"architect", "TASK-7", "design body"}
	first := TraceHash(parts...)
	for i := 0; i < 100; i++ {
		if got := TraceHash(parts...); got != first {
			t.Fatalf("TraceHash not deterministic: iter %d got %q, want %q", i, got, first)
		}
	}
}

func TestTraceHashOrderSensitive(t *testing.T) {
	ab := TraceHash("a", "b")
	ba := TraceHash("b", "a")
	if ab == ba {
		t.Errorf("TraceHash should be order-sensitive: (a,b)=%q == (b,a)=%q", ab, ba)
	}
}

func TestTraceHashSeparatorIsInjective(t *testing.T) {
	// With a NUL separator, splitting boundaries must matter:
	// ["a","b"] and ["ab"] must not collide.
	if TraceHash("a", "b") == TraceHash("ab") {
		t.Error("TraceHash(a,b) collided with TraceHash(ab); separator not effective")
	}
	if TraceHash("a", "bc") == TraceHash("ab", "c") {
		t.Error("TraceHash(a,bc) collided with TraceHash(ab,c)")
	}
}

func TestTraceHashDistinctInputsDistinctHashes(t *testing.T) {
	// Note: TraceHash() and TraceHash("") both join to "" and so are
	// expected to collide; that documented equality is asserted separately
	// in TestTraceHashSingleEmptyVsZeroParts. Only one of them belongs here.
	inputs := [][]string{
		{""},
		{"", ""},
		{"a"},
		{"b"},
		{"a", "b"},
		{"b", "a"},
		{"ab"},
		{"plan", "TASK-1", "x"},
		{"plan", "TASK-1", "y"},
		{"plan", "TASK-2", "x"},
		{"architect", "TASK-1", "x"},
	}
	seen := make(map[string][]string, len(inputs))
	for _, in := range inputs {
		h := TraceHash(in...)
		if prev, ok := seen[h]; ok {
			t.Errorf("hash collision %q: %v and %v", h, prev, in)
		}
		seen[h] = in
	}
}

func TestTraceHashSingleEmptyVsZeroParts(t *testing.T) {
	// strings.Join nil and [""] both produce "", so these are expected
	// to be equal by construction; assert that documented behavior holds.
	if TraceHash() != TraceHash("") {
		t.Errorf("TraceHash() = %q, TraceHash(\"\") = %q; expected equal", TraceHash(), TraceHash(""))
	}
}

func TestTraceHashKnownVector(t *testing.T) {
	// sha256("") = e3b0c442... -> first 12 hex chars locked in to catch
	// accidental changes to the algorithm or truncation length.
	const want = "e3b0c44298fc"
	if got := TraceHash(); got != want {
		t.Errorf("TraceHash() = %q, want %q (sha256 of empty input, first 12 hex)", got, want)
	}
}
