package azuredevops

import "time"

// Pull request states
const (
	PRStateActive    = "active"
	PRStateCompleted = "completed"
	PRStateAbandoned = "abandoned"
)

// Merge status values
const (
	MergeStatusSucceeded = "succeeded"
	MergeStatusConflicts = "conflicts"
	MergeStatusFailure   = "failure"
	MergeStatusQueued    = "queued"
)

// PullRequest represents an Azure DevOps pull request
type PullRequest struct {
	PullRequestID         int           `json:"pullRequestId"`
	Title                 string        `json:"title"`
	Description           string        `json:"description"`
	Status                string        `json:"status"` // active, completed, abandoned
	SourceRefName         string        `json:"sourceRefName"`
	TargetRefName         string        `json:"targetRefName"`
	MergeStatus           string        `json:"mergeStatus"`
	IsDraft               bool          `json:"isDraft"`
	CreationDate          time.Time     `json:"creationDate"`
	ClosedDate            time.Time     `json:"closedDate,omitempty"`
	URL                   string        `json:"url"`
	Repository            *GitRepo      `json:"repository,omitempty"`
	CreatedBy             *Identity     `json:"createdBy,omitempty"`
	MergeID               string        `json:"mergeId,omitempty"`
	LastMergeSourceCommit *GitCommitRef `json:"lastMergeSourceCommit,omitempty"`
}

// GitRepo represents a Git repository reference
type GitRepo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Project *Project `json:"project,omitempty"`
}

// Project represents an Azure DevOps project reference
type Project struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	State string `json:"state"`
}

// Identity represents an Azure DevOps user identity
type Identity struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	UniqueName  string `json:"uniqueName"`
	URL         string `json:"url"`
	ImageURL    string `json:"imageUrl,omitempty"`
}

// GitCommitRef represents a Git commit reference
type GitCommitRef struct {
	CommitID string `json:"commitId"`
	URL      string `json:"url"`
}

// PullRequestInput is used for creating pull requests
type PullRequestInput struct {
	Title         string `json:"title"`
	Description   string `json:"description,omitempty"`
	SourceRefName string `json:"sourceRefName"` // refs/heads/branch-name
	TargetRefName string `json:"targetRefName"` // refs/heads/main
	IsDraft       bool   `json:"isDraft,omitempty"`
}

// GitRef represents a Git reference (branch/tag)
type GitRef struct {
	Name     string `json:"name"`     // refs/heads/branch-name
	ObjectID string `json:"objectId"` // Commit SHA
	URL      string `json:"url"`
}

// GitRefUpdate is used for creating/updating branches
type GitRefUpdate struct {
	Name        string `json:"name"`        // refs/heads/branch-name
	OldObjectID string `json:"oldObjectId"` // 40 zeros for new branch
	NewObjectID string `json:"newObjectId"` // Target commit SHA
}

// Comment represents a work item comment
type Comment struct {
	ID           int       `json:"id"`
	Text         string    `json:"text"`
	CreatedBy    *Identity `json:"createdBy,omitempty"`
	CreatedDate  time.Time `json:"createdDate"`
	ModifiedDate time.Time `json:"modifiedDate,omitempty"`
}
