package plane

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// cacheLabelIDs fetches and caches the UUIDs for pilot-related labels across all configured projects.
// Plane labels are per-project, so we resolve from the first project that has matching labels.
func (p *Poller) cacheLabelIDs(ctx context.Context) error {
	pilotLabelName := p.config.PilotLabel
	if pilotLabelName == "" {
		pilotLabelName = LabelPilot
	}

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
				p.pilotLabelID = label.ID
			case strings.EqualFold(label.Name, LabelInProgress):
				p.inProgressLabelID = label.ID
			case strings.EqualFold(label.Name, LabelDone):
				p.doneLabelID = label.ID
			case strings.EqualFold(label.Name, LabelFailed):
				p.failedLabelID = label.ID
			}
		}

		// If we found the pilot label, stop looking
		if p.pilotLabelID != "" {
			break
		}
	}

	if p.pilotLabelID == "" {
		return fmt.Errorf("pilot label %q not found in any configured project", pilotLabelName)
	}

	p.logger.Debug("Cached label IDs",
		slog.String("pilot", p.pilotLabelID),
		slog.String("in_progress", p.inProgressLabelID),
		slog.String("done", p.doneLabelID),
		slog.String("failed", p.failedLabelID),
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
