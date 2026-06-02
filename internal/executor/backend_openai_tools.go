package executor

import "encoding/json"

// --- Tool Definitions (OpenAI function-call format) ---

var openaiTools = []openaiToolDef{
	{
		Type: "function",
		Function: openaiFuncDef{
			Name:        "bash",
			Description: "Execute a bash command. Returns stdout+stderr. Commands timeout after 120s.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {"type": "string", "description": "The bash command to execute"},
					"timeout": {"type": "integer", "description": "Timeout in seconds (default 120, max 600)"}
				},
				"required": ["command"]
			}`),
		},
	},
	{
		Type: "function",
		Function: openaiFuncDef{
			Name:        "read_file",
			Description: "Read a file's contents with line numbers.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {"type": "string", "description": "Absolute path to the file"},
					"offset": {"type": "integer", "description": "Line number to start from (1-indexed)"},
					"limit": {"type": "integer", "description": "Max lines to read"}
				},
				"required": ["path"]
			}`),
		},
	},
	{
		Type: "function",
		Function: openaiFuncDef{
			Name:        "write_file",
			Description: "Write content to a file. Creates parent directories. Overwrites existing.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {"type": "string", "description": "Absolute path to write to"},
					"content": {"type": "string", "description": "File content"}
				},
				"required": ["path", "content"]
			}`),
		},
	},
	{
		Type: "function",
		Function: openaiFuncDef{
			Name:        "edit_file",
			Description: "Replace a specific string in a file. old_string must appear exactly once.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {"type": "string", "description": "Absolute path"},
					"old_string": {"type": "string", "description": "Exact text to find"},
					"new_string": {"type": "string", "description": "Replacement text"}
				},
				"required": ["path", "old_string", "new_string"]
			}`),
		},
	},
}
