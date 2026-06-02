package autopilot

import (
	"log/slog"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// CIMonitor watches GitHub CI status for PRs.
type CIMonitor struct {
	ghClient       *github.Client
	owner          string
	repo           string
	pollInterval   time.Duration
	waitTimeout    time.Duration
	requiredChecks []string
	log            *slog.Logger

	// CI checks configuration (auto-discovery)
	ciChecks *CIChecksConfig

	// Discovery state for auto mode
	discoveredChecks map[string][]string  // sha -> check names
	discoveryStart   map[string]time.Time // sha -> when discovery started
	mu               sync.RWMutex
}

// NewCIMonitor creates a CI monitor with configuration from Config.
// The effective CI wait timeout is the minimum of CIWaitTimeout (user override) and
// the environment-specific CITimeout. This lets environments define shorter timeouts
// (e.g. dev uses 5m) while still respecting explicit user overrides in tests or configs.
// Handles both legacy RequiredChecks and new CIChecks configuration.
func NewCIMonitor(ghClient *github.Client, owner, repo string, cfg *Config) *CIMonitor {
	timeout := cfg.CIWaitTimeout
	envCITimeout := cfg.ResolvedEnv().CITimeout
	if envCITimeout > 0 && (timeout == 0 || envCITimeout < timeout) {
		timeout = envCITimeout
	}

	// Determine CI checks configuration
	var ciChecks *CIChecksConfig
	var requiredChecks []string

	if cfg.CIChecks != nil {
		ciChecks = cfg.CIChecks
		// If manual mode, use the Required list
		if ciChecks.Mode == "manual" && len(ciChecks.Required) > 0 {
			requiredChecks = ciChecks.Required
		}
	} else if len(cfg.RequiredChecks) > 0 {
		// Legacy: if RequiredChecks is set, use manual mode
		ciChecks = &CIChecksConfig{
			Mode:     "manual",
			Required: cfg.RequiredChecks,
		}
		requiredChecks = cfg.RequiredChecks
	} else {
		// Default: auto mode
		ciChecks = &CIChecksConfig{
			Mode:                 "auto",
			DiscoveryGracePeriod: 60 * time.Second,
		}
	}

	// Ensure grace period has a default
	if ciChecks.DiscoveryGracePeriod == 0 {
		ciChecks.DiscoveryGracePeriod = 60 * time.Second
	}

	return &CIMonitor{
		ghClient:         ghClient,
		owner:            owner,
		repo:             repo,
		pollInterval:     cfg.CIPollInterval,
		waitTimeout:      timeout,
		requiredChecks:   requiredChecks,
		ciChecks:         ciChecks,
		discoveredChecks: make(map[string][]string),
		discoveryStart:   make(map[string]time.Time),
		log:              slog.Default().With("component", "ci-monitor"),
	}
}
