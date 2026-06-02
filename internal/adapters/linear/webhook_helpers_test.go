package linear

import (
	"context"

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

	// Check if issue has pilot label
	if !h.hasPilotLabel(data) {
		return nil
	}

	// Fetch full issue details from mock server
	issueID, _ := data["id"].(string)
	issue, err := h.getIssue(ctx, issueID)
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

func (h *testWebhookHandler) hasPilotLabel(data map[string]interface{}) bool {
	labels, ok := data["labels"].([]interface{})
	if !ok {
		labelIDs, ok := data["labelIds"].([]interface{})
		if !ok {
			return false
		}
		return len(labelIDs) > 0
	}

	for _, label := range labels {
		labelMap, ok := label.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := labelMap["name"].(string)
		if name == h.pilotLabel {
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
