package upgrade

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestCheckHomebrewEarlyExit verifies that check() returns immediately for a
// Homebrew installation without performing any network call, leaving the
// latest-info/last-check state untouched. This is fully deterministic and
// hermetic: no upgrader is constructed and no callback is invoked.
func TestCheckHomebrewEarlyExit(t *testing.T) {
	vc := NewVersionChecker("1.0.0", time.Minute)
	// Force the Homebrew branch regardless of how this test binary was built.
	vc.mu.Lock()
	vc.isHomebrew = true
	vc.latestInfo = nil
	vc.lastCheck = time.Time{}
	vc.mu.Unlock()

	var mu sync.Mutex
	callbackInvoked := false
	vc.OnUpdate(func(info *VersionInfo) {
		mu.Lock()
		callbackInvoked = true
		mu.Unlock()
	})

	vc.check(context.Background())

	if vc.GetLatestInfo() != nil {
		t.Error("expected latestInfo to remain nil after Homebrew early-exit")
	}
	if !vc.LastCheck().IsZero() {
		t.Error("expected lastCheck to remain zero after Homebrew early-exit")
	}

	mu.Lock()
	invoked := callbackInvoked
	mu.Unlock()
	if invoked {
		t.Error("OnUpdate callback must not fire on a Homebrew early-exit")
	}
}

// TestCheckNowHomebrewReturnsError verifies the CheckNow Homebrew early-exit
// returns the stored homebrew error and performs no network call.
func TestCheckNowHomebrewReturnsError(t *testing.T) {
	vc := NewVersionChecker("1.0.0", time.Minute)

	sentinel := context.Canceled // any stable non-nil error works as a sentinel
	vc.mu.Lock()
	vc.isHomebrew = true
	vc.homebrewErr = sentinel
	vc.mu.Unlock()

	info, err := vc.CheckNow(context.Background())
	if info != nil {
		t.Error("expected nil VersionInfo for Homebrew installation")
	}
	if err != sentinel {
		t.Errorf("expected the stored homebrew error, got %v", err)
	}
}

// TestOnUpdateStoresCallback verifies OnUpdate registers the callback under the
// lock without invoking it. The callback firing path inside check() requires a
// live GitHub API call (NewUpgrader hardcodes the real HTTP client with no
// injection seam), so only registration is asserted here.
func TestOnUpdateStoresCallback(t *testing.T) {
	vc := NewVersionChecker("1.0.0", time.Minute)

	vc.OnUpdate(func(info *VersionInfo) {})

	vc.mu.RLock()
	defer vc.mu.RUnlock()
	if vc.onUpdate == nil {
		t.Error("expected OnUpdate to store the callback")
	}
}
