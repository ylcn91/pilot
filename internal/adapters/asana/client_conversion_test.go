package asana

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled != false {
		t.Errorf("default Enabled = %v, want false", cfg.Enabled)
	}
	if cfg.PilotTag != "pilot" {
		t.Errorf("default PilotTag = %s, want 'pilot'", cfg.PilotTag)
	}
}

func TestPriorityFromTags(t *testing.T) {
	tests := []struct {
		name string
		tags []Tag
		want Priority
	}{
		{
			name: "urgent tag",
			tags: []Tag{{GID: "1", Name: "urgent"}},
			want: PriorityUrgent,
		},
		{
			name: "high tag",
			tags: []Tag{{GID: "1", Name: "High"}},
			want: PriorityHigh,
		},
		{
			name: "medium tag",
			tags: []Tag{{GID: "1", Name: "MEDIUM"}},
			want: PriorityMedium,
		},
		{
			name: "low tag",
			tags: []Tag{{GID: "1", Name: "low"}},
			want: PriorityLow,
		},
		{
			name: "critical tag",
			tags: []Tag{{GID: "1", Name: "Critical"}},
			want: PriorityUrgent,
		},
		{
			name: "no priority tags",
			tags: []Tag{{GID: "1", Name: "bug"}, {GID: "2", Name: "feature"}},
			want: PriorityNone,
		},
		{
			name: "empty tags",
			tags: []Tag{},
			want: PriorityNone,
		},
		{
			name: "priority in mixed tags",
			tags: []Tag{{GID: "1", Name: "bug"}, {GID: "2", Name: "urgent"}, {GID: "3", Name: "feature"}},
			want: PriorityUrgent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PriorityFromTags(tt.tags)
			if got != tt.want {
				t.Errorf("PriorityFromTags() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPriorityName(t *testing.T) {
	tests := []struct {
		priority Priority
		want     string
	}{
		{PriorityUrgent, "Urgent"},
		{PriorityHigh, "High"},
		{PriorityMedium, "Medium"},
		{PriorityLow, "Low"},
		{PriorityNone, "No Priority"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := PriorityName(tt.priority)
			if got != tt.want {
				t.Errorf("PriorityName(%d) = %s, want %s", tt.priority, got, tt.want)
			}
		})
	}
}

func TestConvertToTaskInfo(t *testing.T) {
	task := &Task{
		GID:   "123456",
		Name:  "Implement feature",
		Notes: "Feature description here",
		Tags: []Tag{
			{GID: "1", Name: "pilot"},
			{GID: "2", Name: "urgent"},
		},
		Projects: []Project{
			{GID: "proj-1", Name: "Main Project"},
		},
		Permalink: "https://app.asana.com/0/0/123456",
	}

	info := ConvertToTaskInfo(task)

	if info.ID != "ASANA-123456" {
		t.Errorf("info.ID = %s, want ASANA-123456", info.ID)
	}
	if info.Title != "Implement feature" {
		t.Errorf("info.Title = %s, want Implement feature", info.Title)
	}
	if info.Description != "Feature description here" {
		t.Errorf("info.Description = %s, want Feature description here", info.Description)
	}
	if info.Priority != PriorityUrgent {
		t.Errorf("info.Priority = %d, want %d", info.Priority, PriorityUrgent)
	}
	if len(info.Labels) != 2 {
		t.Errorf("len(info.Labels) = %d, want 2", len(info.Labels))
	}
	if info.TaskGID != "123456" {
		t.Errorf("info.TaskGID = %s, want 123456", info.TaskGID)
	}
	if info.TaskURL != "https://app.asana.com/0/0/123456" {
		t.Errorf("info.TaskURL = %s, want https://app.asana.com/0/0/123456", info.TaskURL)
	}
	if info.ProjectName != "Main Project" {
		t.Errorf("info.ProjectName = %s, want Main Project", info.ProjectName)
	}
}

func TestConvertToTaskInfo_NoPermalink(t *testing.T) {
	task := &Task{
		GID:  "123456",
		Name: "Task without permalink",
	}

	info := ConvertToTaskInfo(task)

	expectedURL := "https://app.asana.com/0/0/123456"
	if info.TaskURL != expectedURL {
		t.Errorf("info.TaskURL = %s, want %s", info.TaskURL, expectedURL)
	}
}
