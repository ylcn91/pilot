package tunnel

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		wantErr  bool
		wantProv string
	}{
		{
			name:     "nil config uses defaults",
			config:   nil,
			wantErr:  false,
			wantProv: "cloudflare",
		},
		{
			name: "cloudflare provider",
			config: &Config{
				Provider: "cloudflare",
				Port:     9090,
			},
			wantErr:  false,
			wantProv: "cloudflare",
		},
		{
			name: "ngrok provider",
			config: &Config{
				Provider: "ngrok",
				Port:     9090,
			},
			wantErr:  false,
			wantProv: "ngrok",
		},
		{
			name: "manual mode has no provider",
			config: &Config{
				Provider: "manual",
			},
			wantErr:  false,
			wantProv: "manual",
		},
		{
			name: "empty provider defaults to manual",
			config: &Config{
				Provider: "",
			},
			wantErr:  false,
			wantProv: "manual",
		},
		{
			name: "unknown provider fails",
			config: &Config{
				Provider: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewManager(tt.config, slog.Default())

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if m == nil {
				t.Errorf("expected manager, got nil")
				return
			}

			if m.Provider() != tt.wantProv {
				t.Errorf("provider = %q, want %q", m.Provider(), tt.wantProv)
			}
		})
	}
}

func TestNewManagerWithNilLogger(t *testing.T) {
	m, err := NewManager(&Config{Provider: "manual"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected manager, got nil")
	}
	if m.logger == nil {
		t.Error("expected logger to be set to default")
	}
}

func TestManagerStatus(t *testing.T) {
	// Test with manual mode (no provider)
	cfg := &Config{Provider: "manual"}
	m, err := NewManager(cfg, slog.Default())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if status.Running {
		t.Error("expected not running for manual mode")
	}

	if status.Provider != "manual" {
		t.Errorf("status provider = %q, want %q", status.Provider, "manual")
	}
}

func TestManagerStatusWithProvider(t *testing.T) {
	mock := &mockProvider{
		name: "test",
		statusResp: &Status{
			Running:   true,
			Provider:  "test",
			URL:       "https://test.example.com",
			TunnelID:  "abc123",
			Connected: true,
		},
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.Running {
		t.Error("expected running to be true")
	}
	if status.URL != "https://test.example.com" {
		t.Errorf("URL = %q, want %q", status.URL, "https://test.example.com")
	}
	if status.TunnelID != "abc123" {
		t.Errorf("TunnelID = %q, want %q", status.TunnelID, "abc123")
	}
}

func TestManagerURLEmpty(t *testing.T) {
	cfg := &Config{Provider: "manual"}
	m, err := NewManager(cfg, slog.Default())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	if url := m.URL(); url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}

func TestManagerIsRunning(t *testing.T) {
	cfg := &Config{Provider: "manual"}
	m, err := NewManager(cfg, slog.Default())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	if m.IsRunning() {
		t.Error("expected not running initially")
	}
}

func TestManagerSetupNoProvider(t *testing.T) {
	m := &Manager{
		config: &Config{Provider: "manual"},
		logger: slog.Default(),
	}

	ctx := context.Background()
	err := m.Setup(ctx)
	if err == nil {
		t.Error("expected error for no provider")
	}
	if err.Error() != "no tunnel provider configured" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestManagerSetupProviderNotInstalled(t *testing.T) {
	mock := &mockProvider{
		name:      "test",
		installed: false,
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	err := m.Setup(ctx)
	if err == nil {
		t.Error("expected error for provider not installed")
	}
	if err.Error() != "test CLI is not installed" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestManagerSetupSuccess(t *testing.T) {
	mock := &mockProvider{
		name:      "test",
		installed: true,
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	err := m.Setup(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.setupCalled {
		t.Error("expected Setup to be called on provider")
	}
}

func TestManagerSetupProviderError(t *testing.T) {
	mock := &mockProvider{
		name:      "test",
		installed: true,
		setupErr:  errors.New("setup failed"),
	}

	m := &Manager{
		config:   &Config{Provider: "test"},
		provider: mock,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	err := m.Setup(ctx)
	if err == nil {
		t.Error("expected error")
	}
	if !errors.Is(err, mock.setupErr) && err.Error() != "tunnel setup failed: setup failed" {
		t.Errorf("unexpected error: %v", err)
	}
}
