package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

func TestLogOutputPath(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"stdout", "stdout", ""},
		{"stderr", "stderr", ""},
		{"empty", "", ""},
		{"file path", "/var/log/pilot.log", "/var/log/pilot.log"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Logging: &logging.Config{Output: tt.output}}
			if got := logOutputPath(cfg); got != tt.want {
				t.Errorf("logOutputPath() = %q, want %q", got, tt.want)
			}
		})
	}

	if got := logOutputPath(&config.Config{}); got != "" {
		t.Errorf("nil logging: got %q, want empty", got)
	}
}

func TestFollowLogFileRequiresFile(t *testing.T) {
	cfg := &config.Config{Logging: &logging.Config{Output: "stdout"}}
	err := followLogFile(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when output is not a file")
	}
}

func TestTailFileStreamsAppendedData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pilot.log")

	// Seed with content that should NOT be streamed (tail starts at EOF).
	if err := os.WriteFile(path, []byte("old line\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = tailFile(ctx, path, &lockedWriter{mu: &mu, w: &buf}, 10*time.Millisecond)
	}()

	// Append new content after the tail has positioned at EOF.
	time.Sleep(30 * time.Millisecond)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("new line\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// Wait for the tailer to pick it up.
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		got := buf.String()
		mu.Unlock()
		if got == "new line\n" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("did not stream appended content, got %q", got)
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	wg.Wait()
}

func TestTailFileExitsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pilot.log")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- tailFile(ctx, path, &bytes.Buffer{}, 10*time.Millisecond)
	}()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil on cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tailFile did not exit after context cancel")
	}
}

// lockedWriter guards a writer with a mutex for concurrent test reads.
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
