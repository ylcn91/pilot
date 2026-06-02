package tunnel

import (
	"log/slog"
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("default config should not be enabled")
	}

	if cfg.Provider != "cloudflare" {
		t.Errorf("default provider = %q, want %q", cfg.Provider, "cloudflare")
	}

	if cfg.Port != 9090 {
		t.Errorf("default port = %d, want %d", cfg.Port, 9090)
	}
}

func TestConfigFields(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "all fields set",
			config: Config{
				Enabled:  true,
				Provider: "cloudflare",
				Domain:   "example.com",
				Port:     8080,
			},
		},
		{
			name: "minimal config",
			config: Config{
				Provider: "ngrok",
			},
		},
		{
			name:   "zero value config",
			config: Config{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify config can be used
			cfg := tt.config
			_ = cfg.Enabled
			_ = cfg.Provider
			_ = cfg.Domain
			_ = cfg.Port
		})
	}
}

func TestStatusStruct(t *testing.T) {
	status := &Status{
		Running:   true,
		Provider:  "cloudflare",
		URL:       "https://example.cfargotunnel.com",
		TunnelID:  "abc-123",
		Connected: true,
		Error:     "",
	}

	if !status.Running {
		t.Error("expected Running to be true")
	}
	if status.Provider != "cloudflare" {
		t.Errorf("Provider = %q, want %q", status.Provider, "cloudflare")
	}
	if status.URL != "https://example.cfargotunnel.com" {
		t.Errorf("URL = %q, want %q", status.URL, "https://example.cfargotunnel.com")
	}
	if status.TunnelID != "abc-123" {
		t.Errorf("TunnelID = %q, want %q", status.TunnelID, "abc-123")
	}
	if !status.Connected {
		t.Error("expected Connected to be true")
	}
	if status.Error != "" {
		t.Errorf("Error = %q, want empty", status.Error)
	}
}

func TestEnvironmentVariables(t *testing.T) {
	// Test that we can read home directory for config paths
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get user home dir: %v", err)
	}
	if home == "" {
		t.Error("home directory is empty")
	}
}

func TestServiceStatusStruct(t *testing.T) {
	status := &ServiceStatus{
		Installed: true,
		Running:   true,
		PlistPath: "/path/to/plist",
	}

	if !status.Installed {
		t.Error("expected Installed to be true")
	}
	if !status.Running {
		t.Error("expected Running to be true")
	}
	if status.PlistPath != "/path/to/plist" {
		t.Errorf("PlistPath = %q, want %q", status.PlistPath, "/path/to/plist")
	}
}

func TestStatusStructWithError(t *testing.T) {
	status := &Status{
		Running:   false,
		Provider:  "cloudflare",
		URL:       "",
		Connected: false,
		Error:     "connection refused",
	}

	if status.Running {
		t.Error("expected Running to be false")
	}
	if status.Connected {
		t.Error("expected Connected to be false")
	}
	if status.Error != "connection refused" {
		t.Errorf("Error = %q, want %q", status.Error, "connection refused")
	}
}

func TestConfigDomainUsage(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		wantHost string
	}{
		{
			name:     "with domain",
			config:   &Config{Domain: "my.example.com"},
			wantHost: "my.example.com",
		},
		{
			name:     "without domain",
			config:   &Config{Domain: ""},
			wantHost: "*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &CloudflareProvider{
				config: tt.config,
				logger: slog.Default(),
			}
			got := p.getHostname()
			if got != tt.wantHost {
				t.Errorf("getHostname() = %q, want %q", got, tt.wantHost)
			}
		})
	}
}

func TestConfigPortUsage(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		wantPort int
	}{
		{
			name:     "custom port",
			port:     8080,
			wantPort: 8080,
		},
		{
			name:     "default port",
			port:     0,
			wantPort: defaultTunnelPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := tt.port
			if port == 0 {
				port = defaultTunnelPort
			}
			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
		})
	}
}

func TestStatusStructFields(t *testing.T) {
	// Test that all fields can be set and retrieved
	s := Status{
		Running:   true,
		Provider:  "test-provider",
		URL:       "https://example.com",
		TunnelID:  "tunnel-123",
		Connected: true,
		Error:     "no error",
	}

	if !s.Running {
		t.Error("Running not set")
	}
	if s.Provider != "test-provider" {
		t.Errorf("Provider = %q, want %q", s.Provider, "test-provider")
	}
	if s.URL != "https://example.com" {
		t.Errorf("URL = %q, want %q", s.URL, "https://example.com")
	}
	if s.TunnelID != "tunnel-123" {
		t.Errorf("TunnelID = %q, want %q", s.TunnelID, "tunnel-123")
	}
	if !s.Connected {
		t.Error("Connected not set")
	}
	if s.Error != "no error" {
		t.Errorf("Error = %q, want %q", s.Error, "no error")
	}
}
