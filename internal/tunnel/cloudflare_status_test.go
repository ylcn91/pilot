package tunnel

import (
	"context"
	"log/slog"
	"testing"
)

func TestCloudflareProviderStatusWithRunningProcess(t *testing.T) {
	// Create a provider with url set (simulating running state)
	p := &CloudflareProvider{
		config:   &Config{Domain: "test.example.com"},
		tunnelID: "abc-123",
		url:      "https://test.example.com",
		logger:   slog.Default(),
	}

	ctx := context.Background()
	status, err := p.Status(ctx)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if status.Provider != "cloudflare" {
		t.Errorf("Provider = %q, want %q", status.Provider, "cloudflare")
	}
	if status.TunnelID != "abc-123" {
		t.Errorf("TunnelID = %q, want %q", status.TunnelID, "abc-123")
	}
	if status.URL != "https://test.example.com" {
		t.Errorf("URL = %q, want %q", status.URL, "https://test.example.com")
	}
}

func TestCloudflareProviderDetermineURLVariations(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		tunnelID string
		want     string
	}{
		{
			name:     "custom domain takes precedence",
			domain:   "custom.example.com",
			tunnelID: "should-be-ignored",
			want:     "https://custom.example.com",
		},
		{
			name:     "tunnel ID generates cfargotunnel URL",
			domain:   "",
			tunnelID: "my-tunnel-uuid",
			want:     "https://my-tunnel-uuid.cfargotunnel.com",
		},
		{
			name:     "neither domain nor tunnel ID",
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

func TestCloudflareProviderGetHostnameVariations(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		want   string
	}{
		{
			name:   "with custom domain",
			domain: "webhook.mycompany.com",
			want:   "webhook.mycompany.com",
		},
		{
			name:   "empty domain returns wildcard",
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

func TestCloudflareProviderName2(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())
	if got := p.Name(); got != "cloudflare" {
		t.Errorf("Name() = %q, want %q", got, "cloudflare")
	}
}

func TestCloudflareProviderGetTunnelIDEmpty(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())
	if got := p.GetTunnelID(); got != "" {
		t.Errorf("GetTunnelID() = %q, want empty", got)
	}
}

func TestCloudflareProviderGetTunnelIDSet(t *testing.T) {
	p := &CloudflareProvider{
		config:   &Config{},
		tunnelID: "my-tunnel-123",
		logger:   slog.Default(),
	}
	if got := p.GetTunnelID(); got != "my-tunnel-123" {
		t.Errorf("GetTunnelID() = %q, want %q", got, "my-tunnel-123")
	}
}

func TestCloudflareProviderStatusBranches(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		tunnelID string
	}{
		{
			name:     "no url set",
			url:      "",
			tunnelID: "test-id",
		},
		{
			name:     "with url set",
			url:      "https://test.example.com",
			tunnelID: "test-id",
		},
		{
			name:     "no tunnel id",
			url:      "",
			tunnelID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &CloudflareProvider{
				config:   &Config{},
				tunnelID: tt.tunnelID,
				url:      tt.url,
				logger:   slog.Default(),
			}

			ctx := context.Background()
			status, err := p.Status(ctx)
			if err != nil {
				t.Fatalf("Status failed: %v", err)
			}

			if status.Provider != "cloudflare" {
				t.Errorf("Provider = %q, want %q", status.Provider, "cloudflare")
			}
		})
	}
}

func TestCloudflareProviderStatusExternalProcess(t *testing.T) {
	// Test Status when no internal process but might have external
	p := &CloudflareProvider{
		config:   &Config{Domain: "external.example.com"},
		tunnelID: "external-tunnel",
		cmd:      nil, // No internal process
		logger:   slog.Default(),
	}

	ctx := context.Background()
	status, err := p.Status(ctx)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	// Should check for external process
	if status.Provider != "cloudflare" {
		t.Errorf("Provider = %q, want cloudflare", status.Provider)
	}
}
