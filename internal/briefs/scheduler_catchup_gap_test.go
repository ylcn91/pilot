package briefs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// fakeTelegramSender is an in-memory TelegramSender that always succeeds,
// letting a catch-up brief be recorded to the store without any network.
type fakeTelegramSender struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeTelegramSender) SendBriefMessage(ctx context.Context, chatID, text, parseMode string) (*TelegramMessageResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return &TelegramMessageResponse{MessageID: 42}, nil
}

func (f *fakeTelegramSender) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newCatchUpScheduler(t *testing.T, store *memory.Store) (*Scheduler, *fakeTelegramSender) {
	t.Helper()
	config := &BriefConfig{
		Enabled:  true,
		Schedule: "0 9 * * *", // daily at 9am
		Timezone: "UTC",
		Channels: []ChannelConfig{
			{Type: "telegram", Channel: "@test"},
		},
		Content: ContentConfig{
			IncludeMetrics:     true,
			IncludeErrors:      true,
			MaxItemsPerSection: 10,
		},
	}
	sender := &fakeTelegramSender{}
	generator := NewGenerator(store, config)
	delivery := NewDeliveryService(config, WithTelegramSender(sender))
	scheduler := NewScheduler(generator, delivery, config, nil, store)
	return scheduler, sender
}

// TestMaybeCatchUpMissedBriefFires drives maybeCatchUp directly with a last
// brief recorded well before the previous scheduled run. The schedule is
// stepped backwards from now; a missed brief must fire delivery, which (via
// the fake telegram sender) records a new brief in the store.
func TestMaybeCatchUpMissedBriefFires(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	// Seed a brief sent 25h ago: for a daily 9am schedule this is before the
	// most recent past scheduled run, so catch-up should fire.
	seed := &memory.BriefRecord{
		SentAt:    time.Now().Add(-25 * time.Hour),
		Channel:   "telegram",
		BriefType: "daily",
	}
	if err := store.RecordBriefSent(seed); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	scheduler, sender := newCatchUpScheduler(t, store)

	scheduler.maybeCatchUp(context.Background())

	if sender.Calls() != 1 {
		t.Fatalf("expected catch-up to deliver exactly one brief, got %d sends", sender.Calls())
	}

	// A successful delivery is recorded to the store under the delivery
	// channel identifier ("telegram:@test"); the catch-up detection itself
	// keys on the representative "telegram" channel, which still holds the
	// older seed.
	recorded, err := store.GetLastBriefSent("telegram:@test")
	if err != nil {
		t.Fatalf("GetLastBriefSent: %v", err)
	}
	if recorded == nil {
		t.Error("expected the fired catch-up delivery to be recorded to the store")
	}
}

// TestMaybeCatchUpNeverSentFires verifies that with no prior brief recorded the
// catch-up detection treats it as missed and fires.
func TestMaybeCatchUpNeverSentFires(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	scheduler, sender := newCatchUpScheduler(t, store)

	scheduler.maybeCatchUp(context.Background())

	if sender.Calls() != 1 {
		t.Errorf("expected catch-up to fire when no brief was ever sent, got %d sends", sender.Calls())
	}
}

// TestMaybeCatchUpRecentBriefDoesNotFire verifies that a brief sent after the
// previous scheduled run is not treated as missed, so no delivery occurs.
func TestMaybeCatchUpRecentBriefDoesNotFire(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	// Sent "now": prevScheduled is always <= now (the loop stops once the next
	// run would be after now), so a record at now is never Before(prevScheduled)
	// and catch-up must not fire. This avoids the narrow flaky window just
	// after the scheduled hour.
	seed := &memory.BriefRecord{
		SentAt:    time.Now(),
		Channel:   "telegram",
		BriefType: "daily",
	}
	if err := store.RecordBriefSent(seed); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	scheduler, sender := newCatchUpScheduler(t, store)

	scheduler.maybeCatchUp(context.Background())

	if sender.Calls() != 0 {
		t.Errorf("expected no catch-up delivery for a recent brief, got %d sends", sender.Calls())
	}
}

// TestMaybeCatchUpNilStoreSkips verifies the graceful-degradation branch where
// no store is configured: maybeCatchUp must return without panicking.
func TestMaybeCatchUpNilStoreSkips(t *testing.T) {
	config := &BriefConfig{
		Enabled:  true,
		Schedule: "0 9 * * *",
		Timezone: "UTC",
		Channels: []ChannelConfig{{Type: "telegram", Channel: "@test"}},
	}
	sender := &fakeTelegramSender{}
	generator := NewGenerator(nil, config)
	delivery := NewDeliveryService(config, WithTelegramSender(sender))
	scheduler := NewScheduler(generator, delivery, config, nil, nil)

	scheduler.maybeCatchUp(context.Background())

	if sender.Calls() != 0 {
		t.Errorf("expected no delivery when store is nil, got %d sends", sender.Calls())
	}
}
