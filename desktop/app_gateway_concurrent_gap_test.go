package main

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEnsureGatewayRunning_ConcurrentSingleSpawn verifies the gatewayStarting
// guard: two concurrent EnsureGatewayRunning calls must spawn the daemon at
// most once. The second caller observes gatewayStarting=true (or an
// already-started cmd) and falls through to waitForGateway instead of spawning
// a duplicate process.
func TestEnsureGatewayRunning_ConcurrentSingleSpawn(t *testing.T) {
	var healthy atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// release gates the first spawn so the second caller is guaranteed to arrive
	// while gatewayStarting is still true.
	release := make(chan struct{})
	inSpawn := make(chan struct{}, 1)
	var spawns int32

	app := &App{
		httpClient: &http.Client{Timeout: 2 * time.Second},
		gatewayURL: srv.URL,
		startGatewayProcess: func(string, string) (*exec.Cmd, error) {
			atomic.AddInt32(&spawns, 1)
			select {
			case inSpawn <- struct{}{}:
			default:
			}
			<-release // block so the concurrent caller collides on gatewayStarting
			healthy.Store(true)
			return &exec.Cmd{}, nil
		},
		getwd: func() (string, error) { return t.TempDir(), nil },
		sleep: func(time.Duration) {},
	}

	var wg sync.WaitGroup
	statuses := make([]ServerStatus, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		statuses[0] = app.EnsureGatewayRunning()
	}()

	// Wait until the first caller is inside startGatewayProcess (so
	// gatewayStarting==true), then launch the second caller and release.
	<-inSpawn

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Small spin to let the second caller pass GetServerStatus and reach the
		// gatewayStarting check while the first is still blocked in spawn.
		statuses[1] = app.EnsureGatewayRunning()
	}()

	// Give the second goroutine time to reach the guard, then unblock the spawn.
	time.Sleep(50 * time.Millisecond)
	close(release)

	wg.Wait()

	if got := atomic.LoadInt32(&spawns); got != 1 {
		t.Fatalf("startGatewayProcess called %d times, want exactly 1 (gatewayStarting guard)", got)
	}
	for i, s := range statuses {
		if !s.Running {
			t.Errorf("caller %d: gateway not running, err=%q", i, s.Error)
		}
	}
}
