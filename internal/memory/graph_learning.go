package memory

import (
	"fmt"
	"time"
)

// AddPattern adds a pattern to the knowledge graph
func (kg *KnowledgeGraph) AddPattern(patternType, content string, metadata map[string]interface{}) error {
	id := fmt.Sprintf("pattern_%s_%d", patternType, time.Now().UnixNano())
	node := &GraphNode{
		ID:       id,
		Type:     "pattern",
		Title:    patternType,
		Content:  content,
		Metadata: metadata,
	}
	return kg.Add(node)
}

// AddLearning adds a learning to the knowledge graph
func (kg *KnowledgeGraph) AddLearning(title, content string, metadata map[string]interface{}) error {
	id := fmt.Sprintf("learning_%d", time.Now().UnixNano())
	node := &GraphNode{
		ID:       id,
		Type:     "learning",
		Title:    title,
		Content:  content,
		Metadata: metadata,
	}
	return kg.Add(node)
}

// AddExecutionLearning adds a learning node with relations linking task→files,
// files→patterns, patterns→outcome. Unlike AddLearning which creates flat nodes,
// this populates the Relations field to connect related concept nodes.
// All nodes are staged via addUnlocked, then flushed with a single saveUnlocked call.
func (kg *KnowledgeGraph) AddExecutionLearning(title, content string, filesChanged []string, patterns []string, outcome string) error {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	now := time.Now()
	nano := now.UnixNano()

	learningID := fmt.Sprintf("exec_learning_%d", nano)
	var relationIDs []string

	for i, file := range filesChanged {
		fileID := fmt.Sprintf("file_%d_%d", nano, i)
		kg.addUnlocked(&GraphNode{
			ID:    fileID,
			Type:  "file",
			Title: file,
			Metadata: map[string]interface{}{
				"learning_id": learningID,
			},
		})
		relationIDs = append(relationIDs, fileID)
	}

	for i, pattern := range patterns {
		patternID := fmt.Sprintf("exec_pattern_%d_%d", nano, i)
		kg.addUnlocked(&GraphNode{
			ID:    patternID,
			Type:  "pattern",
			Title: pattern,
			Metadata: map[string]interface{}{
				"learning_id": learningID,
			},
		})
		relationIDs = append(relationIDs, patternID)
	}

	outcomeID := fmt.Sprintf("outcome_%d", nano)
	kg.addUnlocked(&GraphNode{
		ID:    outcomeID,
		Type:  "outcome",
		Title: outcome,
		Metadata: map[string]interface{}{
			"learning_id": learningID,
		},
	})
	relationIDs = append(relationIDs, outcomeID)

	kg.addUnlocked(&GraphNode{
		ID:      learningID,
		Type:    "execution_learning",
		Title:   title,
		Content: content,
		Metadata: map[string]interface{}{
			"files_changed": filesChanged,
			"patterns":      patterns,
			"outcome":       outcome,
		},
		Relations: relationIDs,
	})

	return kg.saveUnlocked()
}
