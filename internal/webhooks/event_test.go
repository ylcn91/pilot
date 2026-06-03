package webhooks

import (
	"strings"
	"sync"
	"testing"
)

func TestRandomString_LengthAndCharset(t *testing.T) {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	for _, n := range []int{0, 1, 16, 64} {
		got := randomString(n)
		if len(got) != n {
			t.Fatalf("randomString(%d) length = %d, want %d", n, len(got), n)
		}
		for _, r := range got {
			if !strings.ContainsRune(charset, r) {
				t.Fatalf("randomString produced char %q outside charset", r)
			}
		}
	}
}

func TestGenerateEventID_Prefix(t *testing.T) {
	id := generateEventID()
	if !strings.HasPrefix(id, "evt_") {
		t.Fatalf("event ID %q missing evt_ prefix", id)
	}
	if len(id) != len("evt_")+16 {
		t.Fatalf("event ID %q has unexpected length %d", id, len(id))
	}
}

// TestRandomString_ConcurrentUniqueness exercises the path that the old
// time.Now().UnixNano()+Sleep implementation could collide on: many IDs
// generated concurrently must stay unique with crypto/rand.
func TestRandomString_ConcurrentUniqueness(t *testing.T) {
	const (
		goroutines = 16
		perG       = 500
	)

	var (
		mu   sync.Mutex
		seen = make(map[string]struct{}, goroutines*perG)
		wg   sync.WaitGroup
	)

	collisions := 0
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]string, 0, perG)
			for i := 0; i < perG; i++ {
				local = append(local, randomString(16))
			}
			mu.Lock()
			for _, id := range local {
				if _, ok := seen[id]; ok {
					collisions++
				}
				seen[id] = struct{}{}
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if collisions > 0 {
		t.Fatalf("got %d collisions across %d generated 16-char IDs", collisions, goroutines*perG)
	}
}
