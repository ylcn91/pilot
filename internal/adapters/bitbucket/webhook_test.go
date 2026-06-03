package bitbucket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func signPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	payload := []byte(`{"event":"test"}`)
	validSig := signPayload(payload, fakeWebhookSecret)

	tests := []struct {
		name      string
		secret    string
		signature string
		wantValid bool
		setEnv    bool
	}{
		{
			name:      "valid signature",
			secret:    fakeWebhookSecret,
			signature: validSig,
			wantValid: true,
		},
		{
			name:      "invalid signature",
			secret:    fakeWebhookSecret,
			signature: "sha256=deadbeef",
			wantValid: false,
		},
		{
			name:      "missing sha256 prefix",
			secret:    fakeWebhookSecret,
			signature: "deadbeef",
			wantValid: false,
		},
		{
			name:      "empty signature",
			secret:    fakeWebhookSecret,
			signature: "",
			wantValid: false,
		},
		{
			name:      "no secret configured fail-closed",
			secret:    "",
			signature: validSig,
			wantValid: false,
		},
		{
			name:      "no secret, dev flag allows through",
			secret:    "",
			signature: "anything",
			wantValid: true,
			setEnv:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("PILOT_ALLOW_UNSIGNED_WEBHOOKS", "1")
			}
			client := NewClient(fakeToken, "ws", "repo")
			handler := NewWebhookHandler(client, tt.secret, "pilot")

			got := handler.VerifySignature(payload, tt.signature)
			if got != tt.wantValid {
				t.Errorf("VerifySignature() = %v, want %v", got, tt.wantValid)
			}
		})
	}
}

func TestNewWebhookHandler(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	handler := NewWebhookHandler(client, fakeWebhookSecret, "pilot")

	if handler == nil {
		t.Fatal("NewWebhookHandler returned nil")
	}
	if handler.webhookSecret != fakeWebhookSecret {
		t.Errorf("handler.webhookSecret = %s, want %s", handler.webhookSecret, fakeWebhookSecret)
	}
	if handler.pilotLabel != "pilot" {
		t.Errorf("handler.pilotLabel = %s, want pilot", handler.pilotLabel)
	}
}

func TestWebhookHandler_OnIssue(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	handler := NewWebhookHandler(client, fakeWebhookSecret, "pilot")

	handler.OnIssue(func(ctx context.Context, issue *Issue, repo *Repository) error {
		return nil
	})

	if handler.onIssue == nil {
		t.Error("OnIssue callback was not registered")
	}
}

func TestWebhookHandler_MatchesPilotLabel(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	handler := NewWebhookHandler(client, fakeWebhookSecret, "pilot")

	tests := []struct {
		name  string
		issue *Issue
		want  bool
	}{
		{
			name:  "kind matches pilot",
			issue: &Issue{Kind: "pilot"},
			want:  true,
		},
		{
			name:  "synthesized label matches pilot",
			issue: &Issue{Labels: []string{"pilot"}},
			want:  true,
		},
		{
			name:  "no match",
			issue: &Issue{Kind: "bug", Labels: []string{"bug"}},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.matchesPilotLabel(tt.issue)
			if got != tt.want {
				t.Errorf("matchesPilotLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookHandler_HandlePREventNoop(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	handler := NewWebhookHandler(client, fakeWebhookSecret, "pilot")

	// PR events should be accepted as no-ops in STEP 1 (no issue callback fires).
	called := false
	handler.OnIssue(func(ctx context.Context, issue *Issue, repo *Repository) error {
		called = true
		return nil
	})

	err := handler.Handle(context.Background(), WebhookEventPRCreated, &WebhookPayload{
		PullRequest: &PullRequest{ID: 1, State: PRStateOpen},
	})
	if err != nil {
		t.Errorf("Handle(pullrequest:created) error = %v, want nil", err)
	}
	if called {
		t.Error("onIssue should not be called for PR events")
	}
}

func TestWebhookEventConstants(t *testing.T) {
	tests := []struct {
		constant string
		expected string
	}{
		{WebhookEventIssueCreated, "issue:created"},
		{WebhookEventIssueUpdated, "issue:updated"},
		{WebhookEventPRCreated, "pullrequest:created"},
		{WebhookEventPRMerged, "pullrequest:fulfilled"},
		{WebhookEventPRDeclined, "pullrequest:rejected"},
	}

	for _, tt := range tests {
		if tt.constant != tt.expected {
			t.Errorf("constant = %s, want %s", tt.constant, tt.expected)
		}
	}
}
