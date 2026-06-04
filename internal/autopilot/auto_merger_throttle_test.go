package autopilot

import (
	"testing"
	"time"
)

// #3: max_merges_per_hour rolling-window throttle.
func TestAutoMerger_MergeAllowed_Window(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		cap       int
		preset    []time.Time // existing merge timestamps
		wantAllow bool
		wantKept  int // expected len(mergeTimes) after the call (pruning side effect)
	}{
		{
			name:      "unlimited cap always allows",
			cap:       0,
			preset:    []time.Time{now, now, now, now, now},
			wantAllow: true,
			// cap<=0 short-circuits before pruning, so preset is untouched.
			wantKept: 5,
		},
		{
			name:      "negative cap treated as unlimited",
			cap:       -1,
			preset:    []time.Time{now},
			wantAllow: true,
			wantKept:  1,
		},
		{
			name:      "under cap allows",
			cap:       3,
			preset:    []time.Time{now, now},
			wantAllow: true,
			wantKept:  2,
		},
		{
			name:      "at cap blocks",
			cap:       3,
			preset:    []time.Time{now, now, now},
			wantAllow: false,
			wantKept:  3,
		},
		{
			name:      "over cap blocks",
			cap:       2,
			preset:    []time.Time{now, now, now},
			wantAllow: false,
			wantKept:  3,
		},
		{
			name: "expired entries pruned then allowed",
			cap:  2,
			preset: []time.Time{
				now.Add(-2 * time.Hour),  // expired
				now.Add(-90 * time.Minute), // expired
				now.Add(-30 * time.Minute), // in window
			},
			wantAllow: true, // only 1 in-window entry, under cap of 2
			wantKept:  1,
		},
		{
			name: "expired entries pruned but still at cap",
			cap:  2,
			preset: []time.Time{
				now.Add(-2 * time.Hour),    // expired
				now.Add(-40 * time.Minute), // in window
				now.Add(-10 * time.Minute), // in window
			},
			wantAllow: false, // 2 in-window entries == cap
			wantKept:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &AutoMerger{
				config:     &Config{MaxMergesPerHour: tt.cap},
				mergeTimes: append([]time.Time(nil), tt.preset...),
			}

			got := m.MergeAllowed()
			if got != tt.wantAllow {
				t.Errorf("MergeAllowed() = %v, want %v", got, tt.wantAllow)
			}
			if len(m.mergeTimes) != tt.wantKept {
				t.Errorf("len(mergeTimes) after call = %d, want %d", len(m.mergeTimes), tt.wantKept)
			}
		})
	}
}

// recordMerge stamps merges and respects the unlimited short-circuit.
func TestAutoMerger_RecordMerge(t *testing.T) {
	t.Run("records under cap and blocks at cap", func(t *testing.T) {
		m := &AutoMerger{config: &Config{MaxMergesPerHour: 2}}

		if !m.MergeAllowed() {
			t.Fatal("expected first merge to be allowed")
		}
		m.recordMerge()
		if !m.MergeAllowed() {
			t.Fatal("expected second merge to be allowed")
		}
		m.recordMerge()
		if m.MergeAllowed() {
			t.Fatal("expected third merge to be blocked at cap")
		}
	})

	t.Run("unlimited cap does not accumulate", func(t *testing.T) {
		m := &AutoMerger{config: &Config{MaxMergesPerHour: 0}}
		m.recordMerge()
		m.recordMerge()
		if len(m.mergeTimes) != 0 {
			t.Errorf("unlimited cap should not record merges, got %d", len(m.mergeTimes))
		}
	})
}
