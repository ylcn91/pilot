package executor

import (
	"time"
)

// StagnationConfig controls stagnation detection and recovery (GH-925).
// Detects when tasks are stuck in loops, making no progress, or spinning.
//
// Example YAML configuration:
//
//	executor:
//	  stagnation:
//	    enabled: true
//	    warn_timeout: 10m
//	    pause_timeout: 20m
//	    abort_timeout: 30m
//	    warn_at_iteration: 8
//	    abort_at_iteration: 15
//	    commit_partial_work: true
type StagnationConfig struct {
	// Enabled controls whether stagnation detection is active.
	// Default: false (disabled by default).
	Enabled bool `yaml:"enabled"`

	// Timeout thresholds - absolute time since task start
	WarnTimeout  time.Duration `yaml:"warn_timeout"`
	PauseTimeout time.Duration `yaml:"pause_timeout"`
	AbortTimeout time.Duration `yaml:"abort_timeout"`

	// Iteration limits - Claude Code turn count
	WarnAtIteration  int `yaml:"warn_at_iteration"`
	PauseAtIteration int `yaml:"pause_at_iteration"`
	AbortAtIteration int `yaml:"abort_at_iteration"`

	// Loop detection - detect identical states
	StateHistorySize         int `yaml:"state_history_size"`
	IdenticalStatesThreshold int `yaml:"identical_states_threshold"`

	// Recovery settings
	GracePeriod       time.Duration `yaml:"grace_period"`
	CommitPartialWork bool          `yaml:"commit_partial_work"`
}

// DefaultStagnationConfig returns default stagnation detection settings.
func DefaultStagnationConfig() *StagnationConfig {
	return &StagnationConfig{
		Enabled:                  false, // Disabled by default
		WarnTimeout:              10 * time.Minute,
		PauseTimeout:             20 * time.Minute,
		AbortTimeout:             30 * time.Minute,
		WarnAtIteration:          8,
		PauseAtIteration:         12,
		AbortAtIteration:         15,
		StateHistorySize:         5,
		IdenticalStatesThreshold: 3,
		GracePeriod:              30 * time.Second,
		CommitPartialWork:        true,
	}
}

// SubprocessLimitsConfig controls RSS telemetry and optional memory cap for the
// Claude Code subprocess. GH-3028.
//
// Example YAML configuration:
//
//	executor:
//	  subprocess_limits:
//	    enabled: false           # flip to true after one baseline cycle
//	    max_rss_mb: 4096         # cap at 4 GiB virtual address space (Linux only)
//	    sample_interval_sec: 10  # how often to poll /proc/<pid>/status
type SubprocessLimitsConfig struct {
	// Enabled controls whether RLIMIT_AS is applied to the subprocess.
	// Default: false. Flip to true after collecting a baseline week of peak_rss_mb
	// data to choose a safe cap (recommended: p99 × 1.5).
	Enabled bool `yaml:"enabled"`

	// MaxRSSMB is the virtual address space limit in MiB applied via RLIMIT_AS on Linux.
	// Ignored when Enabled=false or on non-Linux platforms.
	// Default: 4096 (4 GiB).
	MaxRSSMB int `yaml:"max_rss_mb,omitempty"`

	// SampleIntervalSec controls how often (in seconds) the RSS sampler polls the subprocess.
	// Default: 10.
	SampleIntervalSec int `yaml:"sample_interval_sec,omitempty"`
}

// DefaultSubprocessLimitsConfig returns safe defaults: telemetry on, cap off.
func DefaultSubprocessLimitsConfig() *SubprocessLimitsConfig {
	return &SubprocessLimitsConfig{
		Enabled:           false,
		MaxRSSMB:          4096,
		SampleIntervalSec: 10,
	}
}
