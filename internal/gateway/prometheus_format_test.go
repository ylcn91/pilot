package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/autopilot"
)

func TestEscapeLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with\\backslash", "with\\\\backslash"},
		{`with"quote`, `with\"quote`},
		{"with\nnewline", "with\\nnewline"},
		{`complex\n"test`, `complex\\n\"test`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeLabel(tt.input)
			if got != tt.expected {
				t.Errorf("escapeLabel(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatLabels(t *testing.T) {
	tests := []struct {
		name     string
		pairs    []string
		expected string
	}{
		{"empty", []string{}, ""},
		{"single", []string{"key", "value"}, `key="value"`},
		{"multiple", []string{"a", "1", "b", "2"}, `a="1",b="2"`},
		{"with special chars", []string{"key", `val"ue`}, `key="val\"ue"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLabels(tt.pairs)
			if got != tt.expected {
				t.Errorf("formatLabels(%v) = %q, want %q", tt.pairs, got, tt.expected)
			}
		})
	}
}

// TestMetricsEndpointWiring verifies that /metrics returns Prometheus text when
// SetMetricsSource is called, and the fallback response when it is not.
func TestMetricsEndpointWiring(t *testing.T) {
	t.Run("with metrics source returns 200 and pilot counters", func(t *testing.T) {
		srv := NewServer(&Config{Host: "127.0.0.1", Port: 0})
		m := autopilot.NewMetrics()
		m.RecordIssueProcessed("success")
		srv.SetMetricsSource(m)

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		w := httptest.NewRecorder()
		srv.handleMetrics(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body := w.Body.String()
		for _, want := range []string{
			"pilot_issues_processed_total",
			"pilot_prs_merged_total",
			"pilot_prs_failed_total",
			"pilot_prs_conflicting_total",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q", want)
			}
		}
	})

	t.Run("without metrics source returns 503 fallback", func(t *testing.T) {
		srv := NewServer(&Config{Host: "127.0.0.1", Port: 0})

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		w := httptest.NewRecorder()
		srv.handleMetrics(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", resp.StatusCode)
		}
		if !strings.Contains(w.Body.String(), "Metrics not configured") {
			t.Errorf("expected 'Metrics not configured' in body, got: %s", w.Body.String())
		}
	})
}
