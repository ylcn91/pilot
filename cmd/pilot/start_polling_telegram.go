package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/intent"
	"github.com/ylcn91/pilot/internal/logging"
)

func (p *pollingRuntime) setupTelegram() error {
	cfg := p.cfg
	ctx := p.ctx
	runner := p.runner
	store := p.store
	projectPath := p.projectPath
	tgApprovalHandler := p.tgApprovalHandler

	// Initialize Telegram handler if enabled
	var tgHandler *telegram.Handler
	if p.hasTelegram {
		allowedIDs := resolveTelegramAllowedIDs(cfg)

		tgClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
		tgMessenger := telegram.NewMessenger(tgClient, cfg.Adapters.Telegram.PlainTextMode)

		// Build LLM classifier + conversation store for comms.Handler
		var tgLLMClassifier intent.Classifier
		var tgConvStore *intent.ConversationStore
		if cfg.Adapters.Telegram.LLMClassifier != nil && cfg.Adapters.Telegram.LLMClassifier.Enabled {
			apiKey := cfg.Adapters.Telegram.LLMClassifier.APIKey
			if apiKey == "" {
				apiKey = os.Getenv("ANTHROPIC_API_KEY")
			}
			if apiKey != "" {
				client := intent.NewAnthropicClient(apiKey)
				if cfg.Executor != nil {
					if cfg.Executor.DefaultModel != "" {
						client.SetModel(cfg.Executor.DefaultModel)
					}
					if cfg.Executor.APIBaseURL != "" {
						client.SetAPIURL(cfg.Executor.APIBaseURL + "/v1/messages")
					}
				}
				tgLLMClassifier = client
				historySize := 10
				if cfg.Adapters.Telegram.LLMClassifier.HistorySize > 0 {
					historySize = cfg.Adapters.Telegram.LLMClassifier.HistorySize
				}
				historyTTL := 30 * time.Minute
				if cfg.Adapters.Telegram.LLMClassifier.HistoryTTL > 0 {
					historyTTL = cfg.Adapters.Telegram.LLMClassifier.HistoryTTL
				}
				tgConvStore = intent.NewConversationStore(historySize, historyTTL)
			}
		}

		// Build comms.MemberResolver wrapper (GH-634)
		var tgMemberResolver comms.MemberResolver
		if p.teamAdapter != nil {
			tgMemberResolver = &telegram.MemberResolverAdapter{Inner: p.teamAdapter}
		}

		tgCommsHandler := comms.NewHandler(&comms.HandlerConfig{
			Messenger:      tgMessenger,
			Runner:         runner,
			Projects:       config.NewProjectSource(cfg),
			ProjectPath:    projectPath,
			RateLimit:      cfg.Adapters.Telegram.RateLimit,
			LLMClassifier:  tgLLMClassifier,
			ConvStore:      tgConvStore,
			MemberResolver: tgMemberResolver,
			Store:          store,
			TaskIDPrefix:   "TG",
		})

		tgConfig := &telegram.HandlerConfig{
			Client:          tgClient,
			CommsHandler:    tgCommsHandler,
			ProjectPath:     projectPath,
			Projects:        config.NewProjectSource(cfg),
			AllowedIDs:      allowedIDs,
			Transcription:   cfg.Adapters.Telegram.Transcription,
			Store:           store,
			ApprovalHandler: tgApprovalHandler,
		}
		tgHandler = telegram.NewHandler(tgConfig, runner)

		// Security warning if no allowed IDs configured
		if len(allowedIDs) == 0 {
			logging.WithComponent("telegram").Warn("SECURITY: allowed_ids is empty - ALL users can interact with the bot!")
		}

		// Check for existing instance
		if err := tgHandler.CheckSingleton(ctx); err != nil {
			if errors.Is(err, telegram.ErrConflict) {
				if p.replace {
					fmt.Println("🔄 Stopping existing bot instance...")
					if err := killExistingTelegramBot(); err != nil {
						return fmt.Errorf("failed to stop existing instance: %w", err)
					}
					fmt.Print("   Waiting for Telegram to release connection")
					maxRetries := 10
					var lastErr error
					for i := 0; i < maxRetries; i++ {
						delay := time.Duration(500+i*500) * time.Millisecond
						time.Sleep(delay)
						fmt.Print(".")
						if err := tgHandler.CheckSingleton(ctx); err == nil {
							fmt.Println(" ✓")
							fmt.Println("   ✓ Existing instance stopped")
							fmt.Println()
							lastErr = nil
							break
						} else {
							lastErr = err
						}
					}
					if lastErr != nil {
						fmt.Println(" ✗")
						return fmt.Errorf("timeout waiting for Telegram to release connection")
					}
				} else {
					fmt.Println()
					fmt.Println("❌ Another bot instance is already running")
					fmt.Println()
					fmt.Println("   Options:")
					fmt.Println("   • Kill it manually:  pkill -f 'pilot start'")
					fmt.Println("   • Auto-replace:      pilot start --replace")
					fmt.Println()
					return fmt.Errorf("conflict: another bot instance is running")
				}
			} else {
				return fmt.Errorf("singleton check failed: %w", err)
			}
		}
	}

	p.tgHandler = tgHandler
	return nil
}
