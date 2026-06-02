package tunnel

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestCloudflareProviderName(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())
	if p.Name() != "cloudflare" {
		t.Errorf("name = %q, want %q", p.Name(), "cloudflare")
	}
}

func TestCloudflareIsInstalled(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())
	// Just verify it doesn't panic - actual installation varies
	_ = p.IsInstalled()
}

func TestCloudflareProviderURL(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())

	// Initially empty
	if url := p.URL(); url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}

func TestCloudflareProviderGetTunnelID(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())

	// Initially empty
	if id := p.GetTunnelID(); id != "" {
		t.Errorf("expected empty tunnel ID, got %q", id)
	}
}

func TestCloudflareProviderDetermineURL(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		tunnelID string
		want     string
	}{
		{
			name:     "with custom domain",
			domain:   "webhook.example.com",
			tunnelID: "abc123",
			want:     "https://webhook.example.com",
		},
		{
			name:     "with tunnel ID only",
			domain:   "",
			tunnelID: "abc123-def456",
			want:     "https://abc123-def456.cfargotunnel.com",
		},
		{
			name:     "no domain or tunnel ID",
			domain:   "",
			tunnelID: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &CloudflareProvider{
				config:   &Config{Domain: tt.domain},
				tunnelID: tt.tunnelID,
				logger:   slog.Default(),
			}
			got := p.determineURL()
			if got != tt.want {
				t.Errorf("determineURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCloudflareProviderGetHostname(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		want   string
	}{
		{
			name:   "with custom domain",
			domain: "webhook.example.com",
			want:   "webhook.example.com",
		},
		{
			name:   "without domain",
			domain: "",
			want:   "*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &CloudflareProvider{
				config: &Config{Domain: tt.domain},
				logger: slog.Default(),
			}
			got := p.getHostname()
			if got != tt.want {
				t.Errorf("getHostname() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCloudflareProviderStopNotRunning(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())

	// Stop should not error when not running
	err := p.Stop()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCloudflareProviderStatus(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())

	ctx := context.Background()
	status, err := p.Status(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.Provider != "cloudflare" {
		t.Errorf("Provider = %q, want %q", status.Provider, "cloudflare")
	}
}

func TestCloudflareProviderWriteConfigPortDefault(t *testing.T) {
	// Test writeConfig with default port (0)
	p := &CloudflareProvider{
		config:   &Config{Port: 0, Domain: ""},
		tunnelID: "test-tunnel-id",
		logger:   slog.Default(),
	}

	// We can't run writeConfig without side effects but we can test
	// the logic that would be used by checking the port default
	port := p.config.Port
	if port == 0 {
		port = defaultTunnelPort
	}
	if port != 9090 {
		t.Errorf("expected default port 9090, got %d", port)
	}
}

func TestCloudflareProviderConstants(t *testing.T) {
	// Verify constants are set correctly
	if cloudflaredBin != "cloudflared" {
		t.Errorf("cloudflaredBin = %q, want %q", cloudflaredBin, "cloudflared")
	}
	if tunnelName != "pilot-webhook" {
		t.Errorf("tunnelName = %q, want %q", tunnelName, "pilot-webhook")
	}
	if defaultTunnelPort != 9090 {
		t.Errorf("defaultTunnelPort = %d, want %d", defaultTunnelPort, 9090)
	}
	if connectionTimeout != 30*time.Second {
		t.Errorf("connectionTimeout = %v, want %v", connectionTimeout, 30*time.Second)
	}
}

func TestCloudflareProviderStatusWithURL(t *testing.T) {
	p := &CloudflareProvider{
		config:   &Config{Domain: "test.example.com"},
		tunnelID: "abc123",
		url:      "https://test.example.com",
		logger:   slog.Default(),
	}

	ctx := context.Background()
	status, err := p.Status(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.URL != "https://test.example.com" {
		t.Errorf("URL = %q, want %q", status.URL, "https://test.example.com")
	}
	if status.TunnelID != "abc123" {
		t.Errorf("TunnelID = %q, want %q", status.TunnelID, "abc123")
	}
}

func TestCloudflareProviderCheckExternalProcess(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())

	// Should not panic or return an unexpected error
	running, err := p.checkExternalProcess()
	// We can't predict the result, but verify it doesn't panic
	_ = running
	if err != nil {
		// Some errors are expected (e.g., pgrep not found on some systems)
		t.Logf("checkExternalProcess returned error: %v (may be expected)", err)
	}
}

func TestCloudflareProviderDetermineURLEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		tunnelID string
		want     string
	}{
		{
			name:     "domain with subdomain",
			domain:   "api.webhook.example.com",
			tunnelID: "abc",
			want:     "https://api.webhook.example.com",
		},
		{
			name:     "tunnel ID with uuid format",
			domain:   "",
			tunnelID: "550e8400-e29b-41d4-a716-446655440000",
			want:     "https://550e8400-e29b-41d4-a716-446655440000.cfargotunnel.com",
		},
		{
			name:     "empty config",
			domain:   "",
			tunnelID: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &CloudflareProvider{
				config:   &Config{Domain: tt.domain},
				tunnelID: tt.tunnelID,
				logger:   slog.Default(),
			}
			got := p.determineURL()
			if got != tt.want {
				t.Errorf("determineURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCloudflareProviderURLLocking(t *testing.T) {
	p := &CloudflareProvider{
		config: &Config{},
		url:    "https://test.example.com",
		logger: slog.Default(),
	}

	// Concurrent access should be safe
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			url := p.URL()
			if url != "https://test.example.com" {
				t.Errorf("unexpected URL: %q", url)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestCloudflareProviderGetTunnelIDLocking(t *testing.T) {
	p := &CloudflareProvider{
		config:   &Config{},
		tunnelID: "test-tunnel-123",
		logger:   slog.Default(),
	}

	// Concurrent access should be safe
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			id := p.GetTunnelID()
			if id != "test-tunnel-123" {
				t.Errorf("unexpected tunnel ID: %q", id)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestCloudflareProviderInterface(t *testing.T) {
	// Verify CloudflareProvider satisfies Provider interface
	var _ Provider = (*CloudflareProvider)(nil)
}

func TestNewCloudflareProviderFields(t *testing.T) {
	cfg := &Config{
		Provider: "cloudflare",
		Domain:   "test.example.com",
		Port:     8080,
	}
	logger := slog.Default()

	p := NewCloudflareProvider(cfg, logger)

	if p.config != cfg {
		t.Error("config not set correctly")
	}
	if p.logger != logger {
		t.Error("logger not set correctly")
	}
	if p.cmd != nil {
		t.Error("cmd should be nil initially")
	}
	if p.tunnelID != "" {
		t.Error("tunnelID should be empty initially")
	}
	if p.url != "" {
		t.Error("url should be empty initially")
	}
}
