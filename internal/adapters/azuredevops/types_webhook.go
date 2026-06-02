package azuredevops

import "time"

// Webhook payload types

// WebhookPayload is the base webhook payload from Azure DevOps
type WebhookPayload struct {
	SubscriptionID     string                      `json:"subscriptionId"`
	NotificationID     int                         `json:"notificationId"`
	ID                 string                      `json:"id"`
	EventType          string                      `json:"eventType"`
	PublisherID        string                      `json:"publisherId"`
	Message            *WebhookMessage             `json:"message,omitempty"`
	DetailedMessage    *WebhookMessage             `json:"detailedMessage,omitempty"`
	Resource           map[string]interface{}      `json:"resource"`
	ResourceVersion    string                      `json:"resourceVersion"`
	ResourceContainers map[string]WebhookContainer `json:"resourceContainers"`
	CreatedDate        time.Time                   `json:"createdDate"`
}

// WebhookMessage contains the message text for webhooks
type WebhookMessage struct {
	Text     string `json:"text"`
	HTML     string `json:"html,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

// WebhookContainer contains resource container info
type WebhookContainer struct {
	ID      string `json:"id"`
	BaseURL string `json:"baseUrl"`
}

// WorkItemWebhookResource is the resource payload for work item webhooks
type WorkItemWebhookResource struct {
	ID          int                    `json:"id"`
	WorkItemID  int                    `json:"workItemId,omitempty"`
	Rev         int                    `json:"rev"`
	RevisedBy   *Identity              `json:"revisedBy,omitempty"`
	RevisedDate time.Time              `json:"revisedDate"`
	Fields      map[string]interface{} `json:"fields"`
	URL         string                 `json:"url"`
	Revision    *WorkItemRevision      `json:"revision,omitempty"`
}

// WorkItemRevision represents a work item revision in webhook payload
type WorkItemRevision struct {
	ID     int                    `json:"id"`
	Rev    int                    `json:"rev"`
	Fields map[string]interface{} `json:"fields"`
	URL    string                 `json:"url"`
}

// Webhook event types
const (
	WebhookEventWorkItemCreated  = "workitem.created"
	WebhookEventWorkItemUpdated  = "workitem.updated"
	WebhookEventWorkItemDeleted  = "workitem.deleted"
	WebhookEventWorkItemRestored = "workitem.restored"
	WebhookEventPRCreated        = "git.pullrequest.created"
	WebhookEventPRUpdated        = "git.pullrequest.updated"
	WebhookEventPRMerged         = "git.pullrequest.merged"
)
