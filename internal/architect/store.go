package architect

import (
	"context"
	"errors"
	"sync"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// Sentinel errors returned by RunRadar for misconfiguration, so callers can
// distinguish a programmer error from a scan failure.
var (
	errNilStore      = errors.New("architect: RunRadar requires a non-nil FindingsStore")
	errNoProjectPath = errors.New("architect: RunRadar requires a non-empty ProjectPath")
)

// FindingsStore is a thread-safe holder for the latest ranked Architect
// findings. It is the seam between the radar lens (which periodically produces
// findings) and the dashboard sink (which renders them across web/TUI/desktop):
// a scheduler calls Set with each fresh scan's result, while the gateway reads
// the current snapshot via Findings.
//
// It STRUCTURALLY implements gateway.ArchitectProvider — the single-method
// `Findings() []pilotapi.Finding` interface — so it can be injected with
// Server.SetArchitectProvider without internal/architect importing
// internal/gateway (which would invert the dependency direction). The gateway
// depends only on the leaf package internal/pilotapi for the Finding shape; the
// store satisfies its provider contract by shape alone.
//
// The zero value is ready to use and reports an empty (non-nil) slice until the
// first Set, so a gateway wired to a never-populated store renders an empty
// radar rather than nil-panicking.
type FindingsStore struct {
	mu       sync.RWMutex
	findings []pilotapi.Finding
}

// NewFindingsStore returns an empty, ready-to-use store. The zero value works
// too; the constructor exists for call-site clarity at injection points.
func NewFindingsStore() *FindingsStore {
	return &FindingsStore{}
}

// Set replaces the store's findings with a defensive copy of next, so a later
// mutation of the caller's slice cannot race with or corrupt a concurrent
// Findings read. A nil or empty next clears the store to a non-nil empty slice.
func (s *FindingsStore) Set(next []pilotapi.Finding) {
	cp := make([]pilotapi.Finding, len(next))
	copy(cp, next)

	s.mu.Lock()
	s.findings = cp
	s.mu.Unlock()
}

// Findings returns a snapshot copy of the current findings. The result is never
// nil (an unpopulated store yields a zero-length slice) and is safe for the
// caller to retain or mutate: it shares no backing array with the store's
// internal state, so subsequent Set calls do not alter a previously-returned
// snapshot.
func (s *FindingsStore) Findings() []pilotapi.Finding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]pilotapi.Finding, len(s.findings))
	copy(out, s.findings)
	return out
}

// Len reports how many findings the store currently holds, without copying the
// slice. It is a cheap probe for callers (e.g. health checks) that only need
// the count.
func (s *FindingsStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.findings)
}

// RadarConfig is the input to RunRadar: the project root to scan and the scan
// tuning passed through to the radar lens's collectors. It deliberately omits
// any EMIT/issue-creation fields — RunRadar never files issues; it only refreshes
// the in-memory findings sink the dashboard reads.
type RadarConfig struct {
	// ProjectPath is the project root the radar lens scans. Required; an empty
	// path makes RunRadar return an error rather than scanning the process CWD by
	// surprise.
	ProjectPath string

	// Options tunes the radar roster (e.g. QualityRunner, MinCoverage, ProjectID).
	// The zero value runs the full deterministic roster with the quality-gate
	// collectors inert, which is the correct unattended default.
	Options ScanOptions
}

// RunRadar runs the radar lens against cfg.ProjectPath using the deterministic,
// network-free PROPOSE path (SynthesizeFindings), ranks the result, and stores
// it in store so the dashboard sink picks it up. It is the entry point a
// scheduler calls on each tick.
//
// It is fully offline: no LLM backend is spawned and no issues are filed, so it
// is safe to run unattended on a timer. Every collector in the radar roster
// degrades gracefully, so a missing `go`/`git` toolchain yields fewer signals
// rather than an error. RunRadar returns an error only for a misconfiguration
// (empty path or nil store) or a context cancellation during the scan; a scan
// that legitimately finds nothing clears the store to empty and returns nil.
func RunRadar(ctx context.Context, cfg RadarConfig, store *FindingsStore) error {
	if store == nil {
		return errNilStore
	}
	if cfg.ProjectPath == "" {
		return errNoProjectPath
	}

	scanner, err := BuildLensScanner(RadarLensName, cfg.ProjectPath, cfg.Options)
	if err != nil {
		return err
	}

	signals, err := scanner.Scan(ctx, cfg.ProjectPath)
	if err != nil {
		return err
	}

	findings := rankFindings(SynthesizeFindings(signals))
	store.Set(findings)
	return nil
}
