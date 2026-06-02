package executor

import "encoding/json"

// --- Anthropic API Types ---

type apiMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []apiContentBlock
}

type apiContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type apiToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []apiMessage `json:"messages"`
	Tools     []apiToolDef `json:"tools,omitempty"`
	Stream    bool         `json:"stream"`
	Thinking  *apiThinking `json:"thinking,omitempty"`
}

type apiThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type apiResponse struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Role       string            `json:"role"`
	Content    []apiContentBlock `json:"content"`
	Model      string            `json:"model"`
	StopReason string            `json:"stop_reason"`
	Usage      apiUsage          `json:"usage"`
	Error      *apiError         `json:"error,omitempty"`
}

type apiUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// SSE event for streaming
type sseEvent struct {
	Type string `json:"type"`
	// Various data fields depending on type
	Index        int              `json:"index,omitempty"`
	ContentBlock *apiContentBlock `json:"content_block,omitempty"`
	Delta        *sseDelta        `json:"delta,omitempty"`
	Message      *apiResponse     `json:"message,omitempty"`
	Usage        *apiUsage        `json:"usage,omitempty"`
	// Error carries the nested {"error": {...}} of an in-stream "error" event.
	// Without it, marshaling the event drops the detail and retry classification
	// (callAPI checks for "overloaded") cannot see an in-stream overloaded_error.
	Error *apiError `json:"error,omitempty"`
}

type sseDelta struct {
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

// --- Tool Definitions ---

var apiTools = []apiToolDef{
	{
		Name:        "bash",
		Description: "Execute a bash command. Returns stdout+stderr. Commands timeout after 120s.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "The bash command to execute"},
				"timeout": {"type": "integer", "description": "Timeout in seconds (default 120, max 600)"}
			},
			"required": ["command"]
		}`),
	},
	{
		Name:        "read_file",
		Description: "Read a file's contents with line numbers.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute path to the file"},
				"offset": {"type": "integer", "description": "Line number to start from (1-indexed)"},
				"limit": {"type": "integer", "description": "Max lines to read"}
			},
			"required": ["path"]
		}`),
	},
	{
		Name:        "write_file",
		Description: "Write content to a file. Creates parent directories. Overwrites existing.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute path to write to"},
				"content": {"type": "string", "description": "File content"}
			},
			"required": ["path", "content"]
		}`),
	},
	{
		Name:        "edit_file",
		Description: "Replace a specific string in a file. old_string must appear exactly once.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute path"},
				"old_string": {"type": "string", "description": "Exact text to find"},
				"new_string": {"type": "string", "description": "Replacement text"}
			},
			"required": ["path", "old_string", "new_string"]
		}`),
	},
}
