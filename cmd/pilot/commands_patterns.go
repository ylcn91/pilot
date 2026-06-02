package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/memory"
)

func newPatternsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "patterns",
		Short: "Manage cross-project patterns",
		Long:  `View, search, and manage learned patterns across projects.`,
	}

	cmd.AddCommand(
		newPatternsListCmd(),
		newPatternsSearchCmd(),
		newPatternsStatsCmd(),
		newPatternsApplyCmd(),
		newPatternsIgnoreCmd(),
	)

	return cmd
}

func newPatternsListCmd() *cobra.Command {
	var (
		limit       int
		minConf     float64
		patternType string
		showAnti    bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List learned patterns",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Open store
			store, err := memory.NewStore(cfg.Memory.Path)
			if err != nil {
				return fmt.Errorf("failed to open memory store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Query patterns
			ctx := context.Background()
			queryService := memory.NewPatternQueryService(store)

			query := &memory.PatternQuery{
				MaxResults:    limit,
				MinConfidence: minConf,
				IncludeAnti:   showAnti,
			}

			if patternType != "" {
				query.Types = []string{patternType}
			}

			result, err := queryService.Query(ctx, query)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(result.Patterns) == 0 {
				fmt.Println("No patterns found.")
				return nil
			}

			fmt.Printf("Found %d patterns (showing %d):\n\n", result.TotalMatches, len(result.Patterns))

			for _, p := range result.Patterns {
				icon := "📘"
				if p.IsAntiPattern {
					icon = "⚠️"
				}
				fmt.Printf("%s %s (%.0f%% confidence)\n", icon, p.Title, p.Confidence*100)
				fmt.Printf("   Type: %s | Uses: %d | Scope: %s\n", p.Type, p.Occurrences, p.Scope)
				if p.Description != "" {
					desc := p.Description
					if len(desc) > 80 {
						desc = desc[:77] + "..."
					}
					fmt.Printf("   %s\n", desc)
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum patterns to show")
	cmd.Flags().Float64Var(&minConf, "min-confidence", 0.5, "Minimum confidence threshold")
	cmd.Flags().StringVar(&patternType, "type", "", "Filter by type (code, structure, workflow, error, naming)")
	cmd.Flags().BoolVar(&showAnti, "anti", false, "Include anti-patterns")

	return cmd
}

func newPatternsSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search patterns by keyword",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Open store
			store, err := memory.NewStore(cfg.Memory.Path)
			if err != nil {
				return fmt.Errorf("failed to open memory store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Search patterns
			patterns, err := store.SearchCrossPatterns(query, 20)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			if len(patterns) == 0 {
				fmt.Printf("No patterns found matching '%s'\n", query)
				return nil
			}

			fmt.Printf("Found %d patterns matching '%s':\n\n", len(patterns), query)

			for _, p := range patterns {
				icon := "📘"
				if p.IsAntiPattern {
					icon = "⚠️"
				}
				fmt.Printf("%s %s (%.0f%%)\n", icon, p.Title, p.Confidence*100)
				if p.Context != "" {
					fmt.Printf("   Context: %s\n", p.Context)
				}
				fmt.Printf("   %s\n\n", p.Description)
			}

			return nil
		},
	}

	return cmd
}

func newPatternsStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show pattern statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Open store
			store, err := memory.NewStore(cfg.Memory.Path)
			if err != nil {
				return fmt.Errorf("failed to open memory store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Get stats
			stats, err := store.GetCrossPatternStats()
			if err != nil {
				return fmt.Errorf("failed to get stats: %w", err)
			}

			fmt.Println("📊 Cross-Project Pattern Statistics")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("Total Patterns:     %d\n", stats.TotalPatterns)
			fmt.Printf("  ├─ Patterns:      %d\n", stats.Patterns)
			fmt.Printf("  └─ Anti-Patterns: %d\n", stats.AntiPatterns)
			fmt.Printf("Avg Confidence:     %.1f%%\n", stats.AvgConfidence*100)
			fmt.Printf("Total Occurrences:  %d\n", stats.TotalOccurrences)
			fmt.Printf("Projects Using:     %d\n", stats.ProjectCount)
			fmt.Println()

			if len(stats.ByType) > 0 {
				fmt.Println("By Type:")
				for pType, count := range stats.ByType {
					fmt.Printf("  %s: %d\n", pType, count)
				}
			}

			return nil
		},
	}

	return cmd
}

func newPatternsApplyCmd() *cobra.Command {
	var projectPath string

	cmd := &cobra.Command{
		Use:   "apply <pattern-id>",
		Short: "Apply a pattern to a project",
		Long:  `Link a pattern to a project so it will be considered during task execution.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			patternID := args[0]

			// Resolve project path
			if projectPath == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get current directory: %w", err)
				}
				projectPath = cwd
			}

			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Open store
			store, err := memory.NewStore(cfg.Memory.Path)
			if err != nil {
				return fmt.Errorf("failed to open memory store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Verify pattern exists
			pattern, err := store.GetCrossPattern(patternID)
			if err != nil {
				return fmt.Errorf("pattern not found: %w", err)
			}

			// Link pattern to project
			if err := store.LinkPatternToProject(patternID, projectPath); err != nil {
				return fmt.Errorf("failed to apply pattern: %w", err)
			}

			fmt.Printf("✅ Applied pattern to project:\n")
			fmt.Printf("   Pattern: %s\n", pattern.Title)
			fmt.Printf("   Type:    %s\n", pattern.Type)
			fmt.Printf("   Project: %s\n", shortenPath(projectPath))

			return nil
		},
	}

	cmd.Flags().StringVarP(&projectPath, "project", "p", "", "Project path (default: current directory)")

	return cmd
}

func newPatternsIgnoreCmd() *cobra.Command {
	var (
		projectPath string
		global      bool
	)

	cmd := &cobra.Command{
		Use:   "ignore <pattern-id>",
		Short: "Ignore a pattern",
		Long: `Mark a pattern as ignored. By default, ignores for the current project only.
Use --global to ignore across all projects (deletes the pattern).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			patternID := args[0]

			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Open store
			store, err := memory.NewStore(cfg.Memory.Path)
			if err != nil {
				return fmt.Errorf("failed to open memory store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Verify pattern exists
			pattern, err := store.GetCrossPattern(patternID)
			if err != nil {
				return fmt.Errorf("pattern not found: %w", err)
			}

			if global {
				// Delete the pattern entirely
				if err := store.DeleteCrossPattern(patternID); err != nil {
					return fmt.Errorf("failed to delete pattern: %w", err)
				}
				fmt.Printf("✅ Deleted pattern globally:\n")
				fmt.Printf("   Pattern: %s\n", pattern.Title)
				fmt.Printf("   Type:    %s\n", pattern.Type)
			} else {
				// Record negative feedback for this project
				if projectPath == "" {
					cwd, err := os.Getwd()
					if err != nil {
						return fmt.Errorf("failed to get current directory: %w", err)
					}
					projectPath = cwd
				}

				feedback := &memory.PatternFeedback{
					PatternID:       patternID,
					ProjectPath:     projectPath,
					Outcome:         "ignored",
					ConfidenceDelta: -0.1, // Reduce confidence for ignored patterns
				}
				if err := store.RecordPatternFeedback(feedback); err != nil {
					return fmt.Errorf("failed to record ignore: %w", err)
				}
				fmt.Printf("✅ Ignored pattern for project:\n")
				fmt.Printf("   Pattern: %s\n", pattern.Title)
				fmt.Printf("   Project: %s\n", shortenPath(projectPath))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&projectPath, "project", "p", "", "Project path (default: current directory)")
	cmd.Flags().BoolVar(&global, "global", false, "Ignore globally (deletes the pattern)")

	return cmd
}
