package main

import (
	"github.com/ylcn91/pilot/internal/config"
)

// resolveTelegramAllowedIDs builds the list of Telegram user IDs allowed to
// interact with the bot: the explicitly configured allowed IDs plus the
// configured ChatID when it parses as an int64.
func resolveTelegramAllowedIDs(cfg *config.Config) []int64 {
	var allowedIDs []int64
	// Include explicitly configured allowed IDs
	allowedIDs = append(allowedIDs, cfg.Adapters.Telegram.AllowedIDs...)
	// Also include ChatID so user can message their own bot
	if cfg.Adapters.Telegram.ChatID != "" {
		if id, err := parseInt64(cfg.Adapters.Telegram.ChatID); err == nil {
			allowedIDs = append(allowedIDs, id)
		}
	}
	return allowedIDs
}
