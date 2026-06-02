package approval

import (
	"context"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

func TestTelegramHandler_WithStore_PersistOnSend(t *testing.T) {
	client := &mockTelegramClient{}
	store := newMockPendingStore()
	handler := NewTelegramHandler(client, "chat123").WithStore(store)

	req := &Request{
		ID: "persist-1", TaskID: "T-1", Stage: StagePreMerge,
		Title: "Test", ExpiresAt: time.Now().Add(time.Hour),
	}
	if _, err := handler.SendApprovalRequest(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.len() != 1 {
		t.Fatalf("expected 1 persisted row, got %d", store.len())
	}
	row := store.get("persist-1")
	if row == nil {
		t.Fatal("expected row to be stored")
	}
	if row.TaskID != "T-1" {
		t.Errorf("expected TaskID T-1, got %s", row.TaskID)
	}
}

func TestTelegramHandler_WithStore_DeleteOnCallback(t *testing.T) {
	client := &mockTelegramClient{}
	store := newMockPendingStore()
	handler := NewTelegramHandler(client, "chat123").WithStore(store)

	req := &Request{
		ID: "del-cb-1", TaskID: "T-2", Stage: StagePreMerge,
		Title: "Test", ExpiresAt: time.Now().Add(time.Hour),
	}
	if _, err := handler.SendApprovalRequest(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.len() != 1 {
		t.Fatal("expected 1 row after send")
	}

	handler.HandleCallback(context.Background(), "cb1", "approve:del-cb-1", "u", "user")

	if store.len() != 0 {
		t.Errorf("expected 0 rows after callback, got %d", store.len())
	}
}

func TestTelegramHandler_WithStore_DeleteOnCancel(t *testing.T) {
	client := &mockTelegramClient{}
	store := newMockPendingStore()
	handler := NewTelegramHandler(client, "chat123").WithStore(store)

	req := &Request{
		ID: "del-cancel-1", TaskID: "T-3", Stage: StagePreMerge,
		Title: "Test", ExpiresAt: time.Now().Add(time.Hour),
	}
	if _, err := handler.SendApprovalRequest(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := handler.CancelRequest(context.Background(), "del-cancel-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.len() != 0 {
		t.Errorf("expected 0 rows after cancel, got %d", store.len())
	}
}

func TestTelegramHandler_Rehydrate_RestoреsNonExpired(t *testing.T) {
	client := &mockTelegramClient{}
	store := newMockPendingStore()

	// Pre-populate store with one non-expired and one expired row.
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	_ = store.InsertPendingApproval(&memory.PendingApproval{
		ID: "live", TaskID: "T-live", Stage: "pre_merge",
		Title: "Live", CreatedAt: time.Now(), ExpiresAt: future,
	})
	_ = store.InsertPendingApproval(&memory.PendingApproval{
		ID: "dead", TaskID: "T-dead", Stage: "pre_merge",
		Title: "Dead", CreatedAt: time.Now(), ExpiresAt: past,
	})

	handler := NewTelegramHandler(client, "chat123").WithStore(store)
	if err := handler.Rehydrate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handler.mu.RLock()
	_, livePending := handler.pending["live"]
	_, deadPending := handler.pending["dead"]
	handler.mu.RUnlock()

	if !livePending {
		t.Error("expected non-expired approval to be rehydrated")
	}
	if deadPending {
		t.Error("expected expired approval to NOT be rehydrated")
	}
	// Expired row should be pruned from store.
	if store.get("dead") != nil {
		t.Error("expected expired row to be deleted from store")
	}
}

func TestTelegramHandler_Rehydrate_NoStore(t *testing.T) {
	client := &mockTelegramClient{}
	handler := NewTelegramHandler(client, "chat123")
	// No store attached — Rehydrate should be a no-op.
	if err := handler.Rehydrate(context.Background()); err != nil {
		t.Fatalf("expected no error without store, got: %v", err)
	}
}

func TestTelegramHandler_Rehydrate_CallbackWorksAfterRehydrate(t *testing.T) {
	client := &mockTelegramClient{}
	store := newMockPendingStore()
	_ = store.InsertPendingApproval(&memory.PendingApproval{
		ID: "rehy-cb", TaskID: "T-R", Stage: "pre_merge",
		Title: "Rehydrated", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	})

	handler := NewTelegramHandler(client, "chat123").WithStore(store)
	if err := handler.Rehydrate(context.Background()); err != nil {
		t.Fatalf("rehydrate error: %v", err)
	}

	// Simulate a button tap arriving after restart — should NOT answer "expired".
	handled := handler.HandleCallback(context.Background(), "cb-r", "approve:rehy-cb", "u", "tester")
	if !handled {
		t.Error("expected callback to be handled")
	}

	cbs := client.getAnsweredCallbacks()
	if len(cbs) == 0 {
		t.Fatal("expected callback answer")
	}
	if containsString(cbs[0].Text, "expired") {
		t.Errorf("expected non-expired answer, got: %s", cbs[0].Text)
	}
}
