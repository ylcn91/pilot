package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// TestArchitectRadar_StoreToEndpointE2E exercises the full handoff path end to
// end: the radar lens (architect.RunRadar) populates a *architect.FindingsStore
// from a real on-disk fixture, that same store is injected into the gateway as
// the ArchitectProvider, and an HTTP client reads the radar's findings back out
// of the live /api/v1/architect endpoint.
//
// Unlike TestArchitectStoreSatisfiesProviderE2E (which seeds the store with
// hand-written findings via Set), this test never calls Set directly: every
// finding served by the endpoint is produced by the radar's deterministic,
// offline collectors over the fixture. It proves the store -> provider -> web
// path the dashboard sink depends on, against output the radar actually emits.
func TestArchitectRadar_StoreToEndpointE2E(t *testing.T) {
	root := t.TempDir()
	writeOversizedGoFile(t, filepath.Join(root, "big.go"))

	// RunRadar is the scheduler's entry point: deterministic, offline, no LLM,
	// no issue creation. It must synthesise at least one ranked finding from the
	// oversized fixture file into the store.
	store := architect.NewFindingsStore()
	if err := architect.RunRadar(context.Background(), architect.RadarConfig{ProjectPath: root}, store); err != nil {
		t.Fatalf("RunRadar: %v", err)
	}
	if store.Len() == 0 {
		t.Fatal("radar must populate the store with at least one finding from the oversized fixture")
	}

	// Inject the radar-populated store as the gateway's ArchitectProvider and
	// serve the real handler stack.
	server := NewServer(&Config{Host: "127.0.0.1", Port: 0})
	server.SetArchitectProvider(store)

	ts := httptest.NewServer(server.buildHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/architect")
	if err != nil {
		t.Fatalf("GET /api/v1/architect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Count    int                `json:"count"`
		Findings []pilotapi.Finding `json:"findings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// The endpoint must serve exactly what the radar put in the store.
	if body.Count != store.Len() {
		t.Errorf("endpoint count = %d, store Len = %d", body.Count, store.Len())
	}
	if len(body.Findings) == 0 {
		t.Fatal("endpoint returned no findings; radar findings did not reach the web path")
	}

	// Every served finding must be a well-formed radar finding (valid risk,
	// non-empty title), and the radar's LOC signal over big.go must surface.
	var sawFixture bool
	for _, f := range body.Findings {
		if f.Title == "" {
			t.Errorf("served finding missing title: %+v", f)
		}
		if !f.Risk.IsValid() {
			t.Errorf("served finding has invalid risk %q: %+v", f.Risk, f)
		}
		for _, file := range f.Files {
			if filepath.Base(file) == "big.go" {
				sawFixture = true
			}
		}
	}
	if !sawFixture {
		t.Fatalf("endpoint findings did not reference the radar fixture big.go: %+v", body.Findings)
	}
}

// TestArchitectRadar_LiveRefreshThroughEndpoint proves the store remains the
// live seam after wiring: a second RunRadar over a richer fixture refreshes the
// same injected store, and the next endpoint read reflects the new findings
// without re-injecting the provider — exactly how the periodic scheduler keeps
// the dashboard fresh between cron ticks.
func TestArchitectRadar_LiveRefreshThroughEndpoint(t *testing.T) {
	store := architect.NewFindingsStore()

	server := NewServer(&Config{Host: "127.0.0.1", Port: 0})
	server.SetArchitectProvider(store)
	ts := httptest.NewServer(server.buildHandler())
	defer ts.Close()

	count := func() int {
		t.Helper()
		resp, err := http.Get(ts.URL + "/api/v1/architect")
		if err != nil {
			t.Fatalf("GET /api/v1/architect: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			Count int `json:"count"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.Count
	}

	// Before any scan the endpoint serves an empty (never null) radar.
	if got := count(); got != 0 {
		t.Fatalf("pre-scan count = %d, want 0", got)
	}

	// First scan over an empty project: the radar legitimately finds nothing and
	// the endpoint stays empty.
	empty := t.TempDir()
	if err := architect.RunRadar(context.Background(), architect.RadarConfig{ProjectPath: empty}, store); err != nil {
		t.Fatalf("RunRadar(empty): %v", err)
	}
	if got := count(); got != 0 {
		t.Fatalf("post-empty-scan count = %d, want 0", got)
	}

	// Second scan over a fixture with a real drift signal: the same store now
	// carries findings and the endpoint reflects them with no re-injection.
	root := t.TempDir()
	writeOversizedGoFile(t, filepath.Join(root, "huge.go"))
	if err := architect.RunRadar(context.Background(), architect.RadarConfig{ProjectPath: root}, store); err != nil {
		t.Fatalf("RunRadar(fixture): %v", err)
	}
	if got := count(); got == 0 {
		t.Fatal("post-fixture-scan count = 0; radar refresh did not reach the endpoint")
	}
	if got, n := count(), store.Len(); got != n {
		t.Fatalf("endpoint count = %d, store Len = %d after refresh", got, n)
	}
}

// writeOversizedGoFile writes a Go file just over the architect LOC threshold so
// the radar's deterministic LOC collector flags it, giving RunRadar a real
// signal to synthesise into the store.
func writeOversizedGoFile(t *testing.T, path string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("package big\n")
	for i := 0; i < architect.LOCThreshold+10; i++ {
		b.WriteString("// padding line to push the file over the LOC threshold\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for fixture %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}
