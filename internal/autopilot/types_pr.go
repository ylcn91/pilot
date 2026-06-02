package autopilot

import (
	"strings"
	"sync"
	"time"
)

// PRStage represents stages in the PR lifecycle.
type PRStage string

const (
	// StagePRCreated indicates a PR has been created and is ready for processing.
	StagePRCreated PRStage = "pr_created"
	// StageWaitingCI indicates the PR is waiting for CI checks to complete.
	StageWaitingCI PRStage = "waiting_ci"
	// StageCIPassed indicates all CI checks have passed.
	StageCIPassed PRStage = "ci_passed"
	// StageCIFailed indicates one or more CI checks have failed.
	StageCIFailed PRStage = "ci_failed"
	// StageAwaitApproval indicates the PR is waiting for human approval.
	StageAwaitApproval PRStage = "awaiting_approval"
	// StageMerging indicates the PR is being merged.
	StageMerging PRStage = "merging"
	// StageMerged indicates the PR has been successfully merged.
	StageMerged PRStage = "merged"
	// StagePostMergeCI indicates post-merge CI is running on main branch.
	StagePostMergeCI PRStage = "post_merge_ci"
	// StageReleasing indicates the PR is triggering an automatic release.
	StageReleasing PRStage = "releasing"
	// StageReviewRequested indicates a human reviewer requested changes on the PR.
	StageReviewRequested PRStage = "review_requested"
	// StageFailed indicates the PR pipeline has failed and requires intervention.
	StageFailed PRStage = "failed"
)

// AllPRStages returns every defined PRStage value. Used by the Prometheus exporter
// to emit zero-values for stages absent from the current snapshot, preventing
// Prometheus's 5-min lookback from holding stale non-zero values.
func AllPRStages() []PRStage {
	return []PRStage{
		StagePRCreated,
		StageWaitingCI,
		StageCIPassed,
		StageCIFailed,
		StageAwaitApproval,
		StageMerging,
		StageMerged,
		StagePostMergeCI,
		StageReleasing,
		StageReviewRequested,
		StageFailed,
	}
}

// CIStatus represents the current CI check state.
type CIStatus string

const (
	// CIPending indicates CI checks have not started yet.
	CIPending CIStatus = "pending"
	// CIRunning indicates CI checks are currently executing.
	CIRunning CIStatus = "running"
	// CISuccess indicates all CI checks have passed.
	CISuccess CIStatus = "success"
	// CIFailure indicates one or more CI checks have failed.
	CIFailure CIStatus = "failure"
)

// BumpType represents semantic version bump types.
type BumpType string

const (
	// BumpNone indicates no version bump is needed.
	BumpNone BumpType = "none"
	// BumpPatch indicates a patch version bump (bug fixes).
	BumpPatch BumpType = "patch"
	// BumpMinor indicates a minor version bump (new features).
	BumpMinor BumpType = "minor"
	// BumpMajor indicates a major version bump (breaking changes).
	BumpMajor BumpType = "major"
)

// ShortSHA returns a short version of a SHA, safely handling short strings.
func ShortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

