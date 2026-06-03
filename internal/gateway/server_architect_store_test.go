package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// architect.FindingsStore must satisfy the gateway's ArchitectProvider contract
// by shape alone (the radar lens writes findings into the store; the gateway
// reads them). This compile-time assertion fails the build if either side's
// Findings signature drifts.
var _ ArchitectProvider = (*architect.FindingsStore)(nil)

// TestArchitectStoreSatisfiesProviderE2E injects a real *architect.FindingsStore
// (not the local mock) through SetArchitectProvider and proves the dashboard
// sink serves whatever the store currently holds, including live updates: the
// same store reflects a later Set on the next request.
func TestArchitectStoreSatisfiesProviderE2E(t *testing.T) {
	store := architect.NewFindingsStore()
	store.Set([]pilotapi.Finding{
		{Title: "Import cycle in adapters", Kind: "refactor", Risk: pilotapi.RiskHigh},
	})

	server := NewServer(&Config{Host: "127.0.0.1", Port: 0})
	server.SetArchitectProvider(store)

	ts := httptest.NewServer(server.buildHandler())
	defer ts.Close()

	get := func() (int, []pilotapi.Finding) {
		t.Helper()
		resp, err := http.Get(ts.URL + "/api/v1/architect")
		if err != nil {
			t.Fatalf("GET architect: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			Count    int                `json:"count"`
			Findings []pilotapi.Finding `json:"findings"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.Count, body.Findings
	}

	count, findings := get()
	if count != 1 || len(findings) != 1 || findings[0].Title != "Import cycle in adapters" {
		t.Fatalf("first read mismatch: count=%d findings=%+v", count, findings)
	}

	// A later radar scan refreshes the same store; the endpoint must reflect it
	// without re-injection.
	store.Set([]pilotapi.Finding{
		{Title: "Stale test for handler", Kind: "test-gap", Risk: pilotapi.RiskMedium},
		{Title: "Oversized file", Kind: "refactor", Risk: pilotapi.RiskLow},
	})

	count, findings = get()
	if count != 2 || len(findings) != 2 {
		t.Fatalf("live-update read mismatch: count=%d findings=%+v", count, findings)
	}
	if findings[0].Title != "Stale test for handler" {
		t.Errorf("refreshed first title = %q", findings[0].Title)
	}
}
