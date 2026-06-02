package memory

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// GraphNode represents a node in the knowledge graph
type GraphNode struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Title     string                 `json:"title"`
	Content   string                 `json:"content,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Relations []string               `json:"relations,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// KnowledgeGraph provides cross-project knowledge management
type KnowledgeGraph struct {
	nodes     map[string]*GraphNode
	path      string
	mu        sync.RWMutex
	saveCount int64 // incremented by saveUnlocked on each successful disk write
}

// NewKnowledgeGraph creates a new knowledge graph
func NewKnowledgeGraph(dataPath string) (*KnowledgeGraph, error) {
	kg := &KnowledgeGraph{
		nodes: make(map[string]*GraphNode),
		path:  filepath.Join(dataPath, "knowledge.json"),
	}

	if err := kg.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return kg, nil
}

// load loads the graph from disk, falling back to knowledge.json.bak on parse failure.
func (kg *KnowledgeGraph) load() error {
	data, err := os.ReadFile(kg.path)
	if err != nil {
		return err
	}

	var nodes []*GraphNode
	if parseErr := json.Unmarshal(data, &nodes); parseErr != nil {
		bakPath := kg.path + ".bak"
		bakData, bakErr := os.ReadFile(bakPath)
		if bakErr != nil {
			return parseErr
		}
		var bakNodes []*GraphNode
		if bakErr = json.Unmarshal(bakData, &bakNodes); bakErr != nil {
			return parseErr
		}
		slog.Warn("knowledge.json corrupt; recovering from backup", "err", parseErr)
		nodes = bakNodes
		if renameErr := os.Rename(bakPath, kg.path); renameErr != nil {
			slog.Warn("failed to promote knowledge.json.bak to primary", "err", renameErr)
		}
	}

	kg.mu.Lock()
	defer kg.mu.Unlock()

	for _, node := range nodes {
		kg.nodes[node.ID] = node
	}

	return nil
}

// saveUnlocked persists the graph atomically (caller must hold lock).
// It writes to a temp file in the same directory, fsyncs, backs up the current
// knowledge.json to knowledge.json.bak, then renames the temp file over knowledge.json.
func (kg *KnowledgeGraph) saveUnlocked() error {
	nodes := make([]*GraphNode, 0, len(kg.nodes))
	for _, node := range kg.nodes {
		nodes = append(nodes, node)
	}

	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(kg.path)
	tmp, err := os.CreateTemp(dir, "knowledge-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}

	// Back up current file before replacing (best-effort).
	if existing, readErr := os.ReadFile(kg.path); readErr == nil {
		_ = os.WriteFile(kg.path+".bak", existing, 0644)
	}

	if err := os.Rename(tmpName, kg.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp to knowledge.json: %w", err)
	}

	atomic.AddInt64(&kg.saveCount, 1)
	return nil
}

// addUnlocked mutates the in-memory node map without persisting (caller must hold write lock).
func (kg *KnowledgeGraph) addUnlocked(node *GraphNode) {
	now := time.Now()
	if existing, ok := kg.nodes[node.ID]; ok {
		node.CreatedAt = existing.CreatedAt
	} else {
		node.CreatedAt = now
	}
	node.UpdatedAt = now
	kg.nodes[node.ID] = node
}

// Add adds or updates a node
func (kg *KnowledgeGraph) Add(node *GraphNode) error {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	if node.ID == "" {
		return fmt.Errorf("node ID is required")
	}

	now := time.Now()
	if existing, ok := kg.nodes[node.ID]; ok {
		node.CreatedAt = existing.CreatedAt
	} else {
		node.CreatedAt = now
	}
	node.UpdatedAt = now

	kg.nodes[node.ID] = node
	return kg.saveUnlocked()
}

// Get retrieves a node by ID
func (kg *KnowledgeGraph) Get(id string) (*GraphNode, bool) {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	node, ok := kg.nodes[id]
	return node, ok
}

// Remove removes a node
func (kg *KnowledgeGraph) Remove(id string) error {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	delete(kg.nodes, id)
	return kg.saveUnlocked()
}

// Count returns the number of nodes
func (kg *KnowledgeGraph) Count() int {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	return len(kg.nodes)
}
