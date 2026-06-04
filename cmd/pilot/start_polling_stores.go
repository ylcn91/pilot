package main

import (
	"log/slog"
	"path/filepath"
	"time"

	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/teams"
)

// setupStores opens the memory store and wires all store-dependent subsystems.
// On success it returns a cleanup closure that closes the store (the caller is
// responsible for deferring it); it returns nil when no cleanup is needed.
func (p *pollingRuntime) setupStores() func() {
	cfg := p.cfg
	ctx := p.ctx
	runner := p.runner
	projectPath := p.projectPath
	tgApprovalHandlerImpl := p.tgApprovalHandlerImpl
	autopilotControllers := p.autopilotControllers
	memoryPath := startMemoryPath(cfg)

	// Initialize memory store early for dashboard persistence (GH-367)
	store, err := memory.NewStore(memoryPath)
	var closeStore func()
	if err != nil {
		logging.WithComponent("start").Warn("Failed to open memory store", slog.Any("error", err))
		store = nil
	} else {
		closeStore = func() {
			if store != nil {
				_ = store.Close()
			}
		}
	}
	p.store = store

	// Attach persistence store and rehydrate pending approvals after restart.
	if store != nil && tgApprovalHandlerImpl != nil {
		tgApprovalHandlerImpl.WithStore(store)
		if rErr := tgApprovalHandlerImpl.Rehydrate(ctx); rErr != nil {
			logging.WithComponent("approval").Warn("telegram approval rehydrate failed", slog.Any("error", rErr))
		}
	}

	// GH-726: Initialize autopilot state store for crash recovery
	var autopilotStateStore *autopilot.StateStore
	if store != nil && len(autopilotControllers) > 0 {
		// GH-2712: Wire memory store for approval_request_id / approval_decision persistence.
		for _, controller := range autopilotControllers {
			controller.SetMemoryStore(store)
		}

		var storeErr error
		autopilotStateStore, storeErr = autopilot.NewStateStore(store.DB())
		if storeErr != nil {
			logging.WithComponent("autopilot").Warn("Failed to initialize state store", slog.Any("error", storeErr))
		} else {
			// GH-929: Wire state store to all controllers
			for repoName, controller := range autopilotControllers {
				controller.SetStateStore(autopilotStateStore)
				restored, restoreErr := controller.RestoreState()
				if restoreErr != nil {
					logging.WithComponent("autopilot").Warn("Failed to restore state from SQLite",
						slog.String("repo", repoName),
						slog.Any("error", restoreErr))
				} else if restored > 0 {
					logging.WithComponent("autopilot").Info("Restored autopilot PR states from SQLite",
						slog.String("repo", repoName),
						slog.Int("count", restored))
				}
			}
		}
	}
	p.autopilotStateStore = autopilotStateStore

	// GH-634: Initialize teams service for RBAC enforcement
	if store != nil {
		teamStore, teamErr := teams.NewStore(store.DB())
		if teamErr != nil {
			logging.WithComponent("teams").Warn("Failed to initialize team store", slog.Any("error", teamErr))
		} else {
			teamSvc := teams.NewService(teamStore)
			p.teamAdapter = teams.NewServiceAdapter(teamSvc)
			runner.SetTeamChecker(p.teamAdapter)
			logging.WithComponent("teams").Info("team RBAC enforcement enabled for polling mode")
		}
	}

	// GH-1027: Initialize knowledge store for experiential memories.
	// Hoisted so the pattern-maintenance ticker below can call SyncToFiles.
	var knowledgeStore *memory.KnowledgeStore
	if store != nil {
		knowledgeStore = memory.NewKnowledgeStore(store.DB())
		if err := knowledgeStore.InitSchema(); err != nil {
			logging.WithComponent("knowledge").Warn("Failed to initialize knowledge store schema", slog.Any("error", err))
			knowledgeStore = nil
		} else {
			runner.SetKnowledgeStore(knowledgeStore)
			logging.WithComponent("knowledge").Debug("Knowledge store initialized for polling mode")
		}
	}
	p.knowledgeStore = knowledgeStore

	// GH-1599: Wire log store for execution milestone entries
	if store != nil {
		runner.SetLogStore(store)
	}

	// GH-1814: Initialize learning system
	if store != nil && (cfg.Memory == nil || cfg.Memory.Learning == nil || cfg.Memory.Learning.Enabled) {
		patternStore, patternErr := memory.NewGlobalPatternStore(memoryPath)
		if patternErr != nil {
			logging.WithComponent("learning").Warn("Failed to create pattern store, learning disabled", slog.Any("error", patternErr))
		} else {
			extractor := memory.NewPatternExtractor(patternStore, store)
			learningLoop := memory.NewLearningLoop(store, extractor, nil)
			patternContext := executor.NewPatternContext(store)

			runner.SetLearningLoop(learningLoop)
			runner.SetPatternContext(patternContext)
			runner.SetSelfReviewExtractor(extractor)

			// GH-1823: Wire review learning into autopilot controllers
			for _, ctrl := range autopilotControllers {
				ctrl.SetLearningLoop(learningLoop)
			}

			logging.WithComponent("learning").Info("Learning system initialized")

			// GH-1991: Wire outcome tracker for model escalation
			outcomeTracker := memory.NewModelOutcomeTracker(store)
			runner.SetOutcomeTracker(outcomeTracker)
			if runner.HasModelRouter() {
				runner.ModelRouter().SetOutcomeTracker(outcomeTracker)
			}
			logging.WithComponent("learning").Info("Model outcome tracker initialized")

			// GH-2016: Wire knowledge graph into runner
			kg, kgErr := memory.NewKnowledgeGraph(memoryPath)
			if kgErr != nil {
				logging.WithComponent("learning").Warn("Failed to create knowledge graph", slog.Any("error", kgErr))
			} else {
				runner.SetKnowledgeGraph(kg)
				logging.WithComponent("learning").Info("Knowledge graph initialized")
			}

			// Pattern maintenance — decay and cleanup every 24h
			logging.SafeGo("learning.maintenance", func() {
				ticker := time.NewTicker(24 * time.Hour)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if n, decayErr := learningLoop.ApplyDecay(ctx); decayErr != nil {
							logging.WithComponent("learning").Warn("Pattern decay failed", slog.Any("error", decayErr))
						} else if n > 0 {
							logging.WithComponent("learning").Info("Applied pattern decay", slog.Int("patterns_decayed", n))
						}
						minConfidence := 0.1
						if cfg.Memory != nil && cfg.Memory.Learning != nil && cfg.Memory.Learning.MinConfidence > 0 {
							minConfidence = cfg.Memory.Learning.MinConfidence
						}
						if n, depErr := learningLoop.DeprecateLowConfidencePatterns(ctx, minConfidence); depErr != nil {
							logging.WithComponent("learning").Warn("Pattern deprecation failed", slog.Any("error", depErr))
						} else if n > 0 {
							logging.WithComponent("learning").Info("Deprecated low-confidence patterns", slog.Int("deprecated", n))
						}

						// Opt-in (default off): SyncToFiles writes hash-named files under
						// .agent/knowledge/memories/{type}s/ — the same tree Navigator
						// manages with slug-named files, so enabling is explicit to avoid
						// surprising that layout.
						if knowledgeStore != nil && cfg.Memory != nil && cfg.Memory.SyncToFiles {
							agentPath := filepath.Join(projectPath, ".agent")
							if syncErr := knowledgeStore.SyncToFiles(agentPath); syncErr != nil {
								logging.WithComponent("knowledge").Warn("Knowledge sync to files failed", slog.Any("error", syncErr))
							} else {
								logging.WithComponent("knowledge").Info("Synced knowledge memories to files", slog.String("agent_path", agentPath))
							}
						}
					}
				}
			})
		}
	}

	return closeStore
}
