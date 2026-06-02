package linear

import "time"

// Issue represents a Linear issue
type Issue struct {
	ID          string    `json:"id"`
	Identifier  string    `json:"identifier"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Priority    int       `json:"priority"`
	State       State     `json:"state"`
	Labels      []Label   `json:"labels"`
	Assignee    *User     `json:"assignee"`
	Project     *Project  `json:"project"`
	Team        Team      `json:"team"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// State represents an issue state
type State struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Label represents a Linear label
type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// User represents a Linear user
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Project represents a Linear project
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Team represents a Linear team
type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}

// issueListItem is the raw GraphQL response for an issue (labels have nested nodes)
type issueListItem struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	State       State  `json:"state"`
	Labels      struct {
		Nodes []Label `json:"nodes"`
	} `json:"labels"`
	Assignee  *User     `json:"assignee"`
	Project   *Project  `json:"project"`
	Team      Team      `json:"team"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// toIssue converts the response to an Issue
func (r *issueListItem) toIssue() *Issue {
	return &Issue{
		ID:          r.ID,
		Identifier:  r.Identifier,
		Title:       r.Title,
		Description: r.Description,
		Priority:    r.Priority,
		State:       r.State,
		Labels:      r.Labels.Nodes,
		Assignee:    r.Assignee,
		Project:     r.Project,
		Team:        r.Team,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
