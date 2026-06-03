package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// mockArchitectProvider is a test double for ArchitectProvider.
type mockArchitectProvider struct {
	findings []pilotapi.Finding
}

func (m *mockArchitectProvider) Findings() []pilotapi.Finding { return m.findings }

// decodeArchitectBody decodes the JSON envelope returned by the architect
// endpoint into its findings array and count, failing the test on any error.
func decodeArchitectBody(t *testing.T, w *httptest.ResponseRecorder) (map[string]interface{}, []interface{}) {
	t.Helper()
	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	findings, ok := response["findings"].([]interface{})
	if !ok {
		t.Fatalf("findings should be an array, got %T", response["findings"])
	}
	return response, findings
}

func TestHandleArchitectNoProvider(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/architect", nil)
	w := httptest.NewRecorder()

	server.handleArchitectFindings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", ct)
	}

	response, findings := decodeArchitectBody(t, w)
	if len(findings) != 0 {
		t.Errorf("Expected empty findings, got %d", len(findings))
	}
	if response["count"] != float64(0) {
		t.Errorf("Expected count=0, got %v", response["count"])
	}
}

// TestHandleArchitectNilFindingsSlice guards against the provider returning a
// nil slice: the JSON must still be an empty array, never null.
func TestHandleArchitectNilFindingsSlice(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	server.SetArchitectProvider(&mockArchitectProvider{findings: nil})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/architect", nil)
	w := httptest.NewRecorder()

	server.handleArchitectFindings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if want := `"findings":[]`; !contains(body, want) {
		t.Errorf("Expected findings to serialize as empty array, body=%s", body)
	}

	response, findings := decodeArchitectBody(t, w)
	if len(findings) != 0 {
		t.Errorf("Expected 0 findings, got %d", len(findings))
	}
	if response["count"] != float64(0) {
		t.Errorf("Expected count=0, got %v", response["count"])
	}
}

func TestHandleArchitectEmptyFindings(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	server.SetArchitectProvider(&mockArchitectProvider{findings: []pilotapi.Finding{}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/architect", nil)
	w := httptest.NewRecorder()

	server.handleArchitectFindings(w, req)

	response, findings := decodeArchitectBody(t, w)
	if len(findings) != 0 {
		t.Errorf("Expected 0 findings, got %d", len(findings))
	}
	if response["count"] != float64(0) {
		t.Errorf("Expected count=0, got %v", response["count"])
	}
}

func TestHandleArchitectWithProvider(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	server.SetArchitectProvider(&mockArchitectProvider{findings: []pilotapi.Finding{
		{
			Title:             "Race in queue drain",
			Kind:              "bug",
			Risk:              pilotapi.RiskReleaseBlocker,
			WhyItMatters:      "drops tasks under load",
			SuggestedPRPieces: []string{"add mutex", "regression test"},
			TestPlan:          "stress test 1k concurrent enqueues",
			Files:             []string{"internal/queue/drain.go"},
		},
		{
			Title:        "Outdated dependency",
			Kind:         "proposal",
			Risk:         pilotapi.RiskLow,
			WhyItMatters: "minor security patch available",
		},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/architect", nil)
	w := httptest.NewRecorder()

	server.handleArchitectFindings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", ct)
	}

	response, findings := decodeArchitectBody(t, w)
	if response["count"] != float64(2) {
		t.Errorf("Expected count=2, got %v", response["count"])
	}
	if len(findings) != 2 {
		t.Fatalf("Expected 2 findings, got %d", len(findings))
	}

	first := findings[0].(map[string]interface{})
	// Verify the JSON keys mirror the pilotapi.Finding struct tags exactly.
	if first["title"] != "Race in queue drain" {
		t.Errorf("Expected title, got %v", first["title"])
	}
	if first["kind"] != "bug" {
		t.Errorf("Expected kind=bug, got %v", first["kind"])
	}
	if first["risk"] != "release-blocker" {
		t.Errorf("Expected risk=release-blocker, got %v", first["risk"])
	}
	if first["why_it_matters"] != "drops tasks under load" {
		t.Errorf("Expected why_it_matters, got %v", first["why_it_matters"])
	}
	if first["test_plan"] != "stress test 1k concurrent enqueues" {
		t.Errorf("Expected test_plan, got %v", first["test_plan"])
	}

	pieces, ok := first["suggested_pr_pieces"].([]interface{})
	if !ok || len(pieces) != 2 {
		t.Fatalf("Expected 2 suggested_pr_pieces, got %v", first["suggested_pr_pieces"])
	}
	if pieces[0] != "add mutex" || pieces[1] != "regression test" {
		t.Errorf("suggested_pr_pieces mismatch: %v", pieces)
	}

	files, ok := first["files"].([]interface{})
	if !ok || len(files) != 1 {
		t.Fatalf("Expected 1 file, got %v", first["files"])
	}
	if files[0] != "internal/queue/drain.go" {
		t.Errorf("Expected file path, got %v", files[0])
	}

	second := findings[1].(map[string]interface{})
	if second["risk"] != "low" {
		t.Errorf("Expected risk=low, got %v", second["risk"])
	}
	// Empty slice fields serialize as null when unset (Go zero value for a
	// nil slice). Assert the keys are present so the wire shape is stable.
	if _, present := second["suggested_pr_pieces"]; !present {
		t.Error("Expected suggested_pr_pieces key present even when empty")
	}
}

// TestHandleArchitectAllRiskLevels confirms each canonical RiskLevel
// round-trips to its lowercase JSON string.
func TestHandleArchitectAllRiskLevels(t *testing.T) {
	cases := []struct {
		risk pilotapi.RiskLevel
		want string
	}{
		{pilotapi.RiskLow, "low"},
		{pilotapi.RiskMedium, "medium"},
		{pilotapi.RiskHigh, "high"},
		{pilotapi.RiskReleaseBlocker, "release-blocker"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
			server.SetArchitectProvider(&mockArchitectProvider{findings: []pilotapi.Finding{
				{Title: "x", Kind: "k", Risk: tc.risk},
			}})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/architect", nil)
			w := httptest.NewRecorder()
			server.handleArchitectFindings(w, req)

			_, findings := decodeArchitectBody(t, w)
			if len(findings) != 1 {
				t.Fatalf("Expected 1 finding, got %d", len(findings))
			}
			got := findings[0].(map[string]interface{})["risk"]
			if got != tc.want {
				t.Errorf("Expected risk=%q, got %v", tc.want, got)
			}
		})
	}
}

func TestSetArchitectProvider(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})

	if server.architectProvider != nil {
		t.Error("Expected nil architect provider initially")
	}

	provider := &mockArchitectProvider{findings: []pilotapi.Finding{{Title: "t"}}}
	server.SetArchitectProvider(provider)

	if server.architectProvider == nil {
		t.Error("Expected architect provider to be set")
	}
}

// TestArchitectEndpointRequiresAuth verifies the route is auth-gated like the
// other /api/v1 endpoints: 401 without a token, 200 with a valid one.
func TestArchitectEndpointRequiresAuth(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 19094}
	authConfig := &AuthConfig{
		Type:  AuthTypeAPIToken,
		Token: "secret-api-token",
	}
	server := NewServerWithAuth(config, authConfig)
	server.SetArchitectProvider(&mockArchitectProvider{findings: []pilotapi.Finding{
		{Title: "Race in queue drain", Kind: "bug", Risk: pilotapi.RiskHigh},
	}})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = server.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := "http://127.0.0.1:19094"

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{"architect without auth returns 401", "", http.StatusUnauthorized},
		{"architect with valid auth returns 200", "Bearer secret-api-token", http.StatusOK},
		{"architect with invalid auth returns 401", "Bearer wrong-token", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/architect", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Status = %d, want %d", resp.StatusCode, tt.expectedStatus)
			}

			if resp.StatusCode == http.StatusOK {
				var body map[string]interface{}
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("Failed to decode authed body: %v", err)
				}
				if body["count"] != float64(1) {
					t.Errorf("Expected count=1 through authed route, got %v", body["count"])
				}
			}
		})
	}
}

