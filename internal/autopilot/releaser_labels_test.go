package autopilot

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// #4: pr_labels version strategy label->bump mapping.
func TestDetectBumpFromLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   BumpType
	}{
		{name: "semver:major", labels: []string{"semver:major"}, want: BumpMajor},
		{name: "semver:minor", labels: []string{"semver:minor"}, want: BumpMinor},
		{name: "semver:patch", labels: []string{"semver:patch"}, want: BumpPatch},
		{name: "breaking alias", labels: []string{"breaking"}, want: BumpMajor},
		{name: "feature alias", labels: []string{"feature"}, want: BumpMinor},
		{name: "feat alias", labels: []string{"feat"}, want: BumpMinor},
		{name: "fix alias", labels: []string{"fix"}, want: BumpPatch},
		{name: "bugfix alias", labels: []string{"bugfix"}, want: BumpPatch},
		{name: "case insensitive", labels: []string{"SemVer:Major"}, want: BumpMajor},
		{name: "unrecognised label", labels: []string{"documentation"}, want: BumpNone},
		{name: "no labels", labels: nil, want: BumpNone},
		{name: "highest wins", labels: []string{"fix", "feature", "semver:major"}, want: BumpMajor},
		{name: "minor over patch", labels: []string{"fix", "feat"}, want: BumpMinor},
		{name: "mixed recognised and noise", labels: []string{"pilot", "needs-review", "semver:patch"}, want: BumpPatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := make([]github.Label, len(tt.labels))
			for i, name := range tt.labels {
				labels[i] = github.Label{Name: name}
			}
			if got := DetectBumpFromLabels(labels); got != tt.want {
				t.Errorf("DetectBumpFromLabels(%v) = %v, want %v", tt.labels, got, tt.want)
			}
		})
	}
}
