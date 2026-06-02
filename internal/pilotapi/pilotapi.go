// Package pilotapi defines the neutral contract types exchanged between
// Pilot's planning, architecture, test-authoring, implementation, review,
// and execution stages. It is a leaf package: it imports nothing from
// internal/* so that any subsystem (dashboard, executor, orchestrator,
// the Architect family) can depend on it without risking an import cycle.
//
// These types are the single source of truth for the handoff protocol.
// Do not mirror them in other packages — import this one instead.
package pilotapi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// RiskLevel classifies how dangerous a Finding/Proposal is to ship.
type RiskLevel string

// The canonical risk levels, ordered from least to most severe.
const (
	RiskLow            RiskLevel = "low"
	RiskMedium         RiskLevel = "medium"
	RiskHigh           RiskLevel = "high"
	RiskReleaseBlocker RiskLevel = "release-blocker"
)

// IsValid reports whether r is one of the canonical RiskLevel values.
func (r RiskLevel) IsValid() bool {
	switch r {
	case RiskLow, RiskMedium, RiskHigh, RiskReleaseBlocker:
		return true
	default:
		return false
	}
}

// ParseRisk converts a string into a RiskLevel. Parsing is
// case-insensitive and tolerant of surrounding whitespace. The boolean
// result is false (and the returned RiskLevel empty) when s does not name
// a canonical level.
func ParseRisk(s string) (RiskLevel, bool) {
	r := RiskLevel(strings.ToLower(strings.TrimSpace(s)))
	if r.IsValid() {
		return r, true
	}
	return "", false
}

// Finding is a single observation or change proposal emitted by the
// Architect family and rendered by the dashboard. The same shape serves
// both findings (problems spotted) and proposals (changes suggested).
type Finding struct {
	Title             string    `json:"title"`
	Kind              string    `json:"kind"`
	Risk              RiskLevel `json:"risk"`
	WhyItMatters      string    `json:"why_it_matters"`
	SuggestedPRPieces []string  `json:"suggested_pr_pieces"`
	TestPlan          string    `json:"test_plan"`
	Files             []string  `json:"files"`
}

// SchemaVersion is the current version of the HandoffArtifact wire format.
// Bump it whenever the artifact's field set changes incompatibly.
const SchemaVersion = 1

// Canonical roles for a HandoffArtifact, naming the stage that produced it.
const (
	RolePlan        = "plan"
	RoleArchitect   = "architect"
	RoleTestAuthor  = "test-author"
	RoleImplementer = "implementer"
	RoleReview      = "review"
	RoleExecute     = "execute"
)

// HandoffArtifact carries the spec/design text produced by one stage of
// the pipeline to the next. TraceHash makes each artifact content- and
// lineage-addressable; ParentHash links it to the artifact it was derived
// from, forming an auditable chain.
type HandoffArtifact struct {
	SchemaVersion int    `json:"schema_version"`
	Role          string `json:"role"`
	Content       string `json:"content"`
	TraceHash     string `json:"trace_hash"`
	ParentHash    string `json:"parent_hash"`
	TaskID        string `json:"task_id"`
}

// NewHandoffArtifact builds a HandoffArtifact for the given role, task, and
// content, linking it to parentHash. It fills SchemaVersion and computes a
// deterministic TraceHash over the artifact's identifying fields, so two
// artifacts with identical role/taskID/content/parent share a hash and any
// difference produces a different one.
func NewHandoffArtifact(role, taskID, content, parentHash string) HandoffArtifact {
	return HandoffArtifact{
		SchemaVersion: SchemaVersion,
		Role:          role,
		Content:       content,
		TraceHash:     TraceHash(role, taskID, content, parentHash),
		ParentHash:    parentHash,
		TaskID:        taskID,
	}
}

// traceHashSep separates parts before hashing. Using a separator that is
// unlikely to appear in a single part keeps the join injective for typical
// inputs, so ["a","b"] and ["ab"] hash differently.
const traceHashSep = "\x00"

// TraceHash returns the first 12 hex characters of the SHA-256 of the
// parts joined by a NUL separator. It is deterministic and order-sensitive:
// the same parts in the same order always yield the same hash, and
// reordering the parts yields a different one.
func TraceHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, traceHashSep)))
	return hex.EncodeToString(sum[:])[:12]
}
