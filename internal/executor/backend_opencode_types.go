package executor

// ocModelRef matches OpenCode v1.4.x's PromptInput.model schema:
// {providerID, modelID}. Encoded as a JSON object.
type ocModelRef struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// openCodeEvent represents an event from OpenCode's SSE stream.
type openCodeEvent struct {
	Type    string                 `json:"type"`
	Tool    string                 `json:"tool,omitempty"`
	Input   map[string]interface{} `json:"input,omitempty"`
	Output  string                 `json:"output,omitempty"`
	Error   string                 `json:"error,omitempty"`
	IsError bool                   `json:"is_error,omitempty"`
	Model   string                 `json:"model,omitempty"`
	Delta   *openCodeDelta         `json:"delta,omitempty"`
	Usage   *openCodeUsage         `json:"usage,omitempty"`
}

type openCodeDelta struct {
	Text string `json:"text,omitempty"`
}

// openCodeUsage matches the usage shape OpenCode v1.4.x emits in SSE events.
// Both flat (cache_*) and nested (cache.{read,write}) shapes are accepted —
// different OpenCode builds and provider passthroughs use different layouts.
// GH-2428.
type openCodeUsage struct {
	InputTokens              int64        `json:"input_tokens"`
	OutputTokens             int64        `json:"output_tokens"`
	CacheCreationInputTokens int64        `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64        `json:"cache_read_input_tokens,omitempty"`
	Cache                    ocCacheToken `json:"cache,omitempty"`
}

// cacheCreate returns cache creation tokens from either layout.
func (u openCodeUsage) cacheCreate() int64 {
	if u.CacheCreationInputTokens > 0 {
		return u.CacheCreationInputTokens
	}
	return u.Cache.Write
}

// cacheRead returns cache read tokens from either layout.
func (u openCodeUsage) cacheRead() int64 {
	if u.CacheReadInputTokens > 0 {
		return u.CacheReadInputTokens
	}
	return u.Cache.Read
}

// ocAssistantResponse mirrors the response body of
// POST /session/:sessionID/message in OpenCode v1.4.x:
//
//	{info: AssistantMessage, parts: Part[]}
//
// Field set is intentionally minimal — only what Pilot consumes. Unknown
// fields are ignored by the JSON decoder.
type ocAssistantResponse struct {
	Info  ocAssistantInfo `json:"info"`
	Parts []ocPart        `json:"parts"`
}

type ocAssistantInfo struct {
	ID         string         `json:"id,omitempty"`
	Role       string         `json:"role,omitempty"`
	SessionID  string         `json:"sessionID,omitempty"`
	ProviderID string         `json:"providerID,omitempty"`
	ModelID    string         `json:"modelID,omitempty"`
	Model      string         `json:"model,omitempty"` // legacy/fallback
	Tokens     ocTokens       `json:"tokens"`
	Cost       float64        `json:"cost,omitempty"`
	Error      ocAssistantErr `json:"error,omitempty"`
}

type ocTokens struct {
	Input     int64        `json:"input"`
	Output    int64        `json:"output"`
	Reasoning int64        `json:"reasoning,omitempty"`
	Cache     ocCacheToken `json:"cache"`
}

type ocCacheToken struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

type ocAssistantErr struct {
	Name    string `json:"name,omitempty"`
	Message string `json:"message,omitempty"`
}

// ocPart covers the relevant union variants from MessageV2.Part. Only fields
// Pilot uses are mapped; other types decode with zero values for unused fields.
type ocPart struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	Tool  string      `json:"tool,omitempty"`
	State ocPartState `json:"state,omitempty"`
}

type ocPartState struct {
	Status string                 `json:"status,omitempty"`
	Input  map[string]interface{} `json:"input,omitempty"`
	Output string                 `json:"output,omitempty"`
}
