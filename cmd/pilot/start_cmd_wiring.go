package main

import (
	"context"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/dashboard"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/pilot"
)

// wireGatewayPilot connects the gateway-mode shared infrastructure (gw) onto
// the constructed Pilot instance p: the autopilot provider/metrics source, PR
// review events, dashboard/log stores, git graph fetcher, and the learning
// system. It is a behavior-preserving extraction of the inline wiring that
// followed pilot.New in newStartCmd (GH-1585, GH-2080, GH-1609, GH-1633,
// GH-1935). All conditionals and ordering are unchanged.
func wireGatewayPilot(p *pilot.Pilot, gw *gatewayInfra, cfg *config.Config, projectPath string) {
	// GH-1585: Wire autopilot provider to gateway so /api/v1/autopilot returns live PR data
	if gw.AutopilotController != nil {
		p.Gateway().SetAutopilotProvider(&autopilotProviderAdapter{controller: gw.AutopilotController})
		p.Gateway().SetMetricsSource(gw.AutopilotController.Metrics())
		// GH-2855: wire token/cost/execution counters into executor
		if gw.Runner != nil {
			gw.Runner.SetMetricsRecorder(gw.AutopilotController.Metrics())
		}
	}
	// TASK-332: Wire alert metrics into the Prometheus exporter
	if gw.AlertsEngine != nil {
		p.Gateway().SetAlertsMetricsSource(gw.AlertsEngine)
	}
	if gw.AutopilotController != nil {

		// GH-2080: Wire PR review events to autopilot controller
		p.SetOnPRReview(func(ctx context.Context, prNumber int, action, state, reviewer string, repo *github.Repository) error {
			if action == "submitted" {
				gw.AutopilotController.OnReviewRequested(prNumber, action, state, reviewer)
			}
			return nil
		})
	}

	// GH-1609: Wire dashboard store to gateway so /api/v1/{metrics,queue,history,logs} return 200
	if gw.Store != nil {
		p.Gateway().SetDashboardStore(gw.Store)
		p.Gateway().SetLogStreamStore(gw.Store)
	}

	// GH-1633: Wire git graph fetcher to gateway so /api/v1/gitgraph returns live git data
	p.Gateway().SetGitGraphFetcher(func(path string, limit int) interface{} {
		return dashboard.FetchGitGraph(path, limit)
	})
	p.Gateway().SetGitGraphPath(projectPath)

	// GH-1935: Wire learning system into gateway mode (mirrors polling-mode wiring)
	if gw.Store != nil && (cfg.Memory.Learning == nil || cfg.Memory.Learning.Enabled) {
		gwPatternStore, gwPatternErr := memory.NewGlobalPatternStore(cfg.Memory.Path)
		if gwPatternErr != nil {
			logging.WithComponent("learning").Warn("Failed to create pattern store, learning disabled (gateway mode)", slog.Any("error", gwPatternErr))
		} else {
			gwExtractor := memory.NewPatternExtractor(gwPatternStore, gw.Store)
			gwLearningLoop := memory.NewLearningLoop(gw.Store, gwExtractor, nil)
			gwPatternContext := executor.NewPatternContext(gw.Store)

			gw.Runner.SetLearningLoop(gwLearningLoop)
			gw.Runner.SetPatternContext(gwPatternContext)
			gw.Runner.SetSelfReviewExtractor(gwExtractor)

			if gw.AutopilotController != nil {
				gw.AutopilotController.SetLearningLoop(gwLearningLoop)
			}

			// GH-1991: Wire outcome tracker for model escalation (gateway mode)
			gwOutcomeTracker := memory.NewModelOutcomeTracker(gw.Store)
			gw.Runner.SetOutcomeTracker(gwOutcomeTracker)
			if gw.Runner.HasModelRouter() {
				gw.Runner.ModelRouter().SetOutcomeTracker(gwOutcomeTracker)
			}

			// GH-2016: Wire knowledge graph into gateway runner
			gwKG, gwKGErr := memory.NewKnowledgeGraph(cfg.Memory.Path)
			if gwKGErr != nil {
				logging.WithComponent("learning").Warn("Failed to create knowledge graph (gateway mode)", slog.Any("error", gwKGErr))
			} else {
				gw.Runner.SetKnowledgeGraph(gwKG)
				logging.WithComponent("learning").Info("Knowledge graph initialized (gateway mode)")
			}

			logging.WithComponent("learning").Info("Learning system initialized (gateway mode)")
		}
	}
}
