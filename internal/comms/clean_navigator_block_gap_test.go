package comms

import "testing"

// TestCleanInternalSignals_NavigatorBlockSkipping exercises the NAVIGATOR_STATUS
// block-skipping branch of CleanInternalSignals beyond the single happy-path
// case in util_test.go. The terminator only fires for a line that, after
// TrimSpace, starts with "━" AND only once at least one clean line has been
// emitted (len(clean) > 0). These edge cases pin that exact contract.
func TestCleanInternalSignals_NavigatorBlockSkipping(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// NAVIGATOR_STATUS as the very first line: len(clean) == 0 when the
			// terminator is seen, so the block never closes and everything after
			// is consumed. Trailing-blank trim leaves "".
			name: "block at start consumes rest (no clean line before terminator)",
			in:   "NAVIGATOR_STATUS\nPhase: IMPL\n━━━━━━━━━━\nafter",
			want: "",
		},
		{
			// A clean line precedes the block, so when the ━ terminator line is
			// reached len(clean) > 0 and the block closes; the ━ line itself is
			// skipped and the following content survives.
			name: "block after clean line closes on terminator",
			in:   "intro\nNAVIGATOR_STATUS\nPhase: IMPL\n━━━━━━━━━━\ntail",
			want: "intro\ntail",
		},
		{
			// No ━ terminator anywhere after the block opener: the block eats the
			// remainder of the input.
			name: "block without terminator eats remainder",
			in:   "intro\nNAVIGATOR_STATUS\nline a\nline b",
			want: "intro",
		},
		{
			// The terminator must start with ━ after trimming. Leading spaces are
			// trimmed so an indented ━ line still closes the block.
			name: "indented terminator still closes block",
			in:   "intro\nNAVIGATOR_STATUS\nmeta\n   ━━━\nresumed",
			want: "intro\nresumed",
		},
		{
			// A line that merely contains ━ but does not start with it does NOT
			// close the block, so subsequent lines stay skipped.
			name: "non-leading bar does not close block",
			in:   "intro\nNAVIGATOR_STATUS\ntext ━ more\nstill skipped",
			want: "intro",
		},
		{
			// Two NAVIGATOR_STATUS blocks: after the first closes, the second
			// opener re-enters skip mode and its terminator closes it again.
			name: "two sequential blocks both skipped",
			in:   "a\nNAVIGATOR_STATUS\nx\n━━━\nb\nNAVIGATOR_STATUS\ny\n━━━\nc",
			want: "a\nb\nc",
		},
		{
			// The opener line itself (containing NAVIGATOR_STATUS) is always
			// dropped even when it carries trailing text on the same line.
			name: "opener line with trailing text dropped",
			in:   "head\nNAVIGATOR_STATUS: running\n━━━\nfoot",
			want: "head\nfoot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanInternalSignals(tt.in)
			if got != tt.want {
				t.Errorf("CleanInternalSignals(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
