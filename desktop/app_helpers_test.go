package main

import "testing"

func TestIssueURL(t *testing.T) {
	tests := []struct {
		name   string
		taskID string
		repo   string
		want   string
	}{
		{
			name:   "default repo when unset",
			taskID: "GH-123",
			repo:   "",
			want:   "https://github.com/ylcn91/pilot/issues/123",
		},
		{
			name:   "configured repo overrides default",
			taskID: "GH-456",
			repo:   "acme/widgets",
			want:   "https://github.com/acme/widgets/issues/456",
		},
		{
			name:   "non-github task id yields empty",
			taskID: "LINEAR-789",
			repo:   "acme/widgets",
			want:   "",
		},
		{
			name:   "single-slash prefix keeps trailing GH segment",
			taskID: "owner/GH-7",
			repo:   "",
			want:   "https://github.com/ylcn91/pilot/issues/7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := issueURL(tt.taskID, tt.repo); got != tt.want {
				t.Errorf("issueURL(%q, %q) = %q, want %q", tt.taskID, tt.repo, got, tt.want)
			}
		})
	}
}
