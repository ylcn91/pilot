package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// saveMetadata saves the recording metadata to JSON
func (r *Recorder) saveMetadata() error {
	metadataPath := filepath.Join(r.basePath, r.id, "metadata.json")
	data, err := json.MarshalIndent(r.recording, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath, data, 0644)
}

// saveDiffs saves file diffs
func (r *Recorder) saveDiffs() error {
	if len(r.diffFiles) == 0 {
		return nil
	}

	diffsPath := filepath.Join(r.recording.DiffsPath, "changes.json")
	diffs := make([]*FileDiff, 0, len(r.diffFiles))
	for _, d := range r.diffFiles {
		diffs = append(diffs, d)
	}

	data, err := json.MarshalIndent(diffs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(diffsPath, data, 0644)
}

// generateSummary creates a human-readable summary
func (r *Recorder) generateSummary() error {
	var sb strings.Builder

	sb.WriteString("# Execution Recording Summary\n\n")
	sb.WriteString(fmt.Sprintf("**Recording ID**: %s\n", r.recording.ID))
	sb.WriteString(fmt.Sprintf("**Task ID**: %s\n", r.recording.TaskID))
	sb.WriteString(fmt.Sprintf("**Status**: %s\n", r.recording.Status))
	sb.WriteString(fmt.Sprintf("**Duration**: %s\n", r.recording.Duration.Round(time.Second)))
	sb.WriteString(fmt.Sprintf("**Events**: %d\n", r.recording.EventCount))
	sb.WriteString("\n")

	// Metadata
	if r.recording.Metadata != nil {
		sb.WriteString("## Metadata\n\n")
		if r.recording.Metadata.Branch != "" {
			sb.WriteString(fmt.Sprintf("- **Branch**: %s\n", r.recording.Metadata.Branch))
		}
		if r.recording.Metadata.CommitSHA != "" {
			sb.WriteString(fmt.Sprintf("- **Commit**: %s\n", r.recording.Metadata.CommitSHA))
		}
		if r.recording.Metadata.PRUrl != "" {
			sb.WriteString(fmt.Sprintf("- **PR**: %s\n", r.recording.Metadata.PRUrl))
		}
		if r.recording.Metadata.ModelName != "" {
			sb.WriteString(fmt.Sprintf("- **Model**: %s\n", r.recording.Metadata.ModelName))
		}
		sb.WriteString(fmt.Sprintf("- **Navigator**: %v\n", r.recording.Metadata.HasNavigator))
		sb.WriteString("\n")
	}

	// Token usage
	if r.recording.TokenUsage != nil {
		sb.WriteString("## Token Usage\n\n")
		sb.WriteString(fmt.Sprintf("- **Input**: %d tokens\n", r.recording.TokenUsage.InputTokens))
		sb.WriteString(fmt.Sprintf("- **Output**: %d tokens\n", r.recording.TokenUsage.OutputTokens))
		sb.WriteString(fmt.Sprintf("- **Total**: %d tokens\n", r.recording.TokenUsage.TotalTokens))
		sb.WriteString(fmt.Sprintf("- **Estimated Cost**: $%.4f\n", r.recording.TokenUsage.EstimatedCostUSD))
		sb.WriteString("\n")
	}

	// Phase timings
	if len(r.recording.PhaseTimings) > 0 {
		sb.WriteString("## Phase Timings\n\n")
		for _, pt := range r.recording.PhaseTimings {
			pct := float64(pt.Duration) / float64(r.recording.Duration) * 100
			sb.WriteString(fmt.Sprintf("- **%s**: %s (%.1f%%)\n", pt.Phase, pt.Duration.Round(time.Second), pct))
		}
		sb.WriteString("\n")
	}

	// Files changed
	if len(r.diffFiles) > 0 {
		sb.WriteString("## Files Changed\n\n")
		for fp, diff := range r.diffFiles {
			sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", fp, diff.Operation))
		}
		sb.WriteString("\n")
	}

	return os.WriteFile(r.recording.SummaryPath, []byte(sb.String()), 0644)
}

// estimateCost calculates estimated cost from token usage
// Pricing source: https://platform.claude.com/docs/en/about-claude/pricing
func (r *Recorder) estimateCost() float64 {
	// Pricing per 1M tokens
	const (
		// Sonnet 4.5/4
		sonnetInputPrice  = 3.00
		sonnetOutputPrice = 15.00
		// Opus 4.6/4.5
		opusInputPrice  = 5.00
		opusOutputPrice = 25.00
		// Opus 4.1/4.0 (legacy)
		opus41InputPrice  = 15.00
		opus41OutputPrice = 75.00
		// Haiku 4.5
		haikuInputPrice  = 1.00
		haikuOutputPrice = 5.00
	)

	model := r.recording.Metadata.ModelName
	modelLower := strings.ToLower(model)
	var inPrice, outPrice float64

	switch {
	case strings.Contains(modelLower, "opus-4-1") || strings.Contains(modelLower, "opus-4-0") || model == "claude-opus-4":
		inPrice = opus41InputPrice
		outPrice = opus41OutputPrice
	case strings.Contains(modelLower, "opus"):
		inPrice = opusInputPrice
		outPrice = opusOutputPrice
	case strings.Contains(modelLower, "haiku"):
		inPrice = haikuInputPrice
		outPrice = haikuOutputPrice
	default:
		inPrice = sonnetInputPrice
		outPrice = sonnetOutputPrice
	}

	inputCost := float64(r.recording.TokenUsage.InputTokens) * inPrice / 1_000_000
	outputCost := float64(r.recording.TokenUsage.OutputTokens) * outPrice / 1_000_000
	return inputCost + outputCost
}
