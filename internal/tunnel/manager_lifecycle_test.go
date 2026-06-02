package tunnel

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func TestManagerStartNoProvider(t *testing.T) {
	m := &Manager{
		config: &Config{Provider: "manual"},
		logger: slog.Default(),
	}

	ctx := context.Background()
	_, err := m.Start(ctx)
	if err == nil {
		t.Error("expected error for no provider")
	}
	if err.Error() != "no tunnel provider configured" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestManagerStartSuccess(t *testing.T) {
	mock := &mockProvider{
		name:     "test",
		startURL: "https://test.example.com",
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	url, err := m.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if url != "https://test.example.com" {
		t.Errorf("URL = %q, want %q", url, "https://test.example.com")
	}
	if !mock.startCalled {
		t.Error("expected Start to be called on provider")
	}
	if !m.running {
		t.Error("expected running to be true")
	}
	if m.url != url {
		t.Errorf("m.url = %q, want %q", m.url, url)
	}
}

func TestManagerStartAlreadyRunning(t *testing.T) {
	mock := &mockProvider{
		name:     "test",
		startURL: "https://test.example.com",
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
		running:  true,
		url:      "https://existing.example.com",
	}

	ctx := context.Background()
	url, err := m.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if url != "https://existing.example.com" {
		t.Errorf("URL = %q, want %q", url, "https://existing.example.com")
	}
	if mock.startCalled {
		t.Error("expected Start NOT to be called when already running")
	}
}

func TestManagerStartError(t *testing.T) {
	mock := &mockProvider{
		name:     "test",
		startErr: errors.New("connection refused"),
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	_, err := m.Start(ctx)
	if err == nil {
		t.Error("expected error")
	}
	if m.running {
		t.Error("expected running to be false after error")
	}
}

func TestManagerStopNoProvider(t *testing.T) {
	m := &Manager{
		config: &Config{Provider: "manual"},
		logger: slog.Default(),
	}

	err := m.Stop()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestManagerStopNotRunning(t *testing.T) {
	mock := &mockProvider{
		name: "test",
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
		running:  false,
	}

	err := m.Stop()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if mock.stopCalled {
		t.Error("expected Stop NOT to be called when not running")
	}
}

func TestManagerStopSuccess(t *testing.T) {
	mock := &mockProvider{
		name: "test",
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
		running:  true,
		url:      "https://test.example.com",
	}

	err := m.Stop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mock.stopCalled {
		t.Error("expected Stop to be called on provider")
	}
	if m.running {
		t.Error("expected running to be false")
	}
	if m.url != "" {
		t.Errorf("expected url to be empty, got %q", m.url)
	}
}

func TestManagerStopError(t *testing.T) {
	mock := &mockProvider{
		name:    "test",
		stopErr: errors.New("process not found"),
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
		running:  true,
	}

	err := m.Stop()
	if err == nil {
		t.Error("expected error")
	}
}

func TestManagerProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     string
	}{
		{
			name:     "nil provider returns manual",
			provider: nil,
			want:     "manual",
		},
		{
			name:     "mock provider",
			provider: &mockProvider{name: "test"},
			want:     "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Manager{
				provider: tt.provider,
			}
			if got := m.Provider(); got != tt.want {
				t.Errorf("Provider() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestManagerConcurrency(t *testing.T) {
	m, err := NewManager(&Config{Provider: "manual"}, slog.Default())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Run concurrent operations
	done := make(chan bool, 10)

	for i := 0; i < 5; i++ {
		go func() {
			_ = m.URL()
			_ = m.IsRunning()
			_ = m.Provider()
			done <- true
		}()
	}

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		go func() {
			_, _ = m.Status(ctx)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent operations")
		}
	}
}

func TestManagerURLConcurrency(t *testing.T) {
	m := &Manager{
		config: &Config{Provider: "manual"},
		logger: slog.Default(),
		url:    "https://test.example.com",
	}

	done := make(chan bool, 20)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			url := m.URL()
			if url != "https://test.example.com" && url != "" {
				t.Errorf("unexpected URL: %q", url)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestManagerIsRunningConcurrency(t *testing.T) {
	m := &Manager{
		config:  &Config{Provider: "manual"},
		logger:  slog.Default(),
		running: true,
	}

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_ = m.IsRunning()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestManagerSetupWithCancelledContext(t *testing.T) {
	mock := &mockProvider{
		name:      "test",
		installed: true,
		setupErr:  context.Canceled,
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Setup(ctx)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestManagerStartWithCancelledContext(t *testing.T) {
	mock := &mockProvider{
		name:     "test",
		startErr: context.Canceled,
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Start(ctx)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestMockProviderInterface(t *testing.T) {
	// Verify mockProvider satisfies Provider interface
	var _ Provider = (*mockProvider)(nil)
}
