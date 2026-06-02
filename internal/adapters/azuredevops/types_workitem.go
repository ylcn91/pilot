package azuredevops

import "time"

// Work item states
const (
	StateNew      = "New"
	StateActive   = "Active"
	StateResolved = "Resolved"
	StateClosed   = "Closed"
)

// Tag names used by Pilot
const (
	TagInProgress = "pilot-in-progress"
	TagDone       = "pilot-done"
	TagFailed     = "pilot-failed"
)

// Priority mapping from Azure DevOps priority field
type Priority int

const (
	PriorityNone   Priority = 0
	PriorityUrgent Priority = 1
	PriorityHigh   Priority = 2
	PriorityMedium Priority = 3
	PriorityLow    Priority = 4
)

// PriorityFromValue converts Azure DevOps priority value to internal Priority
// Azure DevOps uses numeric priority: 1 = highest, 4 = lowest
func PriorityFromValue(value int) Priority {
	switch value {
	case 1:
		return PriorityUrgent
	case 2:
		return PriorityHigh
	case 3:
		return PriorityMedium
	case 4:
		return PriorityLow
	default:
		return PriorityNone
	}
}

// PriorityName returns the human-readable priority name
func PriorityName(priority Priority) string {
	switch priority {
	case PriorityUrgent:
		return "Urgent"
	case PriorityHigh:
		return "High"
	case PriorityMedium:
		return "Medium"
	case PriorityLow:
		return "Low"
	default:
		return "No Priority"
	}
}

// WorkItem represents an Azure DevOps work item
type WorkItem struct {
	ID     int                    `json:"id"`
	Rev    int                    `json:"rev"`
	Fields map[string]interface{} `json:"fields"`
	URL    string                 `json:"url"`
}

// GetTitle returns the work item title
func (w *WorkItem) GetTitle() string {
	if title, ok := w.Fields["System.Title"].(string); ok {
		return title
	}
	return ""
}

// GetDescription returns the work item description (HTML)
func (w *WorkItem) GetDescription() string {
	// Azure DevOps stores description in System.Description for Bugs
	// and in System.Description or Microsoft.VSTS.TCM.ReproSteps for different types
	if desc, ok := w.Fields["System.Description"].(string); ok {
		return desc
	}
	if desc, ok := w.Fields["Microsoft.VSTS.TCM.ReproSteps"].(string); ok {
		return desc
	}
	return ""
}

// GetState returns the work item state
func (w *WorkItem) GetState() string {
	if state, ok := w.Fields["System.State"].(string); ok {
		return state
	}
	return ""
}

// GetWorkItemType returns the work item type (Bug, Task, User Story, etc.)
func (w *WorkItem) GetWorkItemType() string {
	if wit, ok := w.Fields["System.WorkItemType"].(string); ok {
		return wit
	}
	return ""
}

// GetTags returns the work item tags as a slice
// Azure DevOps stores tags as semicolon-separated string
func (w *WorkItem) GetTags() []string {
	if tagsStr, ok := w.Fields["System.Tags"].(string); ok && tagsStr != "" {
		return splitTags(tagsStr)
	}
	return nil
}

// HasTag checks if the work item has a specific tag
func (w *WorkItem) HasTag(tag string) bool {
	for _, t := range w.GetTags() {
		if t == tag {
			return true
		}
	}
	return false
}

// GetPriority returns the work item priority as internal Priority type
func (w *WorkItem) GetPriority() Priority {
	if priority, ok := w.Fields["Microsoft.VSTS.Common.Priority"].(float64); ok {
		return PriorityFromValue(int(priority))
	}
	return PriorityNone
}

// GetCreatedDate returns the work item creation date
func (w *WorkItem) GetCreatedDate() time.Time {
	if dateStr, ok := w.Fields["System.CreatedDate"].(string); ok {
		t, _ := time.Parse(time.RFC3339, dateStr)
		return t
	}
	return time.Time{}
}

// GetChangedDate returns the work item last changed date
func (w *WorkItem) GetChangedDate() time.Time {
	if dateStr, ok := w.Fields["System.ChangedDate"].(string); ok {
		t, _ := time.Parse(time.RFC3339, dateStr)
		return t
	}
	return time.Time{}
}

// GetWebURL constructs the web URL for the work item
func (w *WorkItem) GetWebURL(baseURL, organization, project string) string {
	return baseURL + "/" + organization + "/" + project + "/_workitems/edit/" + string(rune(w.ID))
}
