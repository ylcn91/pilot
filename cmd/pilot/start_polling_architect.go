package main

import (
	"log/slog"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

// architectEnabled reports whether a periodic Architect radar should run for
// this config: the Architect block must be present, Enabled, and carry a
// non-empty cron Schedule. An enabled-but-scheduleless config is "on-demand
// only" — it still gets a gateway provider (so the API exists) but no
// background scheduler.
func architectEnabled(cfg *config.Config) bool {
	a := cfg.Architect
	return a != nil && a.Enabled && a.Schedule != ""
}

// architectRadarConfig projects the daemon's config + project path into the
// architect.RadarConfig the scheduler feeds to RunRadar. Thresholds map onto
// the deterministic collectors' ScanOptions; the LLM Backend is intentionally
// ignored here because RunRadar is fully offline (it never spawns a backend or
// files issues).
func architectRadarConfig(cfg *config.Config, projectPath string) architect.RadarConfig {
	opts := architect.ScanOptions{}
	if a := cfg.Architect; a != nil {
		opts.MinCoverage = a.Thresholds.MinCoverage
		opts.Signals = a.Signals
	}
	return architect.RadarConfig{
		ProjectPath: projectPath,
		Options:     opts,
	}
}

// architectSchedulerConfig projects config.ArchitectConfig's cron fields onto
// the leaf architect.SchedulerConfig (keeping internal/architect free of an
// internal/config import).
func architectSchedulerConfig(cfg *config.Config) architect.SchedulerConfig {
	a := cfg.Architect
	if a == nil {
		return architect.SchedulerConfig{}
	}
	return architect.SchedulerConfig{
		Enabled:  a.Enabled,
		Schedule: a.Schedule,
		Timezone: a.Timezone,
	}
}

// startArchitectScheduler constructs and starts the periodic radar scheduler
// when the feature is enabled and the gateway-injected FindingsStore exists.
// The store is shared with the gateway (set in setupGateway) so each scan's
// findings are immediately visible on web/TUI/desktop. A nil store (feature
// disabled, or no gateway) makes this a no-op. The scheduler is recorded on the
// runtime so run() can Stop it alongside the other schedulers.
func (p *pollingRuntime) startArchitectScheduler() {
	cfg := p.cfg

	if p.architectStore == nil || !architectEnabled(cfg) {
		return
	}

	scheduler := architect.NewScheduler(
		architectRadarConfig(cfg, p.projectPath),
		architectSchedulerConfig(cfg),
		p.architectStore,
		slog.Default(),
	)

	if err := scheduler.Start(p.ctx); err != nil {
		logging.WithComponent("start").Warn("Failed to start architect radar scheduler", slog.Any("error", err))
		return
	}

	logging.WithComponent("start").Info("architect radar scheduler started",
		slog.String("schedule", cfg.Architect.Schedule),
		slog.String("timezone", cfg.Architect.Timezone),
		slog.String("project_path", p.projectPath),
	)
	p.architectScheduler = scheduler
}
