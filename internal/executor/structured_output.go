package executor

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSON Schema constants for Claude Code --json-schema structured output

// ClassificationSchema for complexity classifier
const ClassificationSchema = `{"type":"object","properties":{"complexity":{"type":"string","enum":["TRIVIAL","SIMPLE","MEDIUM","COMPLEX","EPIC"]},"reason":{"type":"string"}},"required":["complexity","reason"]}`

// EffortSchema for effort classifier
const EffortSchema = `{"type":"object","properties":{"effort":{"type":"string","enum":["low","medium","high"]},"reason":{"type":"string"}},"required":["effort","reason"]}`

// PostExecutionSummarySchema for branch/SHA/files extraction
const PostExecutionSummarySchema = `{"type":"object","properties":{"branch_name":{"type":"string"},"commit_sha":{"type":"string"},"files_changed":{"type":"array","items":{"type":"string"}},"summary":{"type":"string"}},"required":["branch_name","commit_sha"]}`

// claudeCodeWrapper represents the wrapper format returned by Claude Code with --json-schema
type claudeCodeWrapper struct {
	Result           string          `json:"result"`
	SessionID        string          `json:"session_id"`
	StructuredOutput json.RawMessage `json:"structured_output"`
}

// extractStructuredOutput parses the Claude Code --json-schema wrapper format
// and returns the structured_output field.
// Input format: {"result":"...","session_id":"...","structured_output":{...}}
// Returns: the contents of the structured_output field
func extractStructuredOutput(jsonResponse []byte) (json.RawMessage, error) {
	var wrapper claudeCodeWrapper
	if err := json.Unmarshal(jsonResponse, &wrapper); err != nil {
		return nil, fmt.Errorf("parse claude code wrapper: %w", err)
	}

	if len(wrapper.StructuredOutput) == 0 || string(wrapper.StructuredOutput) == "null" {
		return nil, fmt.Errorf("empty structured_output field in response")
	}

	return wrapper.StructuredOutput, nil
}

// unmarshalJSONFence strips an optional markdown code-fence wrapper from text and
// unmarshals the result into a value of type T. It returns the parsed value, the
// stripped text (so callers can include the raw payload in their own error
// message), and the unmarshal error (nil on success). Callers wrap the returned
// error with their domain-specific message to keep error output unchanged.
func unmarshalJSONFence[T any](text string) (T, string, error) {
	var v T
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	err := json.Unmarshal([]byte(text), &v)
	return v, text, err
}
