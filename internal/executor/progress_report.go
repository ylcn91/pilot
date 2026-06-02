package executor

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ExecutionReport contains all data for the end-of-execution report
type ExecutionReport struct {
	TaskID           string
	TaskTitle        string
	Success          bool
	Duration         time.Duration
	Branch           string
	CommitSHA        string
	PRUrl            string
	HasNavigator     bool
	NavMode          string
	TokensInput      int64
	TokensOutput     int64
	EstimatedCostUSD float64
	FilesChanged     []string
	ModelName        string
	ErrorMessage     string
	// QualityGates contains results from quality gate checks (GH-209)
	QualityGates *QualityGatesResult
}

// Finish prints final status (simple version for backward compatibility)
func (p *ProgressDisplay) Finish(success bool, result string) {
	if !p.enabled {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Clear progress display
	linesToClear := p.maxLogs + 2
	if p.hasNavigator {
		linesToClear++
	}
	for i := 0; i < linesToClear; i++ {
		fmt.Print("\033[A\033[K")
	}

	// Print final summary
	duration := time.Since(p.startTime).Round(time.Second)
	if success {
		fmt.Printf("   %s %s in %s\n",
			phaseStyle.Render("✓ Completed"),
			p.taskID,
			duration.String(),
		)
	} else {
		fmt.Printf("   %s %s in %s\n",
			errorStyle.Render("✗ Failed"),
			p.taskID,
			duration.String(),
		)
	}

	// Print truncated result
	if result != "" {
		lines := strings.Split(result, "\n")
		maxLines := 10
		if len(lines) > maxLines {
			lines = lines[:maxLines]
			lines = append(lines, "...")
		}
		fmt.Println()
		for _, line := range lines {
			if line != "" {
				fmt.Printf("   %s\n", line)
			}
		}
	}
}

// FinishWithReport prints a comprehensive execution report
func (p *ProgressDisplay) FinishWithReport(report *ExecutionReport) {
	if !p.enabled {
		return
	}

	p.mu.Lock()

	// Close out the current phase
	if p.currentPhase != nil {
		p.currentPhase.EndTime = time.Now()
		p.phaseHistory = append(p.phaseHistory, *p.currentPhase)
	}

	// Merge tracked files with report files
	allFiles := make(map[string]bool)
	for _, f := range p.filesChanged {
		allFiles[f] = true
	}
	for _, f := range report.FilesChanged {
		allFiles[f] = true
	}

	p.mu.Unlock()

	// Clear progress display
	linesToClear := p.maxLogs + 2
	if p.hasNavigator {
		linesToClear++
	}
	for i := 0; i < linesToClear; i++ {
		fmt.Print("\033[A\033[K")
	}

	// Print structured report
	p.printExecutionReport(report, allFiles)
}

// printExecutionReport outputs the formatted execution report
func (p *ProgressDisplay) printExecutionReport(report *ExecutionReport, allFiles map[string]bool) {
	divider := "───────────────────────────────────────"

	fmt.Println(divider)
	fmt.Printf("%s\n", headerStyle.Render("📊 EXECUTION REPORT"))
	fmt.Println(divider)

	// Task info
	fmt.Printf("Task:       %s\n", valueStyle.Render(report.TaskID))
	if report.TaskTitle != "" && report.TaskTitle != report.TaskID {
		fmt.Printf("Title:      %s\n", dimStyle.Render(truncateForReport(report.TaskTitle, 50)))
	}

	// Status
	if report.Success {
		fmt.Printf("Status:     %s\n", successStyle.Render("✅ Success"))
	} else {
		fmt.Printf("Status:     %s\n", errorStyle.Render("❌ Failed"))
		if report.ErrorMessage != "" {
			fmt.Printf("Error:      %s\n", errorStyle.Render(truncateForReport(report.ErrorMessage, 60)))
		}
	}

	// Duration
	fmt.Printf("Duration:   %s\n", valueStyle.Render(report.Duration.Round(time.Second).String()))

	// Git info
	if report.Branch != "" {
		fmt.Printf("Branch:     %s\n", valueStyle.Render(report.Branch))
	}
	if report.CommitSHA != "" {
		shortSHA := report.CommitSHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		fmt.Printf("Commit:     %s\n", valueStyle.Render(shortSHA))
	}
	if report.PRUrl != "" {
		fmt.Printf("PR:         %s\n", valueStyle.Render(report.PRUrl))
	}

	// Navigator info
	if report.HasNavigator {
		fmt.Println()
		fmt.Printf("🧭 %s\n", navigatorStyle.Render("Navigator: Active"))
		if report.NavMode != "" {
			fmt.Printf("   Mode:    %s\n", valueStyle.Render(report.NavMode))
		}
	}

	// Quality Gates section (GH-209)
	if report.QualityGates != nil && report.QualityGates.Enabled {
		fmt.Println()
		fmt.Printf("%s\n", headerStyle.Render("🔒 Quality Gates:"))
		for _, gate := range report.QualityGates.Gates {
			var icon string
			var statusStyle lipgloss.Style
			if gate.Passed {
				icon = "✅"
				statusStyle = successStyle
			} else {
				icon = "❌"
				statusStyle = errorStyle
			}

			// Format: "  ✅ build     12s"
			durationStr := gate.Duration.Round(time.Second).String()
			fmt.Printf("  %s %-10s %s", icon, statusStyle.Render(gate.Name), dimStyle.Render(durationStr))

			// Add retry count if any
			if gate.RetryCount > 0 {
				fmt.Printf(" (%d retry)", gate.RetryCount)
			}
			fmt.Println()

			// Show error snippet for failed gates
			if !gate.Passed && gate.Error != "" {
				errSnippet := truncateForReport(gate.Error, 50)
				fmt.Printf("     %s\n", errorStyle.Render(errSnippet))
			}
		}

		// Show total retries if any
		if report.QualityGates.TotalRetries > 0 {
			fmt.Printf("  Retries:    %s\n", valueStyle.Render(fmt.Sprintf("%d", report.QualityGates.TotalRetries)))
		}
	}

	// Phase timing breakdown
	if len(p.phaseHistory) > 0 {
		fmt.Println()
		fmt.Printf("%s\n", headerStyle.Render("📈 Phases:"))
		totalDuration := report.Duration
		for _, ph := range p.phaseHistory {
			phaseDuration := ph.EndTime.Sub(ph.StartTime)
			if phaseDuration < 0 {
				continue
			}
			pct := 0
			if totalDuration > 0 {
				pct = int(float64(phaseDuration) / float64(totalDuration) * 100)
			}
			fmt.Printf("  %-12s %6s   (%2d%%)\n",
				ph.Phase,
				phaseDuration.Round(time.Second).String(),
				pct)
		}
	}

	// Files changed
	if len(allFiles) > 0 {
		fmt.Println()
		fmt.Printf("%s\n", headerStyle.Render("📁 Files Changed:"))
		count := 0
		for f := range allFiles {
			if count >= 10 {
				fmt.Printf("  ... and %d more\n", len(allFiles)-10)
				break
			}
			// Determine prefix (M for modified, A for added - simplified)
			prefix := "M"
			fmt.Printf("  %s %s\n", dimStyle.Render(prefix), filepath.Base(f))
			count++
		}
	}

	// Token usage and cost
	if report.TokensInput > 0 || report.TokensOutput > 0 {
		fmt.Println()
		fmt.Printf("%s\n", headerStyle.Render("💰 Tokens:"))
		fmt.Printf("  Input:    %s\n", valueStyle.Render(formatTokenCount(report.TokensInput)))
		fmt.Printf("  Output:   %s\n", valueStyle.Render(formatTokenCount(report.TokensOutput)))
		if report.EstimatedCostUSD > 0 {
			fmt.Printf("  Cost:     %s\n", costStyle.Render(fmt.Sprintf("~$%.2f", report.EstimatedCostUSD)))
		}
		if report.ModelName != "" {
			fmt.Printf("  Model:    %s\n", dimStyle.Render(report.ModelName))
		}
	}

	fmt.Println(divider)
}

// formatTokenCount formats a token count with thousands separators
func formatTokenCount(count int64) string {
	if count < 1000 {
		return fmt.Sprintf("%d", count)
	}
	if count < 1000000 {
		return fmt.Sprintf("%dk", count/1000)
	}
	return fmt.Sprintf("%.1fM", float64(count)/1000000)
}

// truncateForReport truncates text for report display
func truncateForReport(text string, maxLen int) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.TrimSpace(text)
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}
