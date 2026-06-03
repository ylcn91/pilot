package slack

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postBlockActions builds a dev-mode (unsigned) POST request carrying the given
// block_actions payload. It reuses the empty-secret bypass so the test focuses
// purely on the onAction dispatch loop rather than signature verification.
func postBlockActions(t *testing.T, payload InteractionPayload) *http.Request {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	form := url.Values{}
	form.Set("payload", string(b))
	req := httptest.NewRequest(http.MethodPost, "/slack/interaction", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// TestServeHTTP_DispatchesEachAction verifies the Actions loop invokes onAction
// once per action, in order, with the correct ActionID/Value pairs. Existing
// ServeHTTP tests only exercise single-action payloads.
func TestServeHTTP_DispatchesEachAction(t *testing.T) {
	h := NewInteractionHandler("")

	var got []InteractionAction
	h.OnAction(func(a *InteractionAction) bool {
		got = append(got, *a)
		return true
	})

	payload := InteractionPayload{
		Type:        "block_actions",
		ResponseURL: "https://hooks.slack.test/actions/MULTI",
		User:        &InteractionUser{ID: "U777", Username: "multi"},
		Channel:     &InteractionChannel{ID: "C777"},
		Message:     &InteractionMessage{TS: "111.222"},
		Actions: []InteractionActionDef{
			{ActionID: "approve", Value: "pr-1"},
			{ActionID: "reject", Value: "pr-2"},
			{ActionID: "merge", Value: "pr-3"},
		},
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, postBlockActions(t, payload))

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(got) != 3 {
		t.Fatalf("onAction called %d times, want 3", len(got))
	}

	want := []struct {
		actionID string
		value    string
	}{
		{"approve", "pr-1"},
		{"reject", "pr-2"},
		{"merge", "pr-3"},
	}
	for i, w := range want {
		if got[i].ActionID != w.actionID {
			t.Errorf("action[%d].ActionID = %q, want %q", i, got[i].ActionID, w.actionID)
		}
		if got[i].Value != w.value {
			t.Errorf("action[%d].Value = %q, want %q", i, got[i].Value, w.value)
		}
		// Shared payload fields should be propagated to every dispatched action.
		if got[i].UserID != "U777" {
			t.Errorf("action[%d].UserID = %q, want U777", i, got[i].UserID)
		}
		if got[i].ResponseURL != "https://hooks.slack.test/actions/MULTI" {
			t.Errorf("action[%d].ResponseURL = %q, want MULTI url", i, got[i].ResponseURL)
		}
	}
}

// TestServeHTTP_OnActionFalseStillOK verifies that a handler reporting the
// action as unhandled (returning false) does not change the HTTP outcome:
// ServeHTTP discards the bool and still responds 200.
func TestServeHTTP_OnActionFalseStillOK(t *testing.T) {
	h := NewInteractionHandler("")

	calls := 0
	h.OnAction(func(a *InteractionAction) bool {
		calls++
		return false
	})

	payload := InteractionPayload{
		Type:    "block_actions",
		Actions: []InteractionActionDef{{ActionID: "noop", Value: "x"}},
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, postBlockActions(t, payload))

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if calls != 1 {
		t.Errorf("onAction called %d times, want 1", calls)
	}
}

// TestServeHTTP_EmptyActionsSlice verifies a block_actions payload with no
// actions: the dispatch loop has nothing to iterate, onAction is never called,
// and the handler still returns 200.
func TestServeHTTP_EmptyActionsSlice(t *testing.T) {
	h := NewInteractionHandler("")

	called := false
	h.OnAction(func(a *InteractionAction) bool {
		called = true
		return true
	})

	payload := InteractionPayload{
		Type:    "block_actions",
		Actions: []InteractionActionDef{},
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, postBlockActions(t, payload))

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if called {
		t.Error("onAction must not be called when Actions is empty")
	}
}

// TestServeHTTP_NilOptionalPayloadFields verifies the dispatch loop tolerates a
// block_actions payload whose User/Channel/Message pointers are nil: the action
// is still dispatched (with empty identity fields) and the handler returns 200.
// Existing tests always populate these pointers.
func TestServeHTTP_NilOptionalPayloadFields(t *testing.T) {
	h := NewInteractionHandler("")

	var gotAction *InteractionAction
	h.OnAction(func(a *InteractionAction) bool {
		gotAction = a
		return true
	})

	payload := InteractionPayload{
		Type:        "block_actions",
		ResponseURL: "https://hooks.slack.test/actions/NIL",
		Actions:     []InteractionActionDef{{ActionID: "approve", Value: "pr-5"}},
		// User, Channel, Message intentionally nil.
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, postBlockActions(t, payload))

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if gotAction == nil {
		t.Fatal("onAction was not called")
	}
	if gotAction.ActionID != "approve" || gotAction.Value != "pr-5" {
		t.Errorf("action = %+v, want ActionID=approve Value=pr-5", gotAction)
	}
	if gotAction.UserID != "" || gotAction.Username != "" || gotAction.ChannelID != "" || gotAction.MessageTS != "" {
		t.Errorf("identity fields should be empty with nil payload pointers, got %+v", gotAction)
	}
	if gotAction.ResponseURL != "https://hooks.slack.test/actions/NIL" {
		t.Errorf("ResponseURL = %q, want NIL url", gotAction.ResponseURL)
	}
}
