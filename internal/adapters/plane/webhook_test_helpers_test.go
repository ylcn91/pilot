package plane

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func computeSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func makePayload(t *testing.T, event, action string, data WebhookWorkItemData) []byte {
	t.Helper()
	wp := map[string]interface{}{
		"event":        event,
		"action":       action,
		"webhook_id":   "wh-123",
		"workspace_id": "ws-456",
		"data":         data,
	}
	b, err := json.Marshal(wp)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	return b
}
