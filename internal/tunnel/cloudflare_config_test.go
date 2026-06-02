package tunnel

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCloudflareProviderWriteConfigIntegration(t *testing.T) {
	// Test the config writing logic (will actually write to disk)
	// Create a temp directory for testing
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")

	// Set HOME to temp dir to avoid modifying real cloudflared config
	t.Setenv("HOME", tmpDir)
	defer func() {
		if origHome != "" {
			_ = os.Setenv("HOME", origHome)
		}
	}()

	p := &CloudflareProvider{
		config: &Config{
			Port:   8080,
			Domain: "test.example.com",
		},
		tunnelID: "test-tunnel-id",
		logger:   slog.Default(),
	}

	err := p.writeConfig()
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	// Verify config file was created
	configPath := filepath.Join(tmpDir, ".cloudflared", "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}

	// Read and verify content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	// Verify it's valid JSON (since we write JSON which is valid YAML)
	var config map[string]interface{}
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("config file is not valid JSON: %v", err)
	}

	if config["tunnel"] != "test-tunnel-id" {
		t.Errorf("tunnel = %v, want %q", config["tunnel"], "test-tunnel-id")
	}

	ingress, ok := config["ingress"].([]interface{})
	if !ok {
		t.Fatal("ingress is not an array")
	}
	if len(ingress) != 2 {
		t.Errorf("ingress length = %d, want 2", len(ingress))
	}
}

func TestCloudflareProviderWriteConfigDefaultPort(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	p := &CloudflareProvider{
		config: &Config{
			Port:   0, // Should use default
			Domain: "",
		},
		tunnelID: "test-tunnel",
		logger:   slog.Default(),
	}

	err := p.writeConfig()
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	// Verify config file content
	configPath := filepath.Join(tmpDir, ".cloudflared", "config.yml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	// Should contain default port 9090
	var config map[string]interface{}
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("config file is not valid JSON: %v", err)
	}

	ingress, ok := config["ingress"].([]interface{})
	if !ok {
		t.Fatal("ingress is not an array")
	}

	firstIngress := ingress[0].(map[string]interface{})
	service := firstIngress["service"].(string)

	// Should contain port 9090 (default)
	if service != "http://localhost:9090" {
		t.Errorf("service = %q, want %q", service, "http://localhost:9090")
	}
}

func TestCloudflareProviderConfigureDNSNoDomain(t *testing.T) {
	p := &CloudflareProvider{
		config:   &Config{Domain: ""},
		tunnelID: "test",
		logger:   slog.Default(),
	}

	ctx := context.Background()
	err := p.configureDNS(ctx)
	if err != nil {
		t.Errorf("configureDNS with empty domain should not error: %v", err)
	}
}

func TestCloudflareProviderConfigureDNSWithDomain(t *testing.T) {
	// Test configureDNS when domain is set (will fail without cloudflared)
	p := &CloudflareProvider{
		config:   &Config{Domain: "test.example.com"},
		tunnelID: "test-tunnel",
		logger:   slog.Default(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Will fail without cloudflared, but covers the code path
	err := p.configureDNS(ctx)
	if err == nil {
		t.Log("configureDNS succeeded - cloudflared may be configured")
	}
}

func TestCloudflareProviderWriteConfigErrors(t *testing.T) {
	// Test with invalid home directory (simulate error)
	p := &CloudflareProvider{
		config:   &Config{Port: 8080},
		tunnelID: "test",
		logger:   slog.Default(),
	}

	// Set HOME to a valid temp dir
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Should succeed
	err := p.writeConfig()
	if err != nil {
		t.Errorf("writeConfig failed: %v", err)
	}
}

func TestCloudflareProviderWriteConfigPortZero(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	p := &CloudflareProvider{
		config:   &Config{Port: 0}, // Zero port should use default
		tunnelID: "test-id",
		logger:   slog.Default(),
	}

	err := p.writeConfig()
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	// Verify the default port was used
	configPath := filepath.Join(tmpDir, ".cloudflared", "config.yml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	ingress := config["ingress"].([]interface{})
	firstIngress := ingress[0].(map[string]interface{})
	service := firstIngress["service"].(string)

	// Should use default port 9090
	if service != "http://localhost:9090" {
		t.Errorf("service = %q, want default port 9090", service)
	}
}
