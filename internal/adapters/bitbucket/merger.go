package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
)

// MergeWaitResult represents the outcome of waiting for a PR to merge
type MergeWaitResult struct {
	Merged   bool   // PR was successfully merged
	Declined bool   // PR was declined without merging
	TimedOut bool   // Wait timed out
	PRNumber int    // The PR ID
	PRURL    string // The PR URL
	Message  string // Human-readable status message
}

// MergeWaiterConfig holds configuration for the merge waiter
type MergeWaiterConfig struct {
	PollInterval time.Duration // How often to check PR status
	Timeout      time.Duration // Max time to wait for merge
}

// DefaultMergeWaiterConfig returns sensible defaults
func DefaultMergeWaiterConfig() *MergeWaiterConfig {
	return &MergeWaiterConfig{
		PollInterval: 30 * time.Second,
		Timeout:      1 * time.Hour,
	}
}

// MergeWaiter waits for a PR to be merged
type MergeWaiter struct {
	client *Client
	config *MergeWaiterConfig
	logger *slog.Logger
}

// NewMergeWaiter creates a new merge waiter
func NewMergeWaiter(client *Client, config *MergeWaiterConfig) *MergeWaiter {
	if config == nil {
		config = DefaultMergeWaiterConfig()
	}
	return &MergeWaiter{
		client: client,
		config: config,
		logger: logging.WithComponent("bitbucket-merge-waiter"),
	}
}

// Common errors
var (
	ErrPRDeclined   = errors.New("PR was declined without merging")
	ErrMergeTimeout = errors.New("timed out waiting for PR merge")
	ErrBuildFailed  = errors.New("commit build failed")
)

// WaitForMerge polls the PR status until it's merged, declined, or times out
func (m *MergeWaiter) WaitForMerge(ctx context.Context, prID int) (*MergeWaitResult, error) {
	m.logger.Info("Waiting for PR merge",
		slog.Int("pr_id", prID),
		slog.Duration("timeout", m.config.Timeout),
		slog.Duration("poll_interval", m.config.PollInterval),
	)

	deadline := time.Now().Add(m.config.Timeout)
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		result, err := m.checkPRStatus(ctx, prID)
		if err != nil {
			return nil, fmt.Errorf("failed to check PR status: %w", err)
		}

		if result.Merged || result.Declined {
			return result, nil
		}

		select {
		case <-ctx.Done():
			return &MergeWaitResult{
				PRNumber: prID,
				Message:  "Context cancelled while waiting for merge",
			}, ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			m.logger.Warn("PR merge timed out",
				slog.Int("pr_id", prID),
				slog.Duration("timeout", m.config.Timeout),
			)
			return &MergeWaitResult{
				PRNumber: prID,
				TimedOut: true,
				Message:  fmt.Sprintf("Timed out waiting for PR #%d to merge after %s", prID, m.config.Timeout),
			}, ErrMergeTimeout
		}

		select {
		case <-ctx.Done():
			return &MergeWaitResult{
				PRNumber: prID,
				Message:  "Context cancelled while waiting for merge",
			}, ctx.Err()
		case <-ticker.C:
			m.logger.Debug("Polling PR status",
				slog.Int("pr_id", prID),
				slog.Duration("remaining", time.Until(deadline)),
			)
		}
	}
}

