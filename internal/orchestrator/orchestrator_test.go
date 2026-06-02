package orchestrator

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/linear"
)

func TestExtractLabelNames(t *testing.T) {
	tests := []struct {
		name   string
		labels []linear.Label
		want   []string
	}{
		{
			name:   "empty labels",
			labels: []linear.Label{},
			want:   []string{},
		},
		{
			name:   "nil labels",
			labels: nil,
			want:   []string{},
		},
		{
			name: "single label",
			labels: []linear.Label{
				{ID: "1", Name: "bug"},
			},
			want: []string{"bug"},
		},
		{
			name: "multiple labels",
			labels: []linear.Label{
				{ID: "1", Name: "bug"},
				{ID: "2", Name: "priority-high"},
				{ID: "3", Name: "backend"},
			},
			want: []string{"bug", "priority-high", "backend"},
		},
		{
			name: "labels with empty name",
			labels: []linear.Label{
				{ID: "1", Name: ""},
				{ID: "2", Name: "feature"},
			},
			want: []string{"", "feature"},
		},
		{
			name: "labels with special characters",
			labels: []linear.Label{
				{ID: "1", Name: "pilot:active"},
				{ID: "2", Name: "tech-debt"},
				{ID: "3", Name: "enhancement (v2)"},
			},
			want: []string{"pilot:active", "tech-debt", "enhancement (v2)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLabelNames(tt.labels)

			if len(got) != len(tt.want) {
				t.Errorf("extractLabelNames() returned %d items, want %d", len(got), len(tt.want))
				return
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("item %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTaskStruct(t *testing.T) {
	tests := []struct {
		name string
		task *Task
		want struct {
			id          string
			projectPath string
			branch      string
			priority    float64
		}
	}{
		{
			name: "basic task creation",
			task: &Task{
				ID:          "TASK-001",
				ProjectPath: "/home/user/project",
				Branch:      "pilot/TASK-001",
				Priority:    1.0,
			},
			want: struct {
				id          string
				projectPath string
				branch      string
				priority    float64
			}{
				id:          "TASK-001",
				projectPath: "/home/user/project",
				branch:      "pilot/TASK-001",
				priority:    1.0,
			},
		},
		{
			name: "task with zero priority",
			task: &Task{
				ID:          "TASK-002",
				ProjectPath: "/var/projects/app",
				Branch:      "pilot/ABC-123",
				Priority:    0.0,
			},
			want: struct {
				id          string
				projectPath string
				branch      string
				priority    float64
			}{
				id:          "TASK-002",
				projectPath: "/var/projects/app",
				branch:      "pilot/ABC-123",
				priority:    0.0,
			},
		},
		{
			name: "task with high priority",
			task: &Task{
				ID:          "URGENT-001",
				ProjectPath: "/projects/critical",
				Branch:      "pilot/URGENT-001",
				Priority:    10.5,
			},
			want: struct {
				id          string
				projectPath string
				branch      string
				priority    float64
			}{
				id:          "URGENT-001",
				projectPath: "/projects/critical",
				branch:      "pilot/URGENT-001",
				priority:    10.5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.task.ID != tt.want.id {
				t.Errorf("Task.ID = %s, want %s", tt.task.ID, tt.want.id)
			}
			if tt.task.ProjectPath != tt.want.projectPath {
				t.Errorf("Task.ProjectPath = %s, want %s", tt.task.ProjectPath, tt.want.projectPath)
			}
			if tt.task.Branch != tt.want.branch {
				t.Errorf("Task.Branch = %s, want %s", tt.task.Branch, tt.want.branch)
			}
			if tt.task.Priority != tt.want.priority {
				t.Errorf("Task.Priority = %f, want %f", tt.task.Priority, tt.want.priority)
			}
		})
	}
}

func TestTaskWithLinearIssue(t *testing.T) {
	issue := &linear.Issue{
		ID:          "issue-uuid-123",
		Identifier:  "ABC-42",
		Title:       "Implement feature X",
		Description: "Detailed description of feature X",
		Priority:    2,
		Labels: []linear.Label{
			{ID: "l1", Name: "enhancement"},
			{ID: "l2", Name: "backend"},
		},
	}

	doc := &TaskDocument{
		ID:       "TASK-ABC-42",
		Title:    "Implement feature X",
		Markdown: "# Task: Implement feature X\n\nDescription here.",
	}

	task := &Task{
		ID:          doc.ID,
		Ticket:      issue,
		Document:    doc,
		ProjectPath: "/path/to/project",
		Branch:      "pilot/ABC-42",
		Priority:    float64(issue.Priority),
	}

	if task.ID != "TASK-ABC-42" {
		t.Errorf("Task.ID = %s, want TASK-ABC-42", task.ID)
	}

	if task.Ticket.Title != "Implement feature X" {
		t.Errorf("Task.Ticket.Title = %s, want 'Implement feature X'", task.Ticket.Title)
	}

	if task.Document.Markdown != "# Task: Implement feature X\n\nDescription here." {
		t.Errorf("Task.Document.Markdown mismatch")
	}

	if task.Branch != "pilot/ABC-42" {
		t.Errorf("Task.Branch = %s, want pilot/ABC-42", task.Branch)
	}
}

func TestConfigDefaults(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   struct {
			model         string
			maxConcurrent int
		}
	}{
		{
			name:   "empty config",
			config: &Config{},
			want: struct {
				model         string
				maxConcurrent int
			}{
				model:         "",
				maxConcurrent: 0,
			},
		},
		{
			name: "config with values",
			config: &Config{
				Model:         "claude-3",
				MaxConcurrent: 4,
			},
			want: struct {
				model         string
				maxConcurrent int
			}{
				model:         "claude-3",
				maxConcurrent: 4,
			},
		},
		{
			name: "config with only model",
			config: &Config{
				Model: "gpt-4",
			},
			want: struct {
				model         string
				maxConcurrent int
			}{
				model:         "gpt-4",
				maxConcurrent: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.Model != tt.want.model {
				t.Errorf("Config.Model = %s, want %s", tt.config.Model, tt.want.model)
			}
			if tt.config.MaxConcurrent != tt.want.maxConcurrent {
				t.Errorf("Config.MaxConcurrent = %d, want %d", tt.config.MaxConcurrent, tt.want.maxConcurrent)
			}
		})
	}
}

func TestExtractLabelNamesOrderPreservation(t *testing.T) {
	// Test that label order is preserved
	labels := []linear.Label{
		{ID: "1", Name: "first"},
		{ID: "2", Name: "second"},
		{ID: "3", Name: "third"},
		{ID: "4", Name: "fourth"},
		{ID: "5", Name: "fifth"},
	}

	got := extractLabelNames(labels)

	expected := []string{"first", "second", "third", "fourth", "fifth"}
	for i, name := range got {
		if name != expected[i] {
			t.Errorf("Label order not preserved at index %d: got %q, want %q", i, name, expected[i])
		}
	}
}

func TestExtractLabelNamesLargeInput(t *testing.T) {
	// Test with a larger number of labels
	labelCount := 100
	labels := make([]linear.Label, labelCount)
	for i := 0; i < labelCount; i++ {
		labels[i] = linear.Label{
			ID:   "id-" + string(rune('a'+i%26)),
			Name: "label-" + string(rune('a'+i%26)),
		}
	}

	got := extractLabelNames(labels)

	if len(got) != labelCount {
		t.Errorf("extractLabelNames() returned %d items, want %d", len(got), labelCount)
	}

	// Verify first and last elements
	if got[0] != "label-a" {
		t.Errorf("First label = %s, want label-a", got[0])
	}
	if got[labelCount-1] != "label-"+string(rune('a'+(labelCount-1)%26)) {
		t.Errorf("Last label mismatch")
	}
}