// TestArchitectEndpointE2E exercises the full web data path: a real
// httptest.NewServer wrapping the production buildHandler() routing, a fake
// ArchitectProvider seeded with two findings, and an HTTP GET to
// /api/v1/architect. It asserts the decoded JSON body carries BOTH finding
// titles and their risk values, proving findings flow end-to-end from the
// provider through the gateway mux to the wire — not just through a directly
// invoked handler.
func TestArchitectEndpointE2E(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 0})
	server.SetArchitectProvider(&mockArchitectProvider{findings: []pilotapi.Finding{
		{
			Title:        "Unbounded goroutine leak in poller",
			Kind:         "bug",
			Risk:         pilotapi.RiskReleaseBlocker,
			WhyItMatters: "exhausts file descriptors after hours",
			Files:        []string{"internal/poller/loop.go"},
		},
		{
			Title:        "Vendor SDK two majors behind",
			Kind:         "proposal",
			Risk:         pilotapi.RiskMedium,
			WhyItMatters: "missing upstream security fixes",
		},
	}})

	ts := httptest.NewServer(server.buildHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/architect")
	if err != nil {
		t.Fatalf("GET /api/v1/architect failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", ct)
	}

	var body struct {
		Count    int                `json:"count"`
		Findings []pilotapi.Finding `json:"findings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode end-to-end body: %v", err)
	}

	if body.Count != 2 {
		t.Errorf("Expected count=2, got %d", body.Count)
	}
	if len(body.Findings) != 2 {
		t.Fatalf("Expected 2 findings over the wire, got %d", len(body.Findings))
	}

	// Both titles must survive the round-trip, in order.
	if body.Findings[0].Title != "Unbounded goroutine leak in poller" {
		t.Errorf("first title mismatch: %q", body.Findings[0].Title)
	}
	if body.Findings[1].Title != "Vendor SDK two majors behind" {
		t.Errorf("second title mismatch: %q", body.Findings[1].Title)
	}

	// Both risk values must round-trip to their canonical strings.
	if body.Findings[0].Risk != pilotapi.RiskReleaseBlocker {
		t.Errorf("first risk mismatch: %q", body.Findings[0].Risk)
	}
	if body.Findings[1].Risk != pilotapi.RiskMedium {
		t.Errorf("second risk mismatch: %q", body.Findings[1].Risk)
	}
}

// contains is a tiny substring helper to avoid pulling strings into a test
// that otherwise has no use for it.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
