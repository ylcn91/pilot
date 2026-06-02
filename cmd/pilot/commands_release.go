package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/config"
)

func newReleaseCmd() *cobra.Command {
	var (
		bump   string // force bump type: patch, minor, major
		draft  bool   // create as draft
		dryRun bool   // show what would be released
	)

	cmd := &cobra.Command{
		Use:   "release [version]",
		Short: "Create a release manually",
		Long: `Create a new release for the current repository.

If no version is specified, detects version bump from commits since last release.

Examples:
  pilot release                  # Auto-detect version from commits
  pilot release --bump=minor     # Force minor bump
  pilot release v1.2.3           # Specific version
  pilot release --draft          # Create as draft
  pilot release --dry-run        # Show what would be released`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Get GitHub token
			ghToken := ""
			if cfg.Adapters.GitHub != nil {
				ghToken = cfg.Adapters.GitHub.Token
			}
			if ghToken == "" {
				ghToken = os.Getenv("GITHUB_TOKEN")
			}
			if ghToken == "" {
				return fmt.Errorf("GitHub not configured - set github.token in config or GITHUB_TOKEN env var")
			}

			// Resolve owner/repo
			owner, repo, err := resolveOwnerRepo(cfg)
			if err != nil {
				return err
			}

			ghClient := github.NewClient(ghToken)

			// Create releaser with default config
			releaseCfg := autopilot.DefaultReleaseConfig()
			releaseCfg.Enabled = true
			releaser := autopilot.NewReleaser(ghClient, owner, repo, releaseCfg)

			// Get current version
			currentVersion, err := releaser.GetCurrentVersion(ctx)
			if err != nil {
				return fmt.Errorf("failed to get current version: %w", err)
			}

			var newVersion autopilot.SemVer
			var bumpType autopilot.BumpType

			// Determine version
			if len(args) > 0 {
				// Explicit version provided
				newVersion, err = autopilot.ParseSemVer(args[0])
				if err != nil {
					return fmt.Errorf("invalid version: %w", err)
				}
				bumpType = autopilot.BumpNone // Not applicable for explicit version
			} else if bump != "" {
				// Force bump type
				switch bump {
				case "patch":
					bumpType = autopilot.BumpPatch
				case "minor":
					bumpType = autopilot.BumpMinor
				case "major":
					bumpType = autopilot.BumpMajor
				default:
					return fmt.Errorf("invalid bump type: %s (use: patch, minor, major)", bump)
				}
				newVersion = currentVersion.Bump(bumpType)
			} else {
				// Auto-detect from commits
				latestRelease, _ := ghClient.GetLatestRelease(ctx, owner, repo)
				var baseRef string
				if latestRelease != nil {
					baseRef = latestRelease.TagName
				}

				var commits []*github.Commit
				if baseRef != "" {
					commits, err = ghClient.CompareCommits(ctx, owner, repo, baseRef, "HEAD")
					if err != nil {
						return fmt.Errorf("failed to get commits: %w", err)
					}
				}

				bumpType = autopilot.DetectBumpType(commits)
				if bumpType == autopilot.BumpNone {
					fmt.Println("No releasable commits found (no feat/fix commits)")
					return nil
				}
				newVersion = currentVersion.Bump(bumpType)
			}

			versionStr := newVersion.String(releaseCfg.TagPrefix)

			if dryRun {
				fmt.Printf("Would create release:\n")
				fmt.Printf("  Current version: %s\n", currentVersion.String(releaseCfg.TagPrefix))
				fmt.Printf("  New version: %s\n", versionStr)
				fmt.Printf("  Bump type: %s\n", bumpType)
				fmt.Printf("  Draft: %v\n", draft)
				return nil
			}

			fmt.Printf("Creating release %s...\n", versionStr)

			input := &github.ReleaseInput{
				TagName:         versionStr,
				TargetCommitish: "main",
				Name:            versionStr,
				Body:            fmt.Sprintf("Release %s", versionStr),
				Draft:           draft,
				GenerateNotes:   true, // Let GitHub generate release notes
			}

			release, err := ghClient.CreateRelease(ctx, owner, repo, input)
			if err != nil {
				return fmt.Errorf("failed to create release: %w", err)
			}

			fmt.Printf("✨ Release %s created!\n", versionStr)
			fmt.Printf("   URL: %s\n", release.HTMLURL)

			return nil
		},
	}

	cmd.Flags().StringVar(&bump, "bump", "", "Force bump type: patch, minor, major")
	cmd.Flags().BoolVar(&draft, "draft", false, "Create release as draft")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be released without creating")

	return cmd
}
