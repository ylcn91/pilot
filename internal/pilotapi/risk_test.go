package pilotapi

import "testing"

func TestRiskLevelIsValid(t *testing.T) {
	tests := []struct {
		name string
		risk RiskLevel
		want bool
	}{
		{"low", RiskLow, true},
		{"medium", RiskMedium, true},
		{"high", RiskHigh, true},
		{"release-blocker", RiskReleaseBlocker, true},
		{"empty", RiskLevel(""), false},
		{"unknown word", RiskLevel("critical"), false},
		{"wrong case", RiskLevel("LOW"), false},
		{"trailing space", RiskLevel("low "), false},
		{"numeric", RiskLevel("1"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.risk.IsValid(); got != tt.want {
				t.Errorf("RiskLevel(%q).IsValid() = %v, want %v", string(tt.risk), got, tt.want)
			}
		})
	}
}

func TestRiskLevelValuesAreCanonicalStrings(t *testing.T) {
	cases := map[RiskLevel]string{
		RiskLow:            "low",
		RiskMedium:         "medium",
		RiskHigh:           "high",
		RiskReleaseBlocker: "release-blocker",
	}
	for r, want := range cases {
		if string(r) != want {
			t.Errorf("RiskLevel value = %q, want %q", string(r), want)
		}
	}
}

func TestParseRisk(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   RiskLevel
		wantOK bool
	}{
		{"exact low", "low", RiskLow, true},
		{"exact medium", "medium", RiskMedium, true},
		{"exact high", "high", RiskHigh, true},
		{"exact release-blocker", "release-blocker", RiskReleaseBlocker, true},
		{"uppercase", "HIGH", RiskHigh, true},
		{"mixed case", "Release-Blocker", RiskReleaseBlocker, true},
		{"leading/trailing whitespace", "  medium  ", RiskMedium, true},
		{"tab and newline padding", "\tlow\n", RiskLow, true},
		{"empty", "", "", false},
		{"whitespace only", "   ", "", false},
		{"unknown", "severe", "", false},
		{"partial", "block", "", false},
		{"inner space breaks match", "release blocker", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseRisk(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ParseRisk(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("ParseRisk(%q) = %q, want %q", tt.in, string(got), string(tt.want))
			}
			if ok && !got.IsValid() {
				t.Errorf("ParseRisk(%q) returned non-valid RiskLevel %q", tt.in, string(got))
			}
		})
	}
}

func TestParseRiskRoundTripsAllValidLevels(t *testing.T) {
	for _, r := range []RiskLevel{RiskLow, RiskMedium, RiskHigh, RiskReleaseBlocker} {
		got, ok := ParseRisk(string(r))
		if !ok || got != r {
			t.Errorf("ParseRisk(%q) = (%q, %v), want (%q, true)", string(r), string(got), ok, string(r))
		}
	}
}
