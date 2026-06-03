package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// printArchitectResult renders the run outcome as human-readable text or JSON.
func printArchitectResult(result architect.RunResult, f *architectFlags) error {
	if f.jsonOut {
		return printArchitectJSON(result, f.dryRun)
	}
	printArchitectHuman(result, f.dryRun)
	return nil
}

// architectJSONReport is the stable shape emitted by --json.
type architectJSONReport struct {
	DryRun   bool               `json:"dry_run"`
	Created  int                `json:"created"`
	Skipped  int                `json:"skipped"`
	Findings []pilotapi.Finding `json:"findings"`
}

func printArchitectJSON(result architect.RunResult, dryRun bool) error {
	report := architectJSONReport{
		DryRun:   dryRun,
		Created:  result.Created,
		Skipped:  result.Skipped,
		Findings: result.Findings,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printArchitectHuman(result architect.RunResult, dryRun bool) {
	mode := "created"
	if dryRun {
		mode = "would create"
	}
	fmt.Printf("Architect found %d proposal(s).\n", len(result.Findings))
	for i, f := range result.Findings {
		risk := f.Risk
		if risk == "" {
			risk = pilotapi.RiskMedium
		}
		fmt.Printf("\n%d. [%s] %s\n", i+1, risk, f.Title)
		if f.WhyItMatters != "" {
			fmt.Printf("   why: %s\n", f.WhyItMatters)
		}
		if len(f.Files) > 0 {
			fmt.Printf("   files: %s\n", strings.Join(f.Files, ", "))
		}
	}
	fmt.Printf("\n%s %d issue(s), skipped %d (dedup).\n", mode, result.Created, result.Skipped)
}
