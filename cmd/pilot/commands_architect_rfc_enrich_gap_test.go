package main

import (
	"context"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
)

// TestEnrichRFCWithBackend_UnknownLensFallsBack proves the first fallback gate:
// an unresolvable --lens makes LensByName error, so enrichRFCWithBackend returns
// ok=false with a zero RFCDoc before any backend is constructed. This is a pure,
// network-free path.
func TestEnrichRFCWithBackend_UnknownLensFallsBack(t *testing.T) {
	dir := t.TempDir()
	writeMinimalGoModule(t, dir)
	cfg := rfcTestConfig()

	signals := []architect.Signal{{Kind: "long_function", File: "main.go"}}
	f := &architectFlags{dryRun: false, lens: "no-such-lens"}

	doc, ok := enrichRFCWithBackend(context.Background(), cfg, dir, f, signals, nil, nil)
	if ok {
		t.Fatal("unknown lens must make enrichRFCWithBackend return ok=false")
	}
	if doc != (architect.RFCDoc{}) {
		t.Fatalf("fallback must return a zero RFCDoc, got %+v", doc)
	}
}

// TestEnrichRFCWithBackend_BackendErrorFallsBack proves the Propose-error gate:
// a non-empty signal set with an unresolvable backend type makes the analyzer's
// backend factory error, so Propose returns an error and enrichRFCWithBackend
// falls back (ok=false). No subprocess is spawned — the unknown backend type is
// rejected by the factory before any execution.
func TestEnrichRFCWithBackend_BackendErrorFallsBack(t *testing.T) {
	dir := t.TempDir()
	writeMinimalGoModule(t, dir)
	cfg := rfcTestConfig()

	signals := []architect.Signal{{Kind: "long_function", File: "main.go"}}
	f := &architectFlags{dryRun: false, lens: "rfc", backend: "definitely-not-a-real-backend"}

	doc, ok := enrichRFCWithBackend(context.Background(), cfg, dir, f, signals, nil, nil)
	if ok {
		t.Fatal("an unresolvable backend must make enrichRFCWithBackend return ok=false")
	}
	if doc != (architect.RFCDoc{}) {
		t.Fatalf("fallback must return a zero RFCDoc, got %+v", doc)
	}
}

// TestEnrichRFCWithBackend_EmptyFindingsFallsBack proves the empty-response gate:
// an empty signal set makes Propose short-circuit to an empty, non-nil finding
// slice (no backend call), and len(findings)==0 drives the same ok=false
// fallback — distinct from the error branch above.
func TestEnrichRFCWithBackend_EmptyFindingsFallsBack(t *testing.T) {
	dir := t.TempDir()
	writeMinimalGoModule(t, dir)
	cfg := rfcTestConfig()

	f := &architectFlags{dryRun: false, lens: "rfc", backend: "definitely-not-a-real-backend"}

	doc, ok := enrichRFCWithBackend(context.Background(), cfg, dir, f, nil, nil, nil)
	if ok {
		t.Fatal("an empty backend response must make enrichRFCWithBackend return ok=false")
	}
	if doc != (architect.RFCDoc{}) {
		t.Fatalf("fallback must return a zero RFCDoc, got %+v", doc)
	}
}
