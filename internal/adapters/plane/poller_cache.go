package plane

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// cacheLabelIDs fetches and caches the UUIDs for pilot-related labels per project.
// Plane labels are per-project, so each project resolves its own UUIDs: applying
// one project's UUID to another project's work item would silently fail. We
// resolve labels for every configured project rather than stopping at the first
// match so multi-project workspaces transition labels correctly.
func (p *Poller) cacheLabelIDs(ctx context.Context) error {
	pilotLabelName := p.config.PilotLabel
	if pilotLabelName == "" {
		pilotLabelName = LabelPilot
	}

	p.pilotLabelIDs = make(map[string]string)
	p.inProgressLabelIDs = make(map[string]string)
	p.doneLabelIDs = make(map[string]string)
	p.failedLabelIDs = make(map[string]string)

	for _, projectID := range p.config.ProjectIDs {
		labels, err := p.client.ListLabels(ctx, p.config.WorkspaceSlug, projectID)
		if err != nil {
			p.logger.Warn("Failed to list labels for project",
				slog.String("project_id", projectID),
				slog.Any("error", err),
			)
			continue
		}

		for _, label := range labels {
			switch {
			case strings.EqualFold(label.Name, pilotLabelName):
				p.pilotLabelIDs[projectID] = label.ID
			case strings.EqualFold(label.Name, LabelInProgress):
				p.inProgressLabelIDs[projectID] = label.ID
			case strings.EqualFold(label.Name, LabelDone):
				p.doneLabelIDs[projectID] = label.ID
			case strings.EqualFold(label.Name, LabelFailed):
				p.failedLabelIDs[projectID] = label.ID
			}
		}
	}

	if len(p.pilotLabelIDs) == 0 {
		return fmt.Errorf("pilot label %q not found in any configured project", pilotLabelName)
	}

	p.logger.Debug("Cached label IDs",
		slog.Int("projects_with_pilot", len(p.pilotLabelIDs)),
		slog.Int("projects_with_in_progress", len(p.inProgressLabelIDs)),
		slog.Int("projects_with_done", len(p.doneLabelIDs)),
		slog.Int("projects_with_failed", len(p.failedLabelIDs)),
	)

	return nil
}

// cacheStateIDs fetches and caches the UUIDs for started/completed state groups per project.
// GH-1832: Plane states are per-project; we cache them to avoid API calls on every transition.
func (p *Poller) cacheStateIDs(ctx context.Context) {
	p.startedStateIDs = make(map[string]string)
	p.completedStateIDs = make(map[string]string)

	for _, projectID := range p.config.ProjectIDs {
		states, err := p.client.ListStates(ctx, p.config.WorkspaceSlug, projectID)
		if err != nil {
			p.logger.Warn("Failed to list states for project",
				slog.String("project_id", projectID),
				slog.Any("error", err),
			)
			continue
		}

		for _, s := range states {
			switch s.Group {
			case StateGroupStarted:
				if p.startedStateIDs[projectID] == "" {
					p.startedStateIDs[projectID] = s.ID
				}
			case StateGroupCompleted:
				if p.completedStateIDs[projectID] == "" {
					p.completedStateIDs[projectID] = s.ID
				}
			}
		}
	}

	p.logger.Debug("Cached state IDs",
		slog.Int("projects_with_started", len(p.startedStateIDs)),
		slog.Int("projects_with_completed", len(p.completedStateIDs)),
	)
}
