package executor

import (
	"encoding/json"
	"strings"
)

// --- OpenAI API Types ---

// openaiMsg is the wire format for a single conversation turn.
type openaiMsg struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"` // nil serialises as null (for tool-call-only turns)
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"` // "function"
	Function openaiCallFunc `json:"function"`
}

// openaiCallFunc carries the resolved call (name + argument JSON string).
type openaiCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // complete JSON string
}

// openaiToolDef describes a tool in the request.
type openaiToolDef struct {
	Type     string        `json:"type"` // "function"
	Function openaiFuncDef `json:"function"`
}

type openaiFuncDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMsg     `json:"messages"`
	Tools    []openaiToolDef `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

// SSE streaming types

type openaiChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Model   string         `json:"model"`
	Choices []openaiChoice `json:"choices"`
	Usage   *openaiUsage   `json:"usage,omitempty"`
}

type openaiChoice struct {
	Index        int         `json:"index"`
	Delta        openaiDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"` // pointer — null vs "stop"/"tool_calls"
}

type openaiDelta struct {
	Role      string                `json:"role,omitempty"`
	Content   string                `json:"content,omitempty"`
	ToolCalls []openaiDeltaToolCall `json:"tool_calls,omitempty"`
}

type openaiDeltaToolCall struct {
	Index    int             `json:"index"`
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Function openaiDeltaFunc `json:"function"`
}

type openaiDeltaFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"` // fragment
}

type openaiUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// openaiAccum collects the fully parsed result of one SSE stream.
type openaiAccum struct {
	Model            string
	Content          string
	ToolCalls        []openaiToolCall
	FinishReason     string
	PromptTokens     int64
	CompletionTokens int64
}

// pendingOAIToolCall accumulates fragmented streaming tool-call data.
type pendingOAIToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

// --- Error Type ---

// OpenAIAPIError implements BackendError for OpenAI-compatible API errors.
type OpenAIAPIError struct {
	ErrType string
	Msg     string
}

func (e *OpenAIAPIError) Error() string        { return e.Msg }
func (e *OpenAIAPIError) ErrorType() string    { return e.ErrType }
func (e *OpenAIAPIError) ErrorMessage() string { return e.Msg }
func (e *OpenAIAPIError) ErrorStderr() string  { return "" }
