package memory

import (
	"sort"
	"strings"
)

// Search searches nodes by query
func (kg *KnowledgeGraph) Search(query string) []*GraphNode {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	query = strings.ToLower(query)
	var results []*GraphNode

	for _, node := range kg.nodes {
		if strings.Contains(strings.ToLower(node.Title), query) ||
			strings.Contains(strings.ToLower(node.Content), query) ||
			strings.Contains(strings.ToLower(node.Type), query) {
			results = append(results, node)
		}
	}

	return results
}

// GetByType retrieves nodes by type
func (kg *KnowledgeGraph) GetByType(nodeType string) []*GraphNode {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	var results []*GraphNode
	for _, node := range kg.nodes {
		if node.Type == nodeType {
			results = append(results, node)
		}
	}

	return results
}

// GetRelated retrieves related nodes
func (kg *KnowledgeGraph) GetRelated(id string) []*GraphNode {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	node, ok := kg.nodes[id]
	if !ok {
		return nil
	}

	var results []*GraphNode
	for _, relID := range node.Relations {
		if related, ok := kg.nodes[relID]; ok {
			results = append(results, related)
		}
	}

	return results
}

// GetRelatedByKeywords searches nodes by keywords across title, content, and
// metadata values. Returns matching nodes sorted by recency (newest first).
func (kg *KnowledgeGraph) GetRelatedByKeywords(keywords []string) []*GraphNode {
	if len(keywords) == 0 {
		return nil
	}

	kg.mu.RLock()
	defer kg.mu.RUnlock()

	var results []*GraphNode
	for _, node := range kg.nodes {
		if kg.nodeMatchesKeywords(node, keywords) {
			results = append(results, node)
		}
	}

	// Sort by UpdatedAt descending (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})

	return results
}

// nodeMatchesKeywords checks if any keyword matches the node's title, content,
// or metadata string values. All comparisons are case-insensitive.
func (kg *KnowledgeGraph) nodeMatchesKeywords(node *GraphNode, keywords []string) bool {
	titleLower := strings.ToLower(node.Title)
	contentLower := strings.ToLower(node.Content)

	for _, kw := range keywords {
		kwLower := strings.ToLower(kw)
		if strings.Contains(titleLower, kwLower) || strings.Contains(contentLower, kwLower) {
			return true
		}
		// Search metadata values
		for _, v := range node.Metadata {
			switch val := v.(type) {
			case string:
				if strings.Contains(strings.ToLower(val), kwLower) {
					return true
				}
			case []interface{}:
				for _, item := range val {
					if s, ok := item.(string); ok && strings.Contains(strings.ToLower(s), kwLower) {
						return true
					}
				}
			}
		}
	}
	return false
}

// GetPatterns retrieves all patterns
func (kg *KnowledgeGraph) GetPatterns() []*GraphNode {
	return kg.GetByType("pattern")
}

// GetLearnings retrieves all learnings
func (kg *KnowledgeGraph) GetLearnings() []*GraphNode {
	return kg.GetByType("learning")
}
