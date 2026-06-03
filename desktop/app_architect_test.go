package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// architectMux builds an httptest mux that serves the given payload bytes at
// /api/v1/architect with the supplied status code.
func architectMux(status int, body []byte) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/architect", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_, _ = w.Write(body)
		}
	})
	return mux
}

func TestGetArchitectFindings_HappyPath(t *testing.T) {
	payload := map[string]interface{}{
		"findings": []map[string]interface{}{
			{
				"title":               "Unbounded goroutine spawn",
				"kind":                "bug",
				"risk":                "high",
				"why_it_matters":      "Leaks under load.",
				"suggested_pr_pieces": []string{"add worker pool", "cap concurrency"},
				"test_plan":           "race test with -race",
				"files":               []string{"internal/gateway/server.go"},
			},
			{
				"title":          "Missing index",
				"kind":           "perf",
				"risk":           "release-blocker",
				"why_it_matters": "Full table scan.",
				"files":          []string{"internal/memory/store.go"},
			},
		},
		"count": 2,
	}
	body, _ := json.Marshal(payload)
	srv := httptest.NewServer(architectMux(http.StatusOK, body))
	defer srv.Close()

	app := &App{gatewayURL: srv.URL}
	findings := app.GetArchitectFindings()

	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	f := findings[0]
	if f.Title != "Unbounded goroutine spawn" {
		t.Errorf("title mismatch: %q", f.Title)
	}
	if f.Kind != "bug" {
		t.Errorf("kind mismatch: %q", f.Kind)
	}
	if f.Risk != "high" {
		t.Errorf("risk mismatch: %q", f.Risk)
	}
	if f.WhyItMatters != "Leaks under load." {
		t.Errorf("whyItMatters mismatch: %q", f.WhyItMatters)
	}
	if len(f.SuggestedPRPieces) != 2 || f.SuggestedPRPieces[0] != "add worker pool" {
		t.Errorf("suggestedPRPieces mismatch: %#v", f.SuggestedPRPieces)
	}
	if f.TestPlan != "race test with -race" {
		t.Errorf("testPlan mismatch: %q", f.TestPlan)
	}
	if len(f.Files) != 1 || f.Files[0] != "internal/gateway/server.go" {
		t.Errorf("files mismatch: %#v", f.Files)
	}
	if findings[1].Risk != "release-blocker" {
		t.Errorf("second finding risk mismatch: %q", findings[1].Risk)
	}
}

func TestGetArchitectFindings_EmptyList(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{"findings": []Finding{}, "count": 0})
	srv := httptest.NewServer(architectMux(http.StatusOK, body))
	defer srv.Close()

	app := &App{gatewayURL: srv.URL}
	findings := app.GetArchitectFindings()
	if findings == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestGetArchitectFindings_NullFindings(t *testing.T) {
	// Defensive: a payload with explicit null should still yield an empty slice.
	srv := httptest.NewServer(architectMux(http.StatusOK, []byte(`{"findings":null,"count":0}`)))
	defer srv.Close()

	app := &App{gatewayURL: srv.URL}
	findings := app.GetArchitectFindings()
	if findings == nil {
		t.Fatal("expected non-nil empty slice for null findings")
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestGetArchitectFindings_DaemonUnreachable(t *testing.T) {
	app := &App{gatewayURL: "http://127.0.0.1:1"} // nothing listening
	findings := app.GetArchitectFindings()
	if findings == nil {
		t.Fatal("expected non-nil empty slice when daemon unreachable")
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestGetArchitectFindings_Non200(t *testing.T) {
	srv := httptest.NewServer(architectMux(http.StatusInternalServerError, []byte(`{"findings":[],"count":0}`)))
	defer srv.Close()

	app := &App{gatewayURL: srv.URL}
	findings := app.GetArchitectFindings()
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings on 500, got %d", len(findings))
	}
}

func TestGetArchitectFindings_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(architectMux(http.StatusOK, []byte(`{not json`)))
	defer srv.Close()

	app := &App{gatewayURL: srv.URL}
	findings := app.GetArchitectFindings()
	if findings == nil {
		t.Fatal("expected non-nil empty slice on malformed JSON")
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings on malformed JSON, got %d", len(findings))
	}
}

func TestGetArchitectFindings_EmptyGatewayURL(t *testing.T) {
	app := &App{gatewayURL: ""}
	findings := app.GetArchitectFindings()
	if findings == nil {
		t.Fatal("expected non-nil empty slice for empty gatewayURL")
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for empty gatewayURL, got %d", len(findings))
	}
}

func TestGetArchitectFindings_SlowServerTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/architect", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second) // exceeds the 2s client timeout
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"findings":[],"count":0}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	app := &App{gatewayURL: srv.URL}
	findings := app.GetArchitectFindings()
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings on timeout, got %d", len(findings))
	}
}
