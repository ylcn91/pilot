package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/logging"
)

// readWebhookBody enforces POST, reads the raw request body, optionally runs a
// signature verifier over those exact bytes, and decodes the JSON payload.
//
// It centralizes the shared prologue of the io.ReadAll-based webhook handlers
// (method check → read raw body → verify → unmarshal). The raw body is returned
// alongside the decoded payload because several adapters stash it verbatim into
// payload["_raw_body"] for downstream body-HMAC verification.
//
// verify, when non-nil, is responsible for writing its own HTTP error response
// on failure; returning false signals the caller to stop. ok is false whenever
// a response has already been written (method/body/verify/JSON error), in which
// case the caller must return without further writes.
func readWebhookBody(w http.ResponseWriter, r *http.Request, verify func(body []byte) bool) (body []byte, payload map[string]interface{}, ok bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return nil, nil, false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return nil, nil, false
	}

	if verify != nil && !verify(body) {
		return nil, nil, false
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return nil, nil, false
	}

	return body, payload, true
}

// handleLinearWebhook receives webhooks from Linear.
//
// TASK-295: if linearWebhookPublicKey is configured, the body's Ed25519
// signature (sent in the linear-signature header, hex-encoded) is verified
// before JSON parsing. Requests without a valid signature are rejected 401.
// If the public key is not configured, verification is skipped and the
// request is processed as before (logged at WARN so operators notice).
func (s *Server) handleLinearWebhook(w http.ResponseWriter, r *http.Request) {
	signature := r.Header.Get("linear-signature")

	// Read raw body — required for Ed25519 verification (signature covers the
	// exact bytes sent, before JSON parsing normalizes them).
	_, payload, ok := readWebhookBody(w, r, func(body []byte) bool {
		if s.linearWebhookPublicKey != nil {
			if verr := linear.VerifyLinearSignature(s.linearWebhookPublicKey, signature, body); verr != nil {
				logging.WithComponent("gateway").Warn("Linear webhook signature verification failed",
					slog.String("error", verr.Error()),
					slog.String("remote_addr", r.RemoteAddr),
				)
				http.Error(w, "Invalid signature", http.StatusUnauthorized)
				return false
			}
		} else {
			// Public key not configured — log once-per-request at WARN so the
			// operator notices that they are running with verification disabled.
			// Pilot won't refuse the request (development & migration friendliness),
			// but the audit trail makes the gap visible.
			logging.WithComponent("gateway").Warn("Linear webhook signature verification SKIPPED — linearWebhookPublicKey not configured. Set adapters.linear.webhook_public_key to enable Ed25519 verification (TASK-295).")
		}
		return true
	})
	if !ok {
		return
	}

	logging.WithComponent("gateway").Info("Received Linear webhook", slog.Any("type", payload["type"]))

	// Route to Linear adapter
	s.router.HandleWebhook("linear", payload)

	w.WriteHeader(http.StatusOK)
}

// handleGithubWebhook receives webhooks from GitHub
func (s *Server) handleGithubWebhook(w http.ResponseWriter, r *http.Request) {
	// GitHub sends event type in header
	eventType := r.Header.Get("X-GitHub-Event")
	signature := r.Header.Get("X-Hub-Signature-256")

	// Read raw body first (required for HMAC signature validation)
	_, payload, ok := readWebhookBody(w, r, func(body []byte) bool {
		// Validate webhook signature if secret is configured
		if s.githubWebhookSecret != "" {
			if !github.VerifyWebhookSignature(body, signature, s.githubWebhookSecret) {
				logging.WithComponent("gateway").Warn("GitHub webhook signature verification failed",
					slog.String("event_type", eventType))
				http.Error(w, "Invalid signature", http.StatusUnauthorized)
				return false
			}
		}
		return true
	})
	if !ok {
		return
	}

	// Add metadata to payload for handler
	payload["_event_type"] = eventType
	payload["_signature"] = signature

	logging.WithComponent("gateway").Info("Received GitHub webhook", slog.String("event_type", eventType))

	// Route to GitHub adapter
	s.router.HandleWebhook("github", payload)

	w.WriteHeader(http.StatusOK)
}

// handleJiraWebhook receives webhooks from Jira
func (s *Server) handleJiraWebhook(w http.ResponseWriter, r *http.Request) {
	// Jira may send signature in header (if configured)
	signature := r.Header.Get("X-Hub-Signature")

	// Read the raw body once. Body-HMAC verification (TASK-333) must run over
	// the exact bytes Jira signed; decoding into a map and re-marshaling would
	// not reproduce them. Decode from the buffered bytes after stashing them.
	body, payload, ok := readWebhookBody(w, r, nil)
	if !ok {
		return
	}

	// Add metadata to payload for handler
	payload["_signature"] = signature
	payload["_raw_body"] = string(body)

	webhookEvent, _ := payload["webhookEvent"].(string)
	logging.WithComponent("gateway").Info("Received Jira webhook", slog.String("event", webhookEvent))

	// Route to Jira adapter
	s.router.HandleWebhook("jira", payload)

	w.WriteHeader(http.StatusOK)
}

