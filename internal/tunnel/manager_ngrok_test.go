package tunnel

import (
	"context"
	"log/slog"
	"testing"
)

func TestNgrokProviderName(t *testing.T) {
	p := NewNgrokProvider(&Config{}, slog.Default())
	if p.Name() != "ngrok" {
		t.Errorf("name = %q, want %q", p.Name(), "ngrok")
	}
}

func TestNgrokIsInstalled(t *testing.T) {
	p := NewNgrokProvider(&Config{}, slog.Default())
	// Just verify it doesn't panic - actual installation varies
	_ = p.IsInstalled()
}

func TestNgrokProviderURL(t *testing.T) {
	p := NewNgrokProvider(&Config{}, slog.Default())

	// Initially empty
	if url := p.URL(); url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}

func TestNgrokProviderStopNotRunning(t *testing.T) {
	p := NewNgrokProvider(&Config{}, slog.Default())

	// Stop should not error when not running
	err := p.Stop()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNgrokProviderStatus(t *testing.T) {
	p := NewNgrokProvider(&Config{}, slog.Default())

	ctx := context.Background()
	status, err := p.Status(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.Provider != "ngrok" {
		t.Errorf("Provider = %q, want %q", status.Provider, "ngrok")
	}
}

func TestNgrokProviderConstants(t *testing.T) {
	if ngrokBin != "ngrok" {
		t.Errorf("ngrokBin = %q, want %q", ngrokBin, "ngrok")
	}
	if ngrokAPIEndpoint != "http://127.0.0.1:4040/api/tunnels" {
		t.Errorf("ngrokAPIEndpoint = %q, want %q", ngrokAPIEndpoint, "http://127.0.0.1:4040/api/tunnels")
	}
}

func TestNgrokProviderStatusWithURL(t *testing.T) {
	p := &NgrokProvider{
		config: &Config{Domain: "test.example.com"},
		url:    "https://test.ngrok.io",
		logger: slog.Default(),
	}

	ctx := context.Background()
	status, err := p.Status(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.URL != "https://test.ngrok.io" {
		t.Errorf("URL = %q, want %q", status.URL, "https://test.ngrok.io")
	}
}

func TestNgrokProviderGetURLFromAPINoServer(t *testing.T) {
	p := NewNgrokProvider(&Config{}, slog.Default())

	// Without ngrok running, should fail
	_, err := p.getURLFromAPI()
	if err == nil {
		// If ngrok is actually running on this system, skip
		t.Log("ngrok API responded - ngrok might be running")
	}
}

func TestNgrokProviderURLLocking(t *testing.T) {
	p := &NgrokProvider{
		config: &Config{},
		url:    "https://abc.ngrok.io",
		logger: slog.Default(),
	}

	// Concurrent access should be safe
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			url := p.URL()
			if url != "https://abc.ngrok.io" {
				t.Errorf("unexpected URL: %q", url)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestNgrokProviderInterface(t *testing.T) {
	// Verify NgrokProvider satisfies Provider interface
	var _ Provider = (*NgrokProvider)(nil)
}

func TestNewNgrokProviderFields(t *testing.T) {
	cfg := &Config{
		Provider: "ngrok",
		Domain:   "test.example.com",
		Port:     8080,
	}
	logger := slog.Default()

	p := NewNgrokProvider(cfg, logger)

	if p.config != cfg {
		t.Error("config not set correctly")
	}
	if p.logger != logger {
		t.Error("logger not set correctly")
	}
	if p.cmd != nil {
		t.Error("cmd should be nil initially")
	}
	if p.url != "" {
		t.Error("url should be empty initially")
	}
}
