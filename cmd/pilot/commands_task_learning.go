package main

import (
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
)

// wireTaskLearning initializes the learning system for the task command,
// mirroring main.go polling/gateway mode learning init (GH-2146).
//
// The learning store it opens must be closed at the *outer* RunE return, so
// this helper returns a closer that the caller registers with defer once it is
// non-nil. closer is non-nil only when the store opened successfully (matching
// the original `defer learningStore.Close()` placement, which sits inside the
// store-open success branch). A nil closer means learning was disabled or the
// store failed to open (already warned).
func wireTaskLearning(runner *executor.Runner, cfg *config.Config) func() error {
	// GH-2146: Initialize learning system for task command
	// Mirrors main.go polling/gateway mode learning init
	if cfg.Memory == nil || cfg.Memory.Path == "" {
		return nil
	}

	learningStore, lsErr := memory.NewStore(cfg.Memory.Path)
	if lsErr != nil {
		logging.WithComponent("learning").Warn("Failed to open memory store for learning, learning disabled", slog.Any("error", lsErr))
		return nil
	}

	// Wire log store for execution milestone entries (GH-1599)
	runner.SetLogStore(learningStore)

	// Wire knowledge store for experiential memories (GH-1027)
	knowledgeStore := memory.NewKnowledgeStore(learningStore.DB())
	if ksErr := knowledgeStore.InitSchema(); ksErr != nil {
		logging.WithComponent("knowledge").Warn("Failed to initialize knowledge store schema", slog.Any("error", ksErr))
	} else {
		runner.SetKnowledgeStore(knowledgeStore)
	}

	// Initialize learning components if enabled
	if cfg.Memory.Learning == nil || cfg.Memory.Learning.Enabled {
		patternStore, patternErr := memory.NewGlobalPatternStore(cfg.Memory.Path)
		if patternErr != nil {
			logging.WithComponent("learning").Warn("Failed to create pattern store, learning disabled", slog.Any("error", patternErr))
		} else {
			extractor := memory.NewPatternExtractor(patternStore, learningStore)
			learningLoop := memory.NewLearningLoop(learningStore, extractor, nil)
			patternContext := executor.NewPatternContext(learningStore)

			runner.SetLearningLoop(learningLoop)
			runner.SetPatternContext(patternContext)
			runner.SetSelfReviewExtractor(extractor)

			logging.WithComponent("learning").Info("Learning system initialized")

			// GH-1991: Wire outcome tracker for model escalation
			outcomeTracker := memory.NewModelOutcomeTracker(learningStore)
			runner.SetOutcomeTracker(outcomeTracker)
			if runner.HasModelRouter() {
				runner.ModelRouter().SetOutcomeTracker(outcomeTracker)
			}
			logging.WithComponent("learning").Info("Model outcome tracker initialized")

			// GH-2016: Wire knowledge graph into runner
			kg, kgErr := memory.NewKnowledgeGraph(cfg.Memory.Path)
			if kgErr != nil {
				logging.WithComponent("learning").Warn("Failed to create knowledge graph", slog.Any("error", kgErr))
			} else {
				runner.SetKnowledgeGraph(kg)
				logging.WithComponent("learning").Info("Knowledge graph initialized")
			}
		}
	}

	fmt.Println("   Learning:  ✓ initialized")

	return func() error { return learningStore.Close() }
}