// handleGitlabWebhook receives webhooks from GitLab
func (s *Server) handleGitlabWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// GitLab sends event type in header and uses simple token auth
	eventType := r.Header.Get("X-Gitlab-Event")
	token := r.Header.Get("X-Gitlab-Token")

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Add metadata to payload for handler
	payload["_event_type"] = eventType
	payload["_token"] = token

	logging.WithComponent("gateway").Info("Received GitLab webhook", slog.String("event_type", eventType))

	// Route to GitLab adapter
	s.router.HandleWebhook("gitlab", payload)

	w.WriteHeader(http.StatusOK)
}

// handleBitbucketWebhook receives webhooks from Bitbucket Cloud 2.0
func (s *Server) handleBitbucketWebhook(w http.ResponseWriter, r *http.Request) {
	// Bitbucket Cloud sends the event key in X-Event-Key and signs the body
	// with HMAC-SHA256 in X-Hub-Signature ("sha256=<hex>").
	eventType := r.Header.Get("X-Event-Key")
	signature := r.Header.Get("X-Hub-Signature")

	// Read the raw body once. HMAC verification must run over the exact bytes
	// Bitbucket signed; decoding into a map and re-marshaling would not
	// reproduce them. Decode from the buffered bytes after stashing them.
	body, payload, ok := readWebhookBody(w, r, nil)
	if !ok {
		return
	}

	// Add metadata to payload for handler
	payload["_event_type"] = eventType
	payload["_signature"] = signature
	payload["_raw_body"] = string(body)

	logging.WithComponent("gateway").Info("Received Bitbucket webhook", slog.String("event_type", eventType))

	// Route to Bitbucket adapter
	s.router.HandleWebhook("bitbucket", payload)

	w.WriteHeader(http.StatusOK)
}

// handleAsanaWebhook receives webhooks from Asana
func (s *Server) handleAsanaWebhook(w http.ResponseWriter, r *http.Request) {
	// Asana webhook handshake: respond with X-Hook-Secret header
	if hookSecret := r.Header.Get("X-Hook-Secret"); hookSecret != "" {
		logging.WithComponent("gateway").Info("Received Asana webhook handshake")
		w.Header().Set("X-Hook-Secret", hookSecret)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Asana sends signature in X-Hook-Signature header
	signature := r.Header.Get("X-Hook-Signature")

	// Read the raw body once. Body-HMAC verification (TASK-333) must run over
	// the exact bytes Asana signed; decoding into a map and re-marshaling would
	// not reproduce them. Decode from the buffered bytes after stashing them.
	body, payload, ok := readWebhookBody(w, r, nil)
	if !ok {
		return
	}

	// Add metadata to payload for handler
	payload["_signature"] = signature
	payload["_raw_body"] = string(body)

	logging.WithComponent("gateway").Info("Received Asana webhook")

	// Route to Asana adapter
	s.router.HandleWebhook("asana", payload)

	w.WriteHeader(http.StatusOK)
}

// handleAzureDevOpsWebhook receives webhooks from Azure DevOps service hooks
func (s *Server) handleAzureDevOpsWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Azure DevOps service hooks use basic auth for webhook secret verification
	// The secret is passed in the Authorization header or as a query parameter
	var secret string
	if user, pass, ok := r.BasicAuth(); ok {
		secret = user + ":" + pass
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Add metadata to payload for handler
	payload["_secret"] = secret

	logging.WithComponent("gateway").Info("Received Azure DevOps webhook")

	// Route to Azure DevOps adapter
	s.router.HandleWebhook("azuredevops", payload)

	w.WriteHeader(http.StatusOK)
}

// handlePlaneWebhook receives webhooks from Plane.so
func (s *Server) handlePlaneWebhook(w http.ResponseWriter, r *http.Request) {
	// Plane sends event metadata in headers
	eventType := r.Header.Get("X-Plane-Event")
	signature := r.Header.Get("X-Plane-Signature")
	deliveryID := r.Header.Get("X-Plane-Delivery")

	// Read raw body (needed for signature verification downstream)
	body, payload, ok := readWebhookBody(w, r, nil)
	if !ok {
		return
	}

	// Add metadata to payload for handler
	payload["_event_type"] = eventType
	payload["_signature"] = signature
	payload["_delivery_id"] = deliveryID
	payload["_raw_body"] = string(body)

	logging.WithComponent("gateway").Info("Received Plane webhook",
		slog.String("event_type", eventType),
		slog.String("delivery_id", deliveryID))

	// Route to Plane adapter
	s.router.HandleWebhook("plane", payload)

	w.WriteHeader(http.StatusOK)
}
