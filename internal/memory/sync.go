package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PatternScope defines the visibility scope of a pattern
type PatternScope string

const (
	ScopeProject PatternScope = "project" // Visible only within the project
	ScopeOrg     PatternScope = "org"     // Visible to all org projects
	ScopeGlobal  PatternScope = "global"  // Visible to all users (future)
)

// PatternSync handles cross-project pattern synchronization
type PatternSync struct {
	store       *Store
	orgPatterns *OrgPatternStore
}

// NewPatternSync creates a new pattern sync service
func NewPatternSync(store *Store, dataPath string) (*PatternSync, error) {
	orgStore, err := NewOrgPatternStore(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create org pattern store: %w", err)
	}

	return &PatternSync{
		store:       store,
		orgPatterns: orgStore,
	}, nil
}

// OrgPatternStore manages organization-level pattern aggregation
type OrgPatternStore struct {
	patterns map[string]*AggregatedPattern
	path     string
}

// AggregatedPattern represents a pattern aggregated across projects
type AggregatedPattern struct {
	ID            string           `json:"id"`
	Type          string           `json:"type"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	Context       string           `json:"context"`
	Examples      []string         `json:"examples"`
	Confidence    float64          `json:"confidence"`
	Occurrences   int              `json:"occurrences"`
	ProjectCount  int              `json:"project_count"`
	Projects      []ProjectMention `json:"projects"`
	IsAntiPattern bool             `json:"is_anti_pattern"`
	LastSynced    time.Time        `json:"last_synced"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

// ProjectMention tracks a pattern's presence in a specific project
type ProjectMention struct {
	ProjectPath string    `json:"project_path"`
	Uses        int       `json:"uses"`
	SuccessRate float64   `json:"success_rate"`
	LastUsed    time.Time `json:"last_used"`
}

// NewOrgPatternStore creates a new organization pattern store
func NewOrgPatternStore(dataPath string) (*OrgPatternStore, error) {
	store := &OrgPatternStore{
		patterns: make(map[string]*AggregatedPattern),
		path:     filepath.Join(dataPath, "org_patterns.json"),
	}

	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return store, nil
}

// load loads patterns from disk
func (s *OrgPatternStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	var patterns []*AggregatedPattern
	if err := json.Unmarshal(data, &patterns); err != nil {
		return err
	}

	for _, p := range patterns {
		s.patterns[p.ID] = p
	}

	return nil
}

// save persists patterns to disk
func (s *OrgPatternStore) save() error {
	patterns := make([]*AggregatedPattern, 0, len(s.patterns))
	for _, p := range s.patterns {
		patterns = append(patterns, p)
	}

	// Sort by confidence for predictable output
	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].Confidence > patterns[j].Confidence
	})

	data, err := json.MarshalIndent(patterns, "", "  ")
	if err != nil {
		return err
	}

	return writeFileAtomic(s.path, data)
}

// Get retrieves an aggregated pattern by ID
func (s *OrgPatternStore) Get(id string) (*AggregatedPattern, bool) {
	p, ok := s.patterns[id]
	return p, ok
}

// GetAll retrieves all aggregated patterns
func (s *OrgPatternStore) GetAll() []*AggregatedPattern {
	patterns := make([]*AggregatedPattern, 0, len(s.patterns))
	for _, p := range s.patterns {
		patterns = append(patterns, p)
	}
	return patterns
}

// Update updates an aggregated pattern
func (s *OrgPatternStore) Update(pattern *AggregatedPattern) error {
	pattern.UpdatedAt = time.Now()
	pattern.LastSynced = time.Now()
	s.patterns[pattern.ID] = pattern
	return s.save()
}
