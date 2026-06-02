package memory

import (
	"encoding/json"
)

// PatternAnalysisRequest is sent to the Python LLM analyzer
type PatternAnalysisRequest struct {
	ExecutionID   string `json:"execution_id"`
	ProjectPath   string `json:"project_path"`
	Output        string `json:"output"`
	Error         string `json:"error,omitempty"`
	DiffContent   string `json:"diff_content,omitempty"`
	CommitMessage string `json:"commit_message,omitempty"`
}

// PatternAnalysisResponse is returned from the Python LLM analyzer
type PatternAnalysisResponse struct {
	Patterns     []LLMPattern `json:"patterns"`
	AntiPatterns []LLMPattern `json:"anti_patterns"`
}

// LLMPattern is a pattern identified by the LLM
type LLMPattern struct {
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Context     string   `json:"context"`
	Examples    []string `json:"examples,omitempty"`
	Confidence  float64  `json:"confidence"`
}

// ToJSON converts the request to JSON for the Python bridge
func (r *PatternAnalysisRequest) ToJSON() (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseAnalysisResponse parses the Python LLM response
func ParseAnalysisResponse(jsonData string) (*PatternAnalysisResponse, error) {
	var resp PatternAnalysisResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
