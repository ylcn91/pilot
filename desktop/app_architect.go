package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// GetArchitectFindings returns the current Architect findings by querying the
// running daemon's /api/v1/architect endpoint. Findings are read-only
// observations/proposals emitted by the Architect family (Radar,
// Dependency-Doctor, …). When the daemon is unreachable or returns no findings
// the result is an empty slice, so the panel always receives a valid payload.
//
// This mirrors fetchAutopilotFromDaemon: the App holds no architect provider of
// its own; it surfaces whatever the live gateway exposes. The gateway wraps the
// list as {"findings": [...], "count": N}, which we unwrap to a flat slice.
func (a *App) GetArchitectFindings() []Finding {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(a.gatewayURL + "/api/v1/architect")
	if err != nil {
		return []Finding{}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return []Finding{}
	}

	var data struct {
		Findings []Finding `json:"findings"`
		Count    int       `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return []Finding{}
	}

	if data.Findings == nil {
		return []Finding{}
	}
	return data.Findings
}
