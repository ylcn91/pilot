package executor

import "strings"

// modelPricing returns (inputPrice, outputPrice) in USD per 1M tokens for the given model.
// Pricing source: https://platform.claude.com/docs/en/about-claude/pricing
func modelPricing(model string) (inputPrice, outputPrice float64) {
	// Model pricing in USD per 1M tokens
	const (
		// Sonnet 4.6/4.5/4
		sonnetInputPrice  = 3.00
		sonnetOutputPrice = 15.00
		// Opus 4.6/4.5 (same pricing)
		opusInputPrice  = 5.00
		opusOutputPrice = 25.00
		// Opus 4.1/4.0 (legacy)
		opus41InputPrice  = 15.00
		opus41OutputPrice = 75.00
		// Haiku 4.5
		haikuInputPrice  = 1.00
		haikuOutputPrice = 5.00
	)

	modelLower := strings.ToLower(model)
	switch {
	case strings.Contains(modelLower, "opus-4-1") || strings.Contains(modelLower, "opus-4-0") || model == "claude-opus-4":
		// Legacy Opus 4.1/4.0
		return opus41InputPrice, opus41OutputPrice
	case strings.Contains(modelLower, "opus"):
		// Opus 4.6/4.5 ($5/$25)
		return opusInputPrice, opusOutputPrice
	case strings.Contains(modelLower, "haiku"):
		return haikuInputPrice, haikuOutputPrice
	case strings.Contains(modelLower, "qwen"):
		// Qwen3-Coder pricing (per 1M tokens)
		switch {
		case strings.Contains(modelLower, "480b") || strings.Contains(modelLower, "plus"):
			return 1.00, 5.00 // Qwen3-Coder-Plus (International, 0-32K)
		case strings.Contains(modelLower, "flash"):
			return 0.30, 1.50
		default:
			return 0.07, 0.30 // Qwen3-Coder-Next (default)
		}
	default:
		return sonnetInputPrice, sonnetOutputPrice
	}
}

// estimateCost calculates estimated cost from token usage (TASK-13).
// Backward-compatible wrapper — treats all input tokens at full price.
func estimateCost(inputTokens, outputTokens int64, model string) float64 {
	return estimateCostWithCache(inputTokens, outputTokens, 0, 0, model)
}

// estimateCostWithCache calculates estimated cost with cache-aware pricing (GH-2164).
// Cache creation tokens cost 125% of input price, cache read tokens cost 10%.
// Pricing source: https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching#pricing
func estimateCostWithCache(input, output, cacheCreation, cacheRead int64, model string) float64 {
	inputPrice, outputPrice := modelPricing(model)

	inputCost := float64(input) * inputPrice / 1_000_000
	outputCost := float64(output) * outputPrice / 1_000_000
	cacheCreateCost := float64(cacheCreation) * (inputPrice * 1.25) / 1_000_000
	cacheReadCost := float64(cacheRead) * (inputPrice * 0.10) / 1_000_000
	return inputCost + outputCost + cacheCreateCost + cacheReadCost
}
