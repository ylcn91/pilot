package budget

import (
	"context"
	"testing"
	"time"
)

// TestTaskLimiterCreateContextTimeoutFires verifies the timeout-cancellation
// path of CreateContext: with a positive maxDuration the returned context is
// cancelled with DeadlineExceeded once the duration elapses. Synchronization is
// event-driven (select on ctx.Done()) rather than a fixed assertion sleep; a
// generous safety timeout guards against a hang.
func TestTaskLimiterCreateContextTimeoutFires(t *testing.T) {
	limiter := NewTaskLimiter(0, 20*time.Millisecond)

	ctx, cancel := limiter.CreateContext(context.Background())
	defer cancel()

	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != context.DeadlineExceeded {
			t.Errorf("expected DeadlineExceeded, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("context did not cancel within the safety timeout")
	}
}

// TestTaskLimiterCreateContextParentCancellation verifies the no-duration-limit
// path (context.WithCancel) still propagates parent cancellation.
func TestTaskLimiterCreateContextParentCancellation(t *testing.T) {
	limiter := NewTaskLimiter(0, 0) // no duration limit -> WithCancel branch

	parent, parentCancel := context.WithCancel(context.Background())
	ctx, cancel := limiter.CreateContext(parent)
	defer cancel()

	if _, ok := ctx.Deadline(); ok {
		t.Error("expected no deadline when maxDuration is zero")
	}

	parentCancel()

	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != context.Canceled {
			t.Errorf("expected Canceled propagated from parent, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("child context did not cancel after parent cancellation")
	}
}
