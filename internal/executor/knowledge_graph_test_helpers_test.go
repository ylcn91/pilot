package executor

import (
	"sync"

	"github.com/ylcn91/pilot/internal/memory"
)

// mockKnowledgeGraphRecorder implements KnowledgeGraphRecorder for testing.
type mockKnowledgeGraphRecorder struct {
	mu             sync.Mutex
	learningCalls  []graphLearningCall
	keywordResults []*memory.GraphNode
	returnErr      error
}

type graphLearningCall struct {
	title        string
	content      string
	filesChanged []string
	patterns     []string
	outcome      string
}

func (m *mockKnowledgeGraphRecorder) AddExecutionLearning(title, content string, filesChanged []string, patterns []string, outcome string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.learningCalls = append(m.learningCalls, graphLearningCall{
		title:        title,
		content:      content,
		filesChanged: filesChanged,
		patterns:     patterns,
		outcome:      outcome,
	})
	return m.returnErr
}

func (m *mockKnowledgeGraphRecorder) GetRelatedByKeywords(_ []string) []*memory.GraphNode {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keywordResults
}