// PRState tracks the lifecycle state of a pull request through the autopilot pipeline.
//
// Concurrency: a live *PRState stored in Controller.activePRs is shared across the
// main processing loop and webhook goroutines. The embedded mu guards every field
// below. Holders of the live pointer MUST take mu before reading/writing fields
// (see TASK-324). The no-deadlock invariant is: always acquire PRState.mu BEFORE
// Controller.mu, never the reverse.
//
// Because PRState now contains a sync.Mutex, a populated value must never be
// copied (go vet copylocks). Use snapshot() to hand a detached, lock-free copy to
// read-only consumers. state_store.go constructs a fresh zero-value `var pr PRState`
// before populating it, which is fine.
type PRState struct {
	// mu guards all fields below for the live pointer held in Controller.activePRs.
	mu sync.Mutex
	// PRNumber is the GitHub PR number.
	PRNumber int
	// PRURL is the full URL to the PR.
	PRURL string
	// IssueNumber is the linked issue number (if any).
	IssueNumber int
	// BranchName is the head branch of the PR (e.g. "pilot/GH-123").
	BranchName string
	// HeadSHA is the commit SHA at the head of the PR.
	HeadSHA string
	// Stage is the current stage in the PR lifecycle.
	Stage PRStage
	// CIStatus is the current CI check status.
	CIStatus CIStatus
	// LastChecked is when the PR status was last polled.
	LastChecked time.Time
	// CIWaitStartedAt is when CI monitoring started (for timeout tracking).
	CIWaitStartedAt time.Time
	// MergeAttempts counts how many times merge has been attempted.
	MergeAttempts int
	// Error holds the last error message if Stage is StageFailed.
	Error string
	// CreatedAt is when the PR entered the autopilot pipeline.
	CreatedAt time.Time
	// ReleaseVersion is the version that was released (if any).
	ReleaseVersion string
	// ReleaseBumpType is the detected bump type from commits.
	ReleaseBumpType BumpType
	// DiscoveredChecks holds check names found in auto mode.
	DiscoveredChecks []string
	// ConsecutiveAPIFailures counts consecutive CI check API failures.
	ConsecutiveAPIFailures int
	// EnvironmentName is the user-friendly environment label (e.g. "staging").
	EnvironmentName string
	// PRTitle is the title of the pull request.
	PRTitle string
	// TargetBranch is the base branch the PR merges into (e.g. "main").
	TargetBranch string
	// IssueNodeID is the GraphQL global node ID of the linked issue, used for board sync.
	IssueNodeID string
	// MergeNotificationPosted is true once the merge-completion comment has been
	// posted to the linked issue. Prevents duplicate comments on state-machine
	// re-entry for an already-merged PR (GH-2345).
	MergeNotificationPosted bool
	// ApprovalRequestID holds the ID of the submitted async approval request (set on first tick in StageAwaitApproval).
	ApprovalRequestID string
	// ApprovalDecision holds the recorded async approval decision ("approved", "rejected", "timeout").
	ApprovalDecision string
	// ApprovalRequestedAt is when the async approval request was first submitted.
	ApprovalRequestedAt time.Time
	// PostMergeSHA is the main branch SHA captured on first entry to StagePostMergeCI.
	// Persisted so a daemon restart resumes monitoring the same commit.
	PostMergeSHA string
	// PostMergeCIStartedAt is when StagePostMergeCI monitoring began (for timeout tracking).
	PostMergeCIStartedAt time.Time
}

// snapshot returns a detached, field-by-field copy of the PRState with a fresh
// (zero-value) mutex. The caller MUST hold ps.mu while calling this so the read of
// every field is race-free; the returned *PRState is independent of the live one
// and safe to hand to read-only consumers (metrics, dashboard, gateway) without any
// lock. It deliberately does NOT use `cp := *ps`, which would copy the mutex and
// trip go vet copylocks.
func (ps *PRState) snapshot() *PRState {
	cp := &PRState{
		PRNumber:                ps.PRNumber,
		PRURL:                   ps.PRURL,
		IssueNumber:             ps.IssueNumber,
		BranchName:              ps.BranchName,
		HeadSHA:                 ps.HeadSHA,
		Stage:                   ps.Stage,
		CIStatus:                ps.CIStatus,
		LastChecked:             ps.LastChecked,
		CIWaitStartedAt:         ps.CIWaitStartedAt,
		MergeAttempts:           ps.MergeAttempts,
		Error:                   ps.Error,
		CreatedAt:               ps.CreatedAt,
		ReleaseVersion:          ps.ReleaseVersion,
		ReleaseBumpType:         ps.ReleaseBumpType,
		ConsecutiveAPIFailures:  ps.ConsecutiveAPIFailures,
		EnvironmentName:         ps.EnvironmentName,
		PRTitle:                 ps.PRTitle,
		TargetBranch:            ps.TargetBranch,
		IssueNodeID:             ps.IssueNodeID,
		MergeNotificationPosted: ps.MergeNotificationPosted,
		ApprovalRequestID:       ps.ApprovalRequestID,
		ApprovalDecision:        ps.ApprovalDecision,
		ApprovalRequestedAt:     ps.ApprovalRequestedAt,
		PostMergeSHA:            ps.PostMergeSHA,
		PostMergeCIStartedAt:    ps.PostMergeCIStartedAt,
	}
	// DiscoveredChecks is a slice — copy the backing array so consumers can't
	// mutate the live PR's slice through the snapshot.
	if ps.DiscoveredChecks != nil {
		cp.DiscoveredChecks = make([]string, len(ps.DiscoveredChecks))
		copy(cp.DiscoveredChecks, ps.DiscoveredChecks)
	}
	return cp
}

// RepoOwnerAndName extracts the repository owner and name from the PR URL.
// Falls back to the provided defaults if the URL is missing or unparseable.
func (ps *PRState) RepoOwnerAndName(fallbackOwner, fallbackRepo string) (string, string) {
	if ps.PRURL != "" {
		trimmed := strings.TrimPrefix(ps.PRURL, "https://github.com/")
		if trimmed != ps.PRURL { // prefix was actually present
			parts := strings.Split(trimmed, "/")
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1]
			}
		}
	}
	return fallbackOwner, fallbackRepo
}
