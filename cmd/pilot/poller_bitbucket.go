package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/bitbucket"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

func bitbucketPollerRegistration() PollerRegistration {
	return PollerRegistration{
		Name: "bitbucket",
		Enabled: func(cfg *config.Config) bool {
			return cfg.Adapters.Bitbucket != nil && cfg.Adapters.Bitbucket.Enabled &&
				cfg.Adapters.Bitbucket.Polling != nil && cfg.Adapters.Bitbucket.Polling.Enabled
		},
		CreateAndStart: func(ctx context.Context, deps *PollerDeps) {
			// Determine interval
			interval := 30 * time.Second
			if deps.Cfg.Adapters.Bitbucket.Polling.Interval > 0 {
				interval = deps.Cfg.Adapters.Bitbucket.Polling.Interval
			}

			bitbucketClient := bitbucket.NewClientWithConfig(deps.Cfg.Adapters.Bitbucket)

			label := deps.Cfg.Adapters.Bitbucket.PilotLabel
			if label == "" {
				label = "pilot"
			}

			bitbucketPollerOpts := []bitbucket.PollerOption{
				bitbucket.WithOnIssueWithResult(func(issueCtx context.Context, issue *bitbucket.Issue) (*bitbucket.IssueResult, error) {
					result, err := handleBitbucketIssueWithResult(issueCtx, deps.Cfg, bitbucketClient, issue, deps.ProjectPath, deps.Dispatcher, deps.Runner, deps.Monitor, deps.Program, deps.AlertsEngine, deps.Enforcer)

					// Wire PR to autopilot for CI monitoring + auto-merge
					if result != nil && result.PRNumber > 0 && deps.AutopilotController != nil {
						deps.AutopilotController.OnPRCreated(result.PRNumber, result.PRURL, 0, result.HeadSHA, result.BranchName, "")
					}

					return result, err
				}),
			}

			if deps.AutopilotStateStore != nil {
				bitbucketPollerOpts = append(bitbucketPollerOpts, bitbucket.WithProcessedStore(deps.AutopilotStateStore))
			}

			// Wire OnPRCreated for autopilot controller
			if deps.AutopilotController != nil {
				ctrl := deps.AutopilotController
				bitbucketPollerOpts = append(bitbucketPollerOpts, bitbucket.WithOnPRCreated(func(prID int, prURL string, issueID int, headSHA string, branchName string) {
					ctrl.OnPRCreated(prID, prURL, issueID, headSHA, branchName, "")
				}))
			}

			if deps.Cfg.Orchestrator.MaxConcurrent > 0 {
				bitbucketPollerOpts = append(bitbucketPollerOpts, bitbucket.WithMaxConcurrent(deps.Cfg.Orchestrator.MaxConcurrent))
			}

			bitbucketPoller := bitbucket.NewPoller(bitbucketClient, label, interval, bitbucketPollerOpts...)

			logging.WithComponent("start").Info("Bitbucket polling enabled",
				slog.String("workspace", deps.Cfg.Adapters.Bitbucket.Workspace),
				slog.String("repo", deps.Cfg.Adapters.Bitbucket.Repo),
				slog.String("label", label),
				slog.Duration("interval", interval),
			)
			go func(p *bitbucket.Poller) {
				defer logging.Recover("bitbucket.poller")
				p.Start(ctx)
			}(bitbucketPoller)
		},
	}
}