// checkPRStatus fetches and interprets the current PR status
func (m *MergeWaiter) checkPRStatus(ctx context.Context, prID int) (*MergeWaitResult, error) {
	pr, err := m.client.GetPullRequest(ctx, prID)
	if err != nil {
		return nil, err
	}

	result := &MergeWaitResult{
		PRNumber: prID,
	}
	if pr.Links != nil && pr.Links.HTML != nil {
		result.PRURL = pr.Links.HTML.Href
	}

	switch pr.State {
	case PRStateMerged:
		m.logger.Info("PR merged successfully", slog.Int("pr_id", prID))
		result.Merged = true
		result.Message = fmt.Sprintf("PR #%d was merged", prID)
		return result, nil
	case PRStateDeclined, PRStateSuperseded:
		m.logger.Warn("PR declined without merging", slog.Int("pr_id", prID))
		result.Declined = true
		result.Message = fmt.Sprintf("PR #%d was declined without merging", prID)
		return result, nil
	}

	// Still open — surface the commit build status if available.
	if pr.Source != nil && pr.Source.Commit != nil && pr.Source.Commit.Hash != "" {
		state := m.aggregateBuildState(ctx, pr.Source.Commit.Hash)
		switch state {
		case BuildFailed, BuildStopped:
			result.Message = fmt.Sprintf("PR #%d build failed", prID)
		case BuildInProgress:
			result.Message = fmt.Sprintf("PR #%d build in progress", prID)
		case BuildSuccessful:
			result.Message = fmt.Sprintf("PR #%d ready for merge", prID)
		default:
			result.Message = fmt.Sprintf("PR #%d is open, waiting for merge...", prID)
		}
		return result, nil
	}

	result.Message = fmt.Sprintf("PR #%d is open, waiting for merge...", prID)
	return result, nil
}

// aggregateBuildState reduces a commit's build statuses to a single state.
// FAILED/STOPPED dominate, then INPROGRESS, then SUCCESSFUL.
func (m *MergeWaiter) aggregateBuildState(ctx context.Context, sha string) string {
	statuses, err := m.client.GetCommitStatuses(ctx, sha)
	if err != nil {
		m.logger.Debug("Failed to fetch commit statuses",
			slog.String("sha", sha),
			slog.Any("error", err),
		)
		return ""
	}
	if len(statuses) == 0 {
		return ""
	}

	state := BuildSuccessful
	for _, s := range statuses {
		switch s.State {
		case BuildFailed, BuildStopped:
			return BuildFailed
		case BuildInProgress:
			state = BuildInProgress
		}
	}
	return state
}

// WaitWithCallback is like WaitForMerge but calls the callback on each poll.
func (m *MergeWaiter) WaitWithCallback(ctx context.Context, prID int, onPoll func(result *MergeWaitResult)) (*MergeWaitResult, error) {
	m.logger.Info("Waiting for PR merge with callback", slog.Int("pr_id", prID))

	deadline := time.Now().Add(m.config.Timeout)
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		result, err := m.checkPRStatus(ctx, prID)
		if err != nil {
			return nil, fmt.Errorf("failed to check PR status: %w", err)
		}

		if onPoll != nil {
			onPoll(result)
		}

		if result.Merged || result.Declined {
			return result, nil
		}

		select {
		case <-ctx.Done():
			return &MergeWaitResult{
				PRNumber: prID,
				Message:  "Context cancelled",
			}, ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			return &MergeWaitResult{
				PRNumber: prID,
				TimedOut: true,
				Message:  fmt.Sprintf("Timed out after %s", m.config.Timeout),
			}, ErrMergeTimeout
		}

		select {
		case <-ctx.Done():
			return &MergeWaitResult{
				PRNumber: prID,
				Message:  "Context cancelled",
			}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// WaitForBuild waits for the PR's head commit build to complete.
func (m *MergeWaiter) WaitForBuild(ctx context.Context, prID int) (string, error) {
	m.logger.Info("Waiting for build", slog.Int("pr_id", prID))

	deadline := time.Now().Add(m.config.Timeout)
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		pr, err := m.client.GetPullRequest(ctx, prID)
		if err != nil {
			return "", fmt.Errorf("failed to get PR: %w", err)
		}

		if pr.Source != nil && pr.Source.Commit != nil && pr.Source.Commit.Hash != "" {
			switch m.aggregateBuildState(ctx, pr.Source.Commit.Hash) {
			case BuildSuccessful:
				return BuildSuccessful, nil
			case BuildFailed:
				return BuildFailed, ErrBuildFailed
			case BuildStopped:
				return BuildStopped, fmt.Errorf("build was stopped")
			}
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			return "", ErrMergeTimeout
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}
