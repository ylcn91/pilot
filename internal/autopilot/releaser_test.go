package autopilot

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

func TestParseSemVer(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    SemVer
		wantErr bool
	}{
		{
			name:  "v prefix",
			input: "v1.2.3",
			want:  SemVer{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name:  "no prefix",
			input: "1.2.3",
			want:  SemVer{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name:  "V prefix uppercase",
			input: "V1.2.3",
			want:  SemVer{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name:  "with pre-release suffix",
			input: "v1.2.3-beta",
			want:  SemVer{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name:  "with pre-release and build",
			input: "v1.2.3-beta.1+build.123",
			want:  SemVer{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name:  "zero version",
			input: "v0.0.0",
			want:  SemVer{Major: 0, Minor: 0, Patch: 0},
		},
		{
			name:  "large numbers",
			input: "v10.20.30",
			want:  SemVer{Major: 10, Minor: 20, Patch: 30},
		},
		{
			name:    "invalid - too few parts",
			input:   "v1.2",
			wantErr: true,
		},
		{
			name:    "invalid - too many parts",
			input:   "v1.2.3.4",
			wantErr: true,
		},
		{
			name:    "invalid - non-numeric major",
			input:   "va.2.3",
			wantErr: true,
		},
		{
			name:    "invalid - non-numeric minor",
			input:   "v1.b.3",
			wantErr: true,
		},
		{
			name:    "invalid - non-numeric patch",
			input:   "v1.2.c",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSemVer(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSemVer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseSemVer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSemVer_String(t *testing.T) {
	tests := []struct {
		name   string
		ver    SemVer
		prefix string
		want   string
	}{
		{
			name:   "with v prefix",
			ver:    SemVer{Major: 1, Minor: 2, Patch: 3},
			prefix: "v",
			want:   "v1.2.3",
		},
		{
			name:   "no prefix",
			ver:    SemVer{Major: 1, Minor: 2, Patch: 3},
			prefix: "",
			want:   "1.2.3",
		},
		{
			name:   "zero version",
			ver:    SemVer{Major: 0, Minor: 0, Patch: 0},
			prefix: "v",
			want:   "v0.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ver.String(tt.prefix)
			if got != tt.want {
				t.Errorf("SemVer.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSemVer_Bump(t *testing.T) {
	tests := []struct {
		name     string
		ver      SemVer
		bumpType BumpType
		want     SemVer
	}{
		{
			name:     "bump major",
			ver:      SemVer{Major: 1, Minor: 2, Patch: 3},
			bumpType: BumpMajor,
			want:     SemVer{Major: 2, Minor: 0, Patch: 0},
		},
		{
			name:     "bump minor",
			ver:      SemVer{Major: 1, Minor: 2, Patch: 3},
			bumpType: BumpMinor,
			want:     SemVer{Major: 1, Minor: 3, Patch: 0},
		},
		{
			name:     "bump patch",
			ver:      SemVer{Major: 1, Minor: 2, Patch: 3},
			bumpType: BumpPatch,
			want:     SemVer{Major: 1, Minor: 2, Patch: 4},
		},
		{
			name:     "bump none - no change",
			ver:      SemVer{Major: 1, Minor: 2, Patch: 3},
			bumpType: BumpNone,
			want:     SemVer{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name:     "bump major from zero",
			ver:      SemVer{Major: 0, Minor: 0, Patch: 0},
			bumpType: BumpMajor,
			want:     SemVer{Major: 1, Minor: 0, Patch: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ver.Bump(tt.bumpType)
			if got != tt.want {
				t.Errorf("SemVer.Bump() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectBumpType(t *testing.T) {
	tests := []struct {
		name     string
		messages []string
		want     BumpType
	}{
		{
			name:     "feat - minor bump",
			messages: []string{"feat: add new feature"},
			want:     BumpMinor,
		},
		{
			name:     "fix - patch bump",
			messages: []string{"fix: resolve bug"},
			want:     BumpPatch,
		},
		{
			name:     "breaking change marker - major bump",
			messages: []string{"feat!: breaking change"},
			want:     BumpMajor,
		},
		{
			name:     "feature with scope",
			messages: []string{"feat(api): add new endpoint"},
			want:     BumpMinor,
		},
		{
			name:     "chore - no bump",
			messages: []string{"chore: update dependencies"},
			want:     BumpNone,
		},
		{
			name:     "docs - no bump",
			messages: []string{"docs: update readme"},
			want:     BumpNone,
		},
		{
			name:     "multiple commits - highest wins",
			messages: []string{"fix: small fix", "feat: new feature", "chore: cleanup"},
			want:     BumpMinor,
		},
		{
			name:     "breaking with other commits",
			messages: []string{"feat: new feature", "fix!: breaking fix"},
			want:     BumpMajor,
		},
		{
			name:     "perf - patch bump",
			messages: []string{"perf: improve speed"},
			want:     BumpPatch,
		},
		{
			name:     "non-conventional commit - no bump",
			messages: []string{"Update something"},
			want:     BumpNone,
		},
		{
			name:     "empty commits",
			messages: []string{},
			want:     BumpNone,
		},
		{
			name:     "multiline commit message - first line only",
			messages: []string{"feat: add feature\n\nThis is a longer description"},
			want:     BumpMinor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commits := make([]*github.Commit, len(tt.messages))
			for i, msg := range tt.messages {
				commits[i] = makeCommit(msg)
			}
			got := DetectBumpType(commits)
			if got != tt.want {
				t.Errorf("DetectBumpType() = %v, want %v", got, tt.want)
			}
		})
	}
}
