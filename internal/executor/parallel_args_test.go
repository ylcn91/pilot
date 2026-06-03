package executor

import (
	"slices"
	"testing"
)

func TestBuildSubagentArgs(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		prompt string
		want   []string
	}{
		{
			name:   "no model leaves base args untouched",
			model:  "",
			prompt: "explore the codebase",
			want:   []string{"-p", "explore the codebase", "--output-format", "text"},
		},
		{
			name:   "model is passed as two separate argv elements",
			model:  "haiku",
			prompt: "find callers",
			want:   []string{"--model", "haiku", "-p", "find callers", "--output-format", "text"},
		},
		{
			name:   "sonnet model",
			model:  "sonnet",
			prompt: "analyze",
			want:   []string{"--model", "sonnet", "-p", "analyze", "--output-format", "text"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSubagentArgs(tt.model, tt.prompt)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("buildSubagentArgs(%q, %q) = %v, want %v", tt.model, tt.prompt, got, tt.want)
			}
		})
	}
}

// TestBuildSubagentArgs_ModelFlagNotCombined guards against the regression
// where "--model haiku" was prepended as a single argv element, which the
// claude CLI does not parse as a flag+value pair.
func TestBuildSubagentArgs_ModelFlagNotCombined(t *testing.T) {
	args := buildSubagentArgs("haiku", "p")
	for _, a := range args {
		if a == "--model haiku" {
			t.Fatalf("model flag was combined into a single arg %q; must be two separate args", a)
		}
	}
	idx := slices.Index(args, "--model")
	if idx < 0 || idx+1 >= len(args) || args[idx+1] != "haiku" {
		t.Fatalf("expected --model immediately followed by haiku, got %v", args)
	}
}
