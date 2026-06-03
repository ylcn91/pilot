package tunnel

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestScanStderrForURLConnectionRegistered(t *testing.T) {
	stderr := strings.NewReader(strings.Join([]string{
		"2024-01-01 starting cloudflared",
		"INF Connection abc123 registered connIndex=0",
		"INF this line is never read",
	}, "\n"))

	urlChan := make(chan string, 1)
	errChan := make(chan error, 1)

	scanStderrForURL(stderr, discardLogger(), func() string {
		return "https://tunnel.example.com"
	}, urlChan, errChan)

	select {
	case url := <-urlChan:
		if url != "https://tunnel.example.com" {
			t.Fatalf("url = %q, want https://tunnel.example.com", url)
		}
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	default:
		t.Fatal("expected a URL to be sent on urlChan")
	}
}

func TestScanStderrForURLError(t *testing.T) {
	stderr := strings.NewReader(strings.Join([]string{
		"INF starting up",
		"ERR failed to connect to edge",
	}, "\n"))

	urlChan := make(chan string, 1)
	errChan := make(chan error, 1)

	scanStderrForURL(stderr, discardLogger(), func() string {
		t.Fatal("urlFn must not be called on the error path")
		return ""
	}, urlChan, errChan)

	select {
	case err := <-errChan:
		if !strings.Contains(err.Error(), "failed to connect to edge") {
			t.Fatalf("error = %v, want it to wrap the offending line", err)
		}
	case url := <-urlChan:
		t.Fatalf("unexpected url: %q", url)
	default:
		t.Fatal("expected an error to be sent on errChan")
	}
}

func TestScanStderrForURLNoMatchReturns(t *testing.T) {
	stderr := strings.NewReader(strings.Join([]string{
		"INF starting up",
		"INF nothing interesting here",
	}, "\n"))

	urlChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Must return (not block) when the reader is exhausted without a match;
	// Start() relies on this so its timeout / ctx.Done arms can fire.
	scanStderrForURL(stderr, discardLogger(), func() string {
		t.Fatal("urlFn must not be called when no connection line appears")
		return ""
	}, urlChan, errChan)

	select {
	case url := <-urlChan:
		t.Fatalf("unexpected url: %q", url)
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	default:
	}
}
