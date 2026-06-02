package autopilot

// ReleaseConfig holds configuration for automatic release creation.
type ReleaseConfig struct {
	// Enabled controls whether auto-release is active.
	Enabled bool `yaml:"enabled"`
	// Trigger determines when to release: "on_merge" or "manual".
	Trigger string `yaml:"trigger"`
	// VersionStrategy determines how to bump version: "conventional_commits" or "pr_labels".
	VersionStrategy string `yaml:"version_strategy"`
	// TagPrefix is prepended to version (default "v").
	TagPrefix string `yaml:"tag_prefix"`
	// GenerateChangelog enables changelog generation from commits.
	GenerateChangelog bool `yaml:"generate_changelog"`
	// NotifyOnRelease sends notification when release is created.
	NotifyOnRelease bool `yaml:"notify_on_release"`
	// RequireCI waits for post-merge CI before releasing.
	RequireCI bool `yaml:"require_ci"`
	// GenerateSummary enables LLM-generated release summary prepended to GoReleaser changelog.
	GenerateSummary bool `yaml:"generate_summary"`
}

// DefaultReleaseConfig returns sensible defaults for release configuration.
func DefaultReleaseConfig() *ReleaseConfig {
	return &ReleaseConfig{
		Enabled:           false,
		Trigger:           "on_merge",
		VersionStrategy:   "conventional_commits",
		TagPrefix:         "v",
		GenerateChangelog: true,
		NotifyOnRelease:   true,
		RequireCI:         true,
		GenerateSummary:   true,
	}
}
