package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/testutil"
)

// recordingMessenger is a comms.Messenger that records whether the shared
// comms.Handler dispatched a callback to it. It lets a test observe whether
// Handler.handleCallback actually reached the comms layer for a given
// CallbackData payload.
type recordingMessenger struct {
	mu              sync.Mutex
	acknowledgments []string
	texts           []string
}

func (r *recordingMessenger) SendText(_ context.Context, _ string, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, text)
	return nil
}

func (r *recordingMessenger) SendConfirmation(context.Context, string, string, string, string, string) (string, error) {
	return "", nil
}

func (r *recordingMessenger) SendProgress(context.Context, string, string, string, string, int, string) (string, error) {
	return "", nil
}

func (r *recordingMessenger) SendResult(context.Context, string, string, string, bool, string, string) error {
	return nil
}

func (r *recordingMessenger) SendChunked(context.Context, string, string, string, string) error {
	return nil
}

func (r *recordingMessenger) AcknowledgeCallback(_ context.Context, callbackID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acknowledgments = append(r.acknowledgments, callbackID)
	return nil
}

func (r *recordingMessenger) MaxMessageLength() int { return 4096 }

func (r *recordingMessenger) ackCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.acknowledgments)
}

// newCallbackDispatchHandler builds a telegram Handler wired to a real
// comms.Handler whose messenger is a recordingMessenger, plus an httptest
// server so the direct h.client.AnswerCallback call in handleCallback succeeds.
func newCallbackDispatchHandler(t *testing.T) (*Handler, *recordingMessenger, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	rec := &recordingMessenger{}
	ch := comms.NewHandler(&comms.HandlerConfig{
		Messenger:    rec,
		TaskIDPrefix: "TG",
	})

	h := &Handler{
		client:       NewClientWithBaseURL(testutil.FakeTelegramBotToken, srv.URL),
		stopCh:       make(chan struct{}),
		commsHandler: ch,
	}
	return h, rec, srv.Close
}

func makeConfirmCallback(id, data string) *CallbackQuery {
	return &CallbackQuery{
		ID:   id,
		Data: data,
		From: &User{ID: 7},
		Message: &Message{
			Chat: &Chat{ID: 555},
		},
	}
}

// TestHandleCallback_ConfirmationDataMismatch documents the CURRENT (buggy)
// behavior: TelegramMessenger.SendConfirmation emits CallbackData of the form
// "execute_task:<id>" / "cancel_task:<id>" (see messenger.go), but
// Handler.handleCallback only matches the bare strings "execute" / "cancel".
// As a result, tapping the Execute/Cancel buttons produced by SendConfirmation
// never reaches the shared comms.Handler.
//
// This test asserts that current behavior (it does NOT change prod code):
//   - bare "execute"/"cancel" DO dispatch to comms (AcknowledgeCallback fires)
//   - the prefixed "execute_task:<id>"/"cancel_task:<id>" forms that
//     SendConfirmation actually emits DO NOT dispatch (no AcknowledgeCallback)
//
// If the mismatch is ever fixed in prod, the prefixed sub-tests below will
// start failing, flagging that this characterization test needs updating.
func TestHandleCallback_ConfirmationDataMismatch(t *testing.T) {
	tests := []struct {
		name         string
		data         string
		wantDispatch bool // whether the comms.Handler is reached (AcknowledgeCallback fires)
	}{
		{name: "bare execute dispatches", data: "execute", wantDispatch: true},
		{name: "bare cancel dispatches", data: "cancel", wantDispatch: true},
		{
			name:         "prefixed execute_task does NOT dispatch (mismatch)",
			data:         "execute_task:TASK-1",
			wantDispatch: false,
		},
		{
			name:         "prefixed cancel_task does NOT dispatch (mismatch)",
			data:         "cancel_task:TASK-1",
			wantDispatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, rec, cleanup := newCallbackDispatchHandler(t)
			defer cleanup()

			cb := makeConfirmCallback("cb-"+tt.name, tt.data)
			h.handleCallback(context.Background(), cb)

			gotDispatch := rec.ackCount() > 0
			if gotDispatch != tt.wantDispatch {
				t.Errorf("data %q: dispatched-to-comms = %v, want %v (AcknowledgeCallback count = %d)",
					tt.data, gotDispatch, tt.wantDispatch, rec.ackCount())
			}
		})
	}
}

// TestSendConfirmation_EmitsPrefixedCallbackData pins the exact CallbackData
// payloads SendConfirmation puts on the inline keyboard, by capturing the real
// reply_markup serialized over the wire. These prefixed strings are what flow
// back into handleCallback, and they are precisely the forms the mismatch test
// above shows handleCallback fails to match.
func TestSendConfirmation_EmitsPrefixedCallbackData(t *testing.T) {
	var captured SendMessageRequest
	m := newTestMessenger(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_ = json.NewEncoder(w).Encode(SendMessageResponse{OK: true, Result: &Result{MessageID: 7}})
	})

	if _, err := m.SendConfirmation(context.Background(), "555", "", "TASK-42", "Add feature", "/proj"); err != nil {
		t.Fatalf("SendConfirmation returned error: %v", err)
	}

	if captured.ReplyMarkup == nil {
		t.Fatal("expected reply_markup with inline keyboard")
	}
	kb := captured.ReplyMarkup.InlineKeyboard
	if len(kb) != 1 || len(kb[0]) != 2 {
		t.Fatalf("unexpected keyboard shape: %+v", kb)
	}
	if got := kb[0][0].CallbackData; got != "execute_task:TASK-42" {
		t.Errorf("execute button CallbackData = %q, want %q", got, "execute_task:TASK-42")
	}
	if got := kb[0][1].CallbackData; got != "cancel_task:TASK-42" {
		t.Errorf("cancel button CallbackData = %q, want %q", got, "cancel_task:TASK-42")
	}
}
