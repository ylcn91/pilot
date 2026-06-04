package pilot

import (
	"log/slog"

	"github.com/ylcn91/pilot/internal/logging"
)

// Start starts Pilot
func (p *Pilot) Start() error {
	logging.WithComponent("pilot").Info("Starting Pilot")

	// Start alerts engine if initialized
	if p.alertEngine != nil {
		if err := p.alertEngine.Start(p.ctx); err != nil {
			logging.WithComponent("pilot").Warn("Failed to start alerts engine", slog.Any("error", err))
		}
	}

	// Start orchestrator
	p.orchestrator.Start()

	// Start gateway
	p.wg.Add(1)
	go func() {
		defer logging.Recover("pilot.lifecycle.gateway")
		defer p.wg.Done()
		if err := p.gateway.Start(p.ctx); err != nil {
			logging.WithComponent("pilot").Error("Gateway error", slog.Any("error", err))
		}
	}()

	// Start Telegram polling if handler is initialized (GH-349)
	if p.telegramHandler != nil {
		p.telegramHandler.StartPolling(p.ctx)
		logging.WithComponent("pilot").Info("Telegram polling started in gateway mode")
	}

	// Start GitHub polling if poller is initialized (GH-350)
	if p.githubPoller != nil {
		go p.githubPoller.Start(p.ctx)
		logging.WithComponent("pilot").Info("GitHub polling started in gateway mode")
	}

	// Start Slack Socket Mode if handler is initialized (GH-652)
	if p.slackHandler != nil {
		p.wg.Add(1)
		go func() {
			defer logging.Recover("pilot.lifecycle.slack")
			defer p.wg.Done()
			if err := p.slackHandler.StartListening(p.ctx); err != nil {
				logging.WithComponent("pilot").Error("Slack Socket Mode error", slog.Any("error", err))
			}
		}()
		logging.WithComponent("pilot").Info("Slack Socket Mode started in gateway mode")
	}

	logging.WithComponent("pilot").Info("Pilot started",
		slog.String("host", p.config.Gateway.Host),
		slog.Int("port", p.config.Gateway.Port))
	return nil
}

// Stop stops Pilot
func (p *Pilot) Stop() error {
	logging.WithComponent("pilot").Info("Stopping Pilot")

	p.cancel()

	// Stop Telegram polling if enabled (GH-349)
	if p.telegramHandler != nil {
		p.telegramHandler.Stop()
		logging.WithComponent("pilot").Info("Telegram polling stopped")
	}

	// Stop Slack Socket Mode if enabled (GH-652)
	if p.slackHandler != nil {
		p.slackHandler.Stop()
		logging.WithComponent("pilot").Info("Slack Socket Mode stopped")
	}

	// Stop alerts engine
	if p.alertEngine != nil {
		p.alertEngine.Stop()
	}

	p.orchestrator.Stop()
	_ = p.gateway.Shutdown()
	_ = p.store.Close()
	p.wg.Wait()

	logging.WithComponent("pilot").Info("Pilot stopped")
	return nil
}

// Wait waits for Pilot to stop
func (p *Pilot) Wait() {
	p.wg.Wait()
}
