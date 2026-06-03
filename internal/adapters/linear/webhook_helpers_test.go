package linear

import (
	"context"
	"encoding/json"

	"github.com/ylcn91/pilot/internal/testutil"
)

// testWebhookHandler is a test helper that mimics WebhookHandler behavior
// but allows injecting a test server URL for fetching issues
type testWebhookHandler struct {
	pilotLabel string
	serverURL  string
	projectIDs []string
	onIssue    func(context.Context, *Issue) error
}

func (h *testWebhookHandler) Handle(ctx context.Context, payload map[string]interface{}) error {
	action, _ := payload["action"].(string)
	eventType, _ := payload["type"].(string)

	// Only process issue creation events
	if action != "create" || eventType != "Issue" {
		return nil
	}

	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		return nil
	}

	rawData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var payloadIssue Issue
	if err := json.Unmarshal(rawData, &payloadIssue); err != nil {
		return err
	}

	// Check if issue has pilot label
	if !h.hasPilotLabel(&payloadIssue) {
		return nil
	}

	// Fetch full issue details from mock server
	issue, err := h.getIssue(ctx, payloadIssue.ID)
	if err != nil {
		return err
	}

	// Check project filter
	if !h.isAllowedProject(issue) {
		return nil
	}

	// Call the callback
	if h.onIssue != nil {
		return h.onIssue(ctx, issue)
	}

	return nil
}

func (h *testWebhookHandler) hasPilotLabel(issue *Issue) bool {
	for _, label := range issue.Labels {
		if label.Name == h.pilotLabel {
			return true
		}
	}

	return false
}

func (h *testWebhookHandler) getIssue(ctx context.Context, id string) (*Issue, error) {
	client := newTestableClient(h.serverURL, testutil.FakeLinearAPIKey)
	return client.getIssue(ctx, id)
}

func (h *testWebhookHandler) isAllowedProject(issue *Issue) bool {
	if len(h.projectIDs) == 0 {
		return true
	}
	if issue.Project == nil {
		return false
	}
	for _, pid := range h.projectIDs {
		if issue.Project.ID == pid {
			return true
		}
	}
	return false
}
